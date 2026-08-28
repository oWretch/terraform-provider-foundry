package provider

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

func TestExternalAgentDefinitionRoundTrip(t *testing.T) {
	t.Parallel()

	model := externalAgentModel{
		Endpoint:    types.StringValue("https://example.com/agent"),
		OtelAgentID: types.StringValue("my-agent"),
	}
	encoded, err := json.Marshal(model.definition())
	if err != nil {
		t.Fatalf("marshal definition: %v", err)
	}

	var definition externalAgentDefinition
	if err := json.Unmarshal(encoded, &definition); err != nil {
		t.Fatalf("unmarshal definition: %v", err)
	}
	if definition.Kind != "external" {
		t.Errorf("kind = %q, want external", definition.Kind)
	}
	if definition.Endpoint != "https://example.com/agent" {
		t.Errorf("endpoint = %q", definition.Endpoint)
	}
	if definition.OtelAgentID != "my-agent" {
		t.Errorf("otel_agent_id = %q", definition.OtelAgentID)
	}

	var diagnostics diag.Diagnostics
	var applied externalAgentModel
	applied.apply(context.Background(), agentVersion{ID: "a:1", Version: "1"}, nil, definition, &diagnostics)
	if diagnostics.HasError() {
		t.Fatalf("apply diagnostics: %v", diagnostics)
	}
	if applied.Endpoint.ValueString() != "https://example.com/agent" {
		t.Errorf("applied endpoint = %q", applied.Endpoint.ValueString())
	}
	if applied.OtelAgentID.ValueString() != "my-agent" {
		t.Errorf("applied otel_agent_id = %q", applied.OtelAgentID.ValueString())
	}
	if !applied.AgentEndpoint.IsNull() {
		t.Error("agent_endpoint should be null when the service returns no endpoint")
	}
}

func TestExternalAgentDefinitionDefaultsOtelAgentIDToName(t *testing.T) {
	t.Parallel()

	model := externalAgentModel{
		agentCommon: agentCommon{Name: types.StringValue("my-agent")},
		Endpoint:    types.StringValue("https://example.com/agent"),
		OtelAgentID: types.StringNull(),
	}
	definition := model.definition()
	if definition.OtelAgentID != "my-agent" {
		t.Errorf("otel_agent_id = %q, want my-agent", definition.OtelAgentID)
	}
}

func TestExternalAgentResourceRequiresPreviewFeature(t *testing.T) {
	t.Parallel()

	client, err := clients.New(clients.Config{
		AccountName: "acct",
		ProjectName: "proj",
		Environment: clients.EnvironmentPublic,
		Auth:        clients.Authentication{Method: clients.AuthenticationAPIKey, APIKey: "key"},
	})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}

	r := &externalAgentResource{}
	r.client = client
	r.feature = "ExternalAgents"
	r.name = "external_agents"

	var resp resource.ValidateConfigResponse
	r.ValidateResourceConfig(context.Background(), resource.ValidateConfigRequest{}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error when external_agents preview is not enabled")
	}
}

func TestExternalAgentResourceAllowsEnabledPreviewFeature(t *testing.T) {
	t.Parallel()

	client, err := clients.New(clients.Config{
		AccountName:     "acct",
		ProjectName:     "proj",
		Environment:     clients.EnvironmentPublic,
		Auth:            clients.Authentication{Method: clients.AuthenticationAPIKey, APIKey: "key"},
		PreviewFeatures: []string{"external_agents"},
	})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}

	r := &externalAgentResource{}
	r.client = client
	r.feature = "ExternalAgents"
	r.name = "external_agents"

	var resp resource.ValidateConfigResponse
	r.ValidateResourceConfig(context.Background(), resource.ValidateConfigRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected error when external_agents preview is enabled: %v", resp.Diagnostics)
	}
}

func TestValidateDraftPreviewRejectsDraftWithoutOptIn(t *testing.T) {
	t.Parallel()

	client, err := clients.New(clients.Config{
		AccountName: "acct",
		ProjectName: "proj",
		Environment: clients.EnvironmentPublic,
		Auth:        clients.Authentication{Method: clients.AuthenticationAPIKey, APIKey: "key"},
	})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}

	var diagnostics diag.Diagnostics
	validateDraftPreview(client, types.BoolValue(true), &diagnostics)
	if !diagnostics.HasError() {
		t.Fatal("expected an error when draft = true without draft_agents preview enabled")
	}
}

func TestValidateDraftPreviewAllowsDraftWithOptIn(t *testing.T) {
	t.Parallel()

	client, err := clients.New(clients.Config{
		AccountName:     "acct",
		ProjectName:     "proj",
		Environment:     clients.EnvironmentPublic,
		Auth:            clients.Authentication{Method: clients.AuthenticationAPIKey, APIKey: "key"},
		PreviewFeatures: []string{"draft_agents"},
	})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}

	var diagnostics diag.Diagnostics
	validateDraftPreview(client, types.BoolValue(true), &diagnostics)
	if diagnostics.HasError() {
		t.Fatalf("unexpected error: %v", diagnostics)
	}
}

func TestValidateDraftPreviewIgnoresFalseOrNull(t *testing.T) {
	t.Parallel()

	client, err := clients.New(clients.Config{
		AccountName: "acct",
		ProjectName: "proj",
		Environment: clients.EnvironmentPublic,
		Auth:        clients.Authentication{Method: clients.AuthenticationAPIKey, APIKey: "key"},
	})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}

	var diagnostics diag.Diagnostics
	validateDraftPreview(client, types.BoolValue(false), &diagnostics)
	validateDraftPreview(client, types.BoolNull(), &diagnostics)
	if diagnostics.HasError() {
		t.Fatalf("unexpected error: %v", diagnostics)
	}
}
