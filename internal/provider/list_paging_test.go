package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

type pagedItem struct {
	ID string `json:"id"`
}

// newPagingTestClient points a real client at a test server, so the paging walk
// is exercised through the same request path production uses.
func newPagingTestClient(t *testing.T, handler http.Handler) *clients.Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := clients.New(clients.Config{
		AccountName: "test",
		ProjectName: "default",
		Environment: clients.EnvironmentPublic,
		Auth:        clients.Authentication{Method: clients.AuthenticationAPIKey, APIKey: "key"},
		Transport:   rewriteHostTransport{target: server.Listener.Addr().String()},
	})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}
	return client
}

// rewriteHostTransport sends every request to the test server regardless of the
// endpoint the client built, so no real host is contacted.
type rewriteHostTransport struct {
	target string
}

func (t rewriteHostTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request.URL.Scheme = "http"
	request.URL.Host = t.target
	return http.DefaultTransport.RoundTrip(request)
}

func TestListAllPagesFollowsCursor(t *testing.T) {
	var seenAfter []string
	client := newPagingTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		after := r.URL.Query().Get("after")
		seenAfter = append(seenAfter, after)
		switch after {
		case "":
			_, _ = fmt.Fprint(w, `{"data":[{"id":"a"}],"has_more":true,"last_id":"a"}`)
		case "a":
			_, _ = fmt.Fprint(w, `{"data":[{"id":"b"}],"has_more":true,"last_id":"b"}`)
		default:
			_, _ = fmt.Fprint(w, `{"data":[{"id":"c"}],"has_more":false,"last_id":"c"}`)
		}
	}))

	items, err := listAllPages[pagedItem](context.Background(), client, "openai/v1/files")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := len(items); got != 3 {
		t.Fatalf("got %d items, want 3", got)
	}
	if items[0].ID != "a" || items[2].ID != "c" {
		t.Fatalf("items out of order: %+v", items)
	}
	if len(seenAfter) != 3 {
		t.Fatalf("expected 3 requests, got %d", len(seenAfter))
	}
}

// A single page that sets has_more is exactly what the live service returns, so
// stopping on it must not depend on has_more alone.
func TestListAllPagesStopsWhenCursorCannotAdvance(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		wantItems int
		wantCalls int
	}{
		{
			// The live service returns exactly this for a single-item list.
			name:      "empty last_id",
			body:      `{"data":[{"id":"a"}],"has_more":true,"last_id":""}`,
			wantItems: 1,
			wantCalls: 1,
		},
		{
			// A cursor that does not move would otherwise re-read one page
			// forever, appending the same item on every pass.
			name:      "last_id never advances",
			body:      `{"data":[{"id":"a"}],"has_more":true,"last_id":"a"}`,
			wantItems: 2,
			wantCalls: 2,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var calls int
			client := newPagingTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				if calls > maxListPages {
					t.Fatal("paging walk did not terminate")
				}
				_, _ = fmt.Fprint(w, testCase.body)
			}))

			items, err := listAllPages[pagedItem](context.Background(), client, "openai/v1/files")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(items) != testCase.wantItems {
				t.Fatalf("got %d items, want %d", len(items), testCase.wantItems)
			}
			if calls != testCase.wantCalls {
				t.Fatalf("expected %d requests, got %d", testCase.wantCalls, calls)
			}
		})
	}
}

// A service that always advances the cursor and never clears has_more must be
// bounded rather than spinning an apply forever.
func TestListAllPagesIsBounded(t *testing.T) {
	var calls int
	client := newPagingTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = fmt.Fprintf(w, `{"data":[{"id":"i%d"}],"has_more":true,"last_id":"i%d"}`, calls, calls)
	}))

	_, err := listAllPages[pagedItem](context.Background(), client, "openai/v1/files")
	if err == nil {
		t.Fatal("expected an error when the cursor never finishes")
	}
	if !strings.Contains(err.Error(), "did not finish") {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != maxListPages {
		t.Fatalf("expected %d requests, got %d", maxListPages, calls)
	}
}
