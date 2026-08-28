package clients

import (
	"context"
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
