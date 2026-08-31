package clients

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestClientRequestAndRetry(t *testing.T) {
	t.Parallel()

	var attempts int
	var requestID string
	transport := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		attempts++
		if request.URL.String() != "https://example.services.ai.azure.com/api/projects/demo/agents?api-version=v1" {
			t.Errorf("request URL = %q", request.URL.String())
		}
		if request.Header.Get("api-key") != "secret" {
			t.Error("api-key header was not set")
		}
		if request.Header.Get("User-Agent") != "terraform-provider-foundry/test" {
			t.Errorf("User-Agent = %q", request.Header.Get("User-Agent"))
		}
		if attempts == 1 {
			requestID = request.Header.Get("x-ms-client-request-id")
		} else if request.Header.Get("x-ms-client-request-id") != requestID {
			t.Error("client request ID changed between retries")
		}

		status := http.StatusOK
		headers := make(http.Header)
		if attempts == 1 {
			status = http.StatusTooManyRequests
			headers.Set("Retry-After", "0")
		}
		return &http.Response{
			StatusCode: status,
			Header:     headers,
			Body:       io.NopCloser(strings.NewReader("{}")),
			Request:    request,
		}, nil
	})

	client, err := New(Config{
		AccountName: "example",
		ProjectName: "demo",
		Environment: EnvironmentPublic,
		UserAgent:   "terraform-provider-foundry/test",
		Transport:   transport,
		Auth: Authentication{
			Method: AuthenticationAPIKey,
			APIKey: "secret",
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	request, err := client.NewRequest(context.Background(), http.MethodGet, "agents", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close response body: %v", err)
	}

	if attempts != 2 {
		t.Errorf("attempts = %d, want 2", attempts)
	}
	if requestID == "" {
		t.Error("x-ms-client-request-id was not set")
	}
}

func TestRetryTransportMethods(t *testing.T) {
	t.Parallel()

	tests := []struct {
		method   string
		attempts int
	}{
		{method: http.MethodGet, attempts: 2},
		{method: http.MethodHead, attempts: 2},
		{method: http.MethodOptions, attempts: 2},
		{method: http.MethodPut, attempts: 2},
		{method: http.MethodPatch, attempts: 2},
		{method: http.MethodDelete, attempts: 2},
		{method: http.MethodTrace, attempts: 2},
		{method: http.MethodPost, attempts: 1},
	}

	for _, test := range tests {
		t.Run(test.method, func(t *testing.T) {
			t.Parallel()

			var attempts int
			transport := retryTransport{
				maxRetries: 1,
				base: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
					attempts++
					status := http.StatusServiceUnavailable
					if attempts > 1 {
						status = http.StatusOK
					}
					return &http.Response{
						StatusCode: status,
						Header:     make(http.Header),
						Body:       io.NopCloser(strings.NewReader("{}")),
						Request:    request,
					}, nil
				}),
			}

			request, err := http.NewRequestWithContext(context.Background(), test.method, "https://example.test", nil)
			if err != nil {
				t.Fatalf("NewRequestWithContext() error = %v", err)
			}
			response, err := transport.RoundTrip(request)
			if err != nil {
				t.Fatalf("RoundTrip() error = %v", err)
			}
			if err := response.Body.Close(); err != nil {
				t.Fatalf("close response body: %v", err)
			}
			if attempts != test.attempts {
				t.Errorf("attempts = %d, want %d", attempts, test.attempts)
			}
		})
	}
}

func TestRetryTransportContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	var attempts int
	transport := retryTransport{
		maxRetries: 3,
		base: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			attempts++
			cancel()
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Header:     http.Header{"Retry-After": []string{"60"}},
				Body:       io.NopCloser(strings.NewReader("{}")),
				Request:    request,
			}, nil
		}),
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.test", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	response, err := transport.RoundTrip(request)
	if response != nil {
		t.Fatal("RoundTrip() response was not nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RoundTrip() error = %v, want %v", err, context.Canceled)
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1", attempts)
	}
}

func TestEnvironmentConfiguration(t *testing.T) {
	t.Parallel()

	tests := map[Environment]struct {
		endpoint string
		scope    string
	}{
		EnvironmentPublic: {
			endpoint: "https://account.services.ai.azure.com/api/projects/project/agents?api-version=v1",
			scope:    "https://ai.azure.com/.default",
		},
		EnvironmentUSGovernment: {
			endpoint: "https://account.services.ai.azure.us/api/projects/project/agents?api-version=v1",
			scope:    "https://ai.azure.us/.default",
		},
		EnvironmentChina: {
			endpoint: "https://account.services.ai.azure.cn/api/projects/project/agents?api-version=v1",
			scope:    "https://ai.azure.cn/.default",
		},
	}

	for environment, test := range tests {
		t.Run(string(environment), func(t *testing.T) {
			t.Parallel()

			configuration, err := configurationForEnvironment(environment)
			if err != nil {
				t.Fatalf("configurationForEnvironment() error = %v", err)
			}
			if configuration.tokenScope != test.scope {
				t.Errorf("token scope = %q, want %q", configuration.tokenScope, test.scope)
			}

			client, err := New(Config{
				AccountName: "account",
				ProjectName: "project",
				Environment: environment,
				Auth: Authentication{
					Method: AuthenticationAPIKey,
					APIKey: "secret",
				},
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			request, err := client.NewRequest(context.Background(), http.MethodGet, "agents", nil)
			if err != nil {
				t.Fatalf("NewRequest() error = %v", err)
			}
			if request.URL.String() != test.endpoint {
				t.Errorf("request URL = %q, want %q", request.URL.String(), test.endpoint)
			}
		})
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestOpenAIRoutesOmitAPIVersion(t *testing.T) {
	t.Parallel()

	client, err := New(Config{
		AccountName: "account",
		ProjectName: "project",
		Environment: EnvironmentPublic,
		Auth:        Authentication{Method: AuthenticationAPIKey, APIKey: "secret"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tests := map[string]string{
		"openai/v1/files": "https://account.services.ai.azure.com/api/projects/project/openai/v1/files",
		"agents":          "https://account.services.ai.azure.com/api/projects/project/agents?api-version=v1",
	}
	for path, want := range tests {
		request, err := client.NewRequest(context.Background(), http.MethodGet, path, nil)
		if err != nil {
			t.Fatalf("NewRequest(%q) error = %v", path, err)
		}
		if request.URL.String() != want {
			t.Errorf("NewRequest(%q) URL = %q, want %q", path, request.URL.String(), want)
		}
	}
}

func TestPreviewFeatureHeader(t *testing.T) {
	t.Parallel()

	client, err := New(Config{
		AccountName:     "account",
		ProjectName:     "project",
		Environment:     EnvironmentPublic,
		Auth:            Authentication{Method: AuthenticationAPIKey, APIKey: "secret"},
		PreviewFeatures: []string{"evaluations"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if !client.PreviewEnabled("evaluations") {
		t.Fatal("PreviewEnabled(evaluations) = false, want true")
	}
	if client.PreviewEnabled("memory_stores") {
		t.Fatal("PreviewEnabled(memory_stores) = true, want false")
	}

	if _, err := client.WithPreviewFeature(context.Background(), "memory_stores", "MemoryStores"); err == nil {
		t.Fatal("WithPreviewFeature(memory_stores) error = nil, want error for unopted-in feature")
	}

	ctx, err := client.WithPreviewFeature(context.Background(), "evaluations", "Evaluations")
	if err != nil {
		t.Fatalf("WithPreviewFeature(evaluations) error = %v", err)
	}
	request, err := client.NewRequest(ctx, http.MethodGet, "evaluationrules/id", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	if got := request.Header.Get("Foundry-Features"); got != "Evaluations=V1Preview" {
		t.Errorf("Foundry-Features header = %q, want %q", got, "Evaluations=V1Preview")
	}

	plainRequest, err := client.NewRequest(context.Background(), http.MethodGet, "evaluationrules/id", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	if got := plainRequest.Header.Get("Foundry-Features"); got != "" {
		t.Errorf("Foundry-Features header = %q, want empty without preview context", got)
	}
}
