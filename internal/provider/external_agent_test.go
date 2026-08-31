package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

func TestExternalAgentDefinitionRoundTrip(t *testing.T) {
	t.Parallel()

	model := externalAgentModel{
		OtelAgentID: types.StringValue("my-agent"),
		RAIConfig: types.ObjectValueMust(raiConfigAttrTypes, map[string]attr.Value{
			"rai_policy_name": types.StringValue("strict"),
		}),
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
	if definition.OtelAgentID != "my-agent" {
		t.Errorf("otel_agent_id = %q", definition.OtelAgentID)
	}
	if definition.RAIConfig == nil || definition.RAIConfig.PolicyName != "strict" {
		t.Errorf("rai_config = %+v", definition.RAIConfig)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("unmarshal fields: %v", err)
	}
	if _, ok := fields["endpoint"]; ok {
		t.Error("obsolete endpoint field was sent")
	}

	var diagnostics diag.Diagnostics
	var applied externalAgentModel
	applied.apply(context.Background(), agentVersion{ID: "a:1", Version: "1"}, nil, definition, &diagnostics)
	if diagnostics.HasError() {
		t.Fatalf("apply diagnostics: %v", diagnostics)
	}
	if applied.OtelAgentID.ValueString() != "my-agent" {
		t.Errorf("applied otel_agent_id = %q", applied.OtelAgentID.ValueString())
	}
	if applied.RAIConfig.Attributes()["rai_policy_name"].(types.String).ValueString() != "strict" {
		t.Errorf("applied rai_config = %+v", applied.RAIConfig)
	}
	if !applied.AgentEndpoint.IsNull() {
		t.Error("agent_endpoint should be null when the service returns no endpoint")
	}
}

func TestExternalAgentDefinitionDefaultsOtelAgentIDToName(t *testing.T) {
	t.Parallel()

	model := externalAgentModel{
		agentCommon: agentCommon{Name: types.StringValue("my-agent")},
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

func TestExternalAgentDataSourcePreviewBehavior(t *testing.T) {
	t.Parallel()

	disabled, err := clients.New(clients.Config{
		AccountName: "acct",
		ProjectName: "proj",
		Environment: clients.EnvironmentPublic,
		Auth:        clients.Authentication{Method: clients.AuthenticationAPIKey, APIKey: "key"},
	})
	if err != nil {
		t.Fatalf("build disabled client: %v", err)
	}
	dataSource := NewExternalAgentDataSource().(*agentDataSource)
	dataSource.client = disabled
	dataSource.feature = "ExternalAgents"
	dataSource.name = "external_agents"
	var validation datasource.ValidateConfigResponse
	dataSource.ValidateConfig(context.Background(), datasource.ValidateConfigRequest{}, &validation)
	if !validation.Diagnostics.HasError() {
		t.Fatal("expected external data source preview validation error")
	}

	enabled := newAgentTestClient(t, []string{"external_agents"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Foundry-Features"); got != "ExternalAgents=V1Preview" {
			t.Errorf("Foundry-Features = %q", got)
		}
		_, _ = fmt.Fprint(w, `{"name":"external-agent","versions":{"latest":{"id":"external-agent:1","version":"1","created_at":1,"definition":{"kind":"external","otel_agent_id":"external-agent"}}}}`)
	}))
	dataSource.client = enabled
	var diagnostics diag.Diagnostics
	var definition externalAgentDefinition
	if _, ok := dataSource.read(context.Background(), "external-agent", &definition, &diagnostics); !ok || diagnostics.HasError() {
		t.Fatalf("read diagnostics: %v", diagnostics)
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
