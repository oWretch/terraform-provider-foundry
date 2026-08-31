package clients

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestHostMatches(t *testing.T) {
	cases := []struct {
		name   string
		host   string
		suffix string
		want   bool
	}{
		{"exact", "api.azureml.ms", "api.azureml.ms", true},
		{"subdomain", "australiaeast.api.azureml.ms", "api.azureml.ms", true},
		{"case insensitive", "AustraliaEast.API.AzureML.MS", "api.azureml.ms", true},
		{"trailing dot", "australiaeast.api.azureml.ms.", "api.azureml.ms", true},
		{"lookalike suffix", "notapi.azureml.ms.example.com", "api.azureml.ms", false},
		{"missing separator", "evilapi.azureml.ms", "api.azureml.ms", false},
		{"unrelated host", "example.com", "api.azureml.ms", false},
		{"empty suffix", "api.azureml.ms", "", false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := hostMatches(testCase.host, testCase.suffix); got != testCase.want {
				t.Fatalf("hostMatches(%q, %q) = %v, want %v", testCase.host, testCase.suffix, got, testCase.want)
			}
		})
	}
}

func TestOperationURL(t *testing.T) {
	client := newOperationTestClient(t, "https://example.services.ai.azure.com/api/projects/default")

	cases := []struct {
		name     string
		location string
		wantErr  string
	}{
		{name: "operation host", location: "https://australiaeast.api.azureml.ms/operations/1"},
		{name: "project endpoint", location: "https://example.services.ai.azure.com/operations/1"},
		{name: "http rejected", location: "http://australiaeast.api.azureml.ms/operations/1", wantErr: "must use https"},
		{name: "foreign host rejected", location: "https://evil.example.com/operations/1", wantErr: "is neither the project endpoint"},
		{name: "lookalike rejected", location: "https://notapi.azureml.ms.evil.com/operations/1", wantErr: "is neither the project endpoint"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := client.operationURL(testCase.location)
			if testCase.wantErr != "" {
				if err == nil {
					t.Fatalf("expected an error containing %q, got %q", testCase.wantErr, got)
				}
				if !strings.Contains(err.Error(), testCase.wantErr) {
					t.Fatalf("error %q does not contain %q", err, testCase.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != testCase.location {
				t.Fatalf("got %q, want %q", got, testCase.location)
			}
		})
	}
}

func TestWaitForOperationPollsUntilComplete(t *testing.T) {
	var calls int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"name": "done"})
	}))
	t.Cleanup(server.Close)

	client := newOperationTestClient(t, server.URL)
	client.httpClient = server.Client()

	var result struct {
		Name string `json:"name"`
	}
	if err := client.WaitForOperation(context.Background(), server.URL+"/operations/1", &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Name != "done" {
		t.Fatalf("got %q, want %q", result.Name, "done")
	}
	if calls != 2 {
		t.Fatalf("expected 2 polls, got %d", calls)
	}
}

func TestWaitForOperationStopsOnCancelledContext(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(server.Close)

	client := newOperationTestClient(t, server.URL)
	client.httpClient = server.Client()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := client.WaitForOperation(ctx, server.URL+"/operations/1", nil)
	if err == nil {
		t.Fatal("expected the poll loop to stop when the context is cancelled")
	}
}

func TestRetryAfterIsCapped(t *testing.T) {
	response := &http.Response{Header: http.Header{}}
	response.Header.Set("Retry-After", "600")

	if got := retryAfter(response); got != maxOperationPollInterval {
		t.Fatalf("got %s, want %s", got, maxOperationPollInterval)
	}

	response.Header.Del("Retry-After")
	if got := retryAfter(response); got != operationPollInterval {
		t.Fatalf("got %s, want the default %s", got, operationPollInterval)
	}
}

// newOperationTestClient builds a Client directly so that the polling logic can
// be exercised without a credential or a live endpoint.
func newOperationTestClient(t *testing.T, endpoint string) *Client {
	t.Helper()

	parsed, err := url.Parse(endpoint)
	if err != nil {
		t.Fatalf("parse endpoint: %v", err)
	}
	return &Client{
		endpoint:            parsed,
		apiVersion:          "v1",
		userAgent:           "test",
		httpClient:          &http.Client{},
		operationHostSuffix: "api.azureml.ms",
	}
}
