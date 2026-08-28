package provider

import (
	"testing"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

// TestPreviewGateUsesEnablePreviewName pins the distinction between the
// enable_preview value a user writes and the Foundry-Features header token.
// Gating on the header token instead would reject every correctly opted-in
// configuration.
func TestPreviewGateUsesEnablePreviewName(t *testing.T) {
	t.Parallel()

	client, err := clients.New(clients.Config{
		AccountName:     "account",
		ProjectName:     "project",
		Environment:     clients.EnvironmentPublic,
		Auth:            clients.Authentication{Method: clients.AuthenticationAPIKey, APIKey: "secret"},
		PreviewFeatures: []string{"memory_stores"},
	})
	if err != nil {
		t.Fatalf("clients.New() error = %v", err)
	}

	gate := previewGate{client: client, feature: "MemoryStores", name: "memory_stores"}

	ctx, err := gate.previewContext(t.Context())
	if err != nil {
		t.Fatalf("previewContext() error = %v, want nil for an opted-in feature", err)
	}
	request, err := client.NewRequest(ctx, "GET", "memory_stores", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	if got := request.Header.Get("Foundry-Features"); got != "MemoryStores=V1Preview" {
		t.Errorf("Foundry-Features = %q, want %q", got, "MemoryStores=V1Preview")
	}

	notEnabled := previewGate{client: client, feature: "Skills", name: "skills"}
	if _, err := notEnabled.previewContext(t.Context()); err == nil {
		t.Error("previewContext() error = nil, want error for a feature not in enable_preview")
	}
}
