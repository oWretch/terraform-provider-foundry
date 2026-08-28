package clients

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	randv2 "math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

const (
	defaultAPIVersion = "v1"
	defaultUserAgent  = "terraform-provider-foundry/dev"
	maxErrorBody      = 64 << 10
)

type Environment string

const (
	EnvironmentPublic       Environment = "public"
	EnvironmentUSGovernment Environment = "usgovernment"
	EnvironmentChina        Environment = "china"
)

type AuthenticationMethod string

const (
	AuthenticationAPIKey            AuthenticationMethod = "api_key"
	AuthenticationClientSecret      AuthenticationMethod = "client_secret"
	AuthenticationClientCertificate AuthenticationMethod = "client_certificate"
	AuthenticationOIDC              AuthenticationMethod = "oidc"
	AuthenticationManagedIdentity   AuthenticationMethod = "managed_identity"
	AuthenticationAzureCLI          AuthenticationMethod = "azure_cli"
)

type Authentication struct {
	Method              AuthenticationMethod
	APIKey              string
	TenantID            string
	ClientID            string
	ClientSecret        string
	CertificatePath     string
	CertificatePassword string
	OIDCTokenFilePath   string
}

type Config struct {
	AccountName string
	ProjectName string
	Environment Environment
	UserAgent   string
	Transport   http.RoundTripper
	Auth        Authentication
}

type Client struct {
	endpoint   *url.URL
	apiVersion string
	userAgent  string
	httpClient *http.Client
}

type ResponseError struct {
	Method          string
	URL             string
	StatusCode      int
	RequestID       string
	ClientRequestID string
	Body            string
}

type environmentConfiguration struct {
	cloud          cloud.Configuration
	endpointSuffix string
	tokenScope     string
}

func (e *ResponseError) Error() string {
	message := fmt.Sprintf("%s %s returned %s", e.Method, e.URL, http.StatusText(e.StatusCode))
	if e.RequestID != "" {
		message += fmt.Sprintf(" (request ID %s)", e.RequestID)
	}
	if e.ClientRequestID != "" {
		message += fmt.Sprintf(" (client request ID %s)", e.ClientRequestID)
	}
	if e.Body != "" {
		message += ": " + e.Body
	}
	return message
}

func New(config Config) (*Client, error) {
	if !validAccountName(config.AccountName) {
		return nil, fmt.Errorf("account name must contain only letters, numbers, and hyphens, and must start and end with a letter or number")
	}
	if strings.TrimSpace(config.ProjectName) == "" {
		return nil, fmt.Errorf("project name must not be empty")
	}

	environment, err := configurationForEnvironment(config.Environment)
	if err != nil {
		return nil, err
	}
	endpoint, err := url.Parse(fmt.Sprintf(
		"https://%s.%s/api/projects/%s",
		strings.ToLower(config.AccountName),
		environment.endpointSuffix,
		url.PathEscape(config.ProjectName),
	))
	if err != nil {
		return nil, fmt.Errorf("build project endpoint: %w", err)
	}
	transport := config.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	authTransport, err := newAuthenticationTransport(config.Auth, environment, transport)
	if err != nil {
		return nil, err
	}

	userAgent := config.UserAgent
	if userAgent == "" {
		userAgent = defaultUserAgent
	}

	return &Client{
		endpoint:   endpoint,
		apiVersion: defaultAPIVersion,
		userAgent:  userAgent,
		httpClient: &http.Client{
			Transport: &retryTransport{
				base:       authTransport,
				maxRetries: 3,
			},
		},
	}, nil
}

func configurationForEnvironment(environment Environment) (environmentConfiguration, error) {
	switch environment {
	case EnvironmentPublic:
		return environmentConfiguration{
			cloud:          cloud.AzurePublic,
			endpointSuffix: "services.ai.azure.com",
			tokenScope:     "https://ai.azure.com/.default",
		}, nil
	case EnvironmentUSGovernment:
		return environmentConfiguration{
			cloud:          cloud.AzureGovernment,
			endpointSuffix: "services.ai.azure.us",
			tokenScope:     "https://ai.azure.us/.default",
		}, nil
	case EnvironmentChina:
		return environmentConfiguration{
			cloud:          cloud.AzureChina,
			endpointSuffix: "services.ai.azure.cn",
			tokenScope:     "https://ai.azure.cn/.default",
		}, nil
	default:
		return environmentConfiguration{}, fmt.Errorf("unsupported environment %q; use public, usgovernment, or china", environment)
	}
}

func validAccountName(accountName string) bool {
	if accountName == "" {
		return false
	}
	for index, character := range accountName {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' {
			continue
		}
		if character != '-' || index == 0 || index == len(accountName)-1 {
			return false
		}
	}
	return true
}

func (c *Client) NewRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	reference, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("parse request path: %w", err)
	}
	if reference.IsAbs() || reference.Host != "" {
		return nil, fmt.Errorf("request path must be relative to the configured endpoint")
	}

	requestURL, err := url.Parse(strings.TrimRight(c.endpoint.String(), "/") + "/" + strings.TrimLeft(reference.String(), "/"))
	if err != nil {
		return nil, fmt.Errorf("build request URL: %w", err)
	}
	// OpenAI-compatible routes are versioned in the path and reject api-version.
	if !isOpenAIRoute(reference.Path) {
		query := requestURL.Query()
		if query.Get("api-version") == "" {
			query.Set("api-version", c.apiVersion)
			requestURL.RawQuery = query.Encode()
		}
	}

	request, err := http.NewRequestWithContext(ctx, method, requestURL.String(), body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", c.userAgent)

	requestID := make([]byte, 16)
	if _, err := rand.Read(requestID); err != nil {
		return nil, fmt.Errorf("generate client request ID: %w", err)
	}
	request.Header.Set("x-ms-client-request-id", hex.EncodeToString(requestID))

	return request, nil
}

func isOpenAIRoute(path string) bool {
	return strings.HasPrefix(strings.TrimLeft(path, "/"), "openai/")
}

func (c *Client) Do(request *http.Request) (*http.Response, error) {
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("send Foundry request: %w", err)
	}
	if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		return response, nil
	}

	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxErrorBody))
	if readErr != nil {
		_ = response.Body.Close()
		return nil, fmt.Errorf("read Foundry error response: %w", readErr)
	}
	if err := response.Body.Close(); err != nil {
		return nil, fmt.Errorf("close Foundry error response: %w", err)
	}

	return nil, &ResponseError{
		Method:          request.Method,
		URL:             request.URL.String(),
		StatusCode:      response.StatusCode,
		RequestID:       response.Header.Get("x-request-id"),
		ClientRequestID: request.Header.Get("x-ms-client-request-id"),
		Body:            strings.TrimSpace(string(body)),
	}
}

func newAuthenticationTransport(authentication Authentication, environment environmentConfiguration, base http.RoundTripper) (http.RoundTripper, error) {
	if authentication.Method == AuthenticationAPIKey {
		if authentication.APIKey == "" {
			return nil, fmt.Errorf("API key must not be empty")
		}
		return &authenticationTransport{base: base, apiKey: authentication.APIKey}, nil
	}

	clientOptions := azcore.ClientOptions{Cloud: environment.cloud}
	var credential azcore.TokenCredential
	var err error
	switch authentication.Method {
	case AuthenticationClientSecret:
		credential, err = azidentity.NewClientSecretCredential(authentication.TenantID, authentication.ClientID, authentication.ClientSecret, &azidentity.ClientSecretCredentialOptions{
			ClientOptions: clientOptions,
		})
	case AuthenticationClientCertificate:
		data, readErr := os.ReadFile(authentication.CertificatePath)
		if readErr != nil {
			return nil, fmt.Errorf("read client certificate: %w", readErr)
		}
		var password []byte
		if authentication.CertificatePassword != "" {
			password = []byte(authentication.CertificatePassword)
		}
		certificates, privateKey, parseErr := azidentity.ParseCertificates(data, password)
		if parseErr != nil {
			return nil, fmt.Errorf("parse client certificate: %w", parseErr)
		}
		credential, err = azidentity.NewClientCertificateCredential(authentication.TenantID, authentication.ClientID, certificates, privateKey, &azidentity.ClientCertificateCredentialOptions{
			ClientOptions: clientOptions,
		})
	case AuthenticationOIDC:
		credential, err = azidentity.NewWorkloadIdentityCredential(&azidentity.WorkloadIdentityCredentialOptions{
			ClientOptions: clientOptions,
			TenantID:      authentication.TenantID,
			ClientID:      authentication.ClientID,
			TokenFilePath: authentication.OIDCTokenFilePath,
		})
	case AuthenticationManagedIdentity:
		options := &azidentity.ManagedIdentityCredentialOptions{ClientOptions: clientOptions}
		if authentication.ClientID != "" {
			options.ID = azidentity.ClientID(authentication.ClientID)
		}
		credential, err = azidentity.NewManagedIdentityCredential(options)
	case AuthenticationAzureCLI:
		credential, err = azidentity.NewAzureCLICredential(&azidentity.AzureCLICredentialOptions{
			TenantID: authentication.TenantID,
		})
	default:
		return nil, fmt.Errorf("unsupported authentication method %q", authentication.Method)
	}
	if err != nil {
		return nil, fmt.Errorf("create %s credential: %w", authentication.Method, err)
	}
	return &authenticationTransport{base: base, credential: credential, tokenScope: environment.tokenScope}, nil
}

type authenticationTransport struct {
	base       http.RoundTripper
	apiKey     string
	credential azcore.TokenCredential
	tokenScope string
}

func (t *authenticationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	cloned := request.Clone(request.Context())
	cloned.Header = request.Header.Clone()

	if t.apiKey != "" {
		cloned.Header.Set("api-key", t.apiKey)
	} else {
		token, err := t.credential.GetToken(request.Context(), policy.TokenRequestOptions{
			Scopes: []string{t.tokenScope},
		})
		if err != nil {
			return nil, fmt.Errorf("get Microsoft Entra token: %w", err)
		}
		cloned.Header.Set("Authorization", "Bearer "+token.Token)
	}

	return t.base.RoundTrip(cloned)
}

type retryTransport struct {
	base       http.RoundTripper
	maxRetries int
}

func (t *retryTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		current := request.Clone(request.Context())
		current.Header = request.Header.Clone()
		if attempt > 0 && request.Body != nil {
			if request.GetBody == nil {
				return nil, errors.New("request body cannot be replayed for retry")
			}
			body, err := request.GetBody()
			if err != nil {
				return nil, fmt.Errorf("recreate request body: %w", err)
			}
			current.Body = body
		} else {
			current.Body = request.Body
		}

		response, err := t.base.RoundTrip(current)
		if attempt >= t.maxRetries || !shouldRetry(response, err) || (request.Body != nil && request.GetBody == nil) {
			return response, err
		}
		if response != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
			_ = response.Body.Close()
		}

		if err := waitForRetry(request.Context(), retryDelay(response, attempt)); err != nil {
			return nil, err
		}
	}
}

func shouldRetry(response *http.Response, err error) bool {
	if err != nil {
		return true
	}
	switch response.StatusCode {
	case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func retryDelay(response *http.Response, attempt int) time.Duration {
	if response != nil {
		if raw := response.Header.Get("Retry-After"); raw != "" {
			if seconds, err := strconv.Atoi(raw); err == nil && seconds >= 0 {
				return time.Duration(seconds) * time.Second
			}
			if when, err := http.ParseTime(raw); err == nil {
				return max(time.Until(when), 0)
			}
		}
	}

	base := 100 * time.Millisecond * time.Duration(1<<attempt)
	return base + time.Duration(randv2.Int64N(int64(base)))
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
