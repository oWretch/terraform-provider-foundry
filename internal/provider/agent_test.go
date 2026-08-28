package provider

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestPromptAgentDefinitionRoundTrip(t *testing.T) {
	t.Parallel()

	model := promptAgentModel{
		Model:        types.StringValue("gpt-4.1"),
		Instructions: types.StringValue("Be terse."),
	}
	encoded, err := json.Marshal(model.definition())
	if err != nil {
		t.Fatalf("marshal definition: %v", err)
	}

	var applied promptAgentModel
	var definition promptAgentDefinition
	if err := json.Unmarshal(encoded, &definition); err != nil {
		t.Fatalf("unmarshal definition: %v", err)
	}
	var diagnostics diag.Diagnostics
	applied.apply(context.Background(), agentVersion{ID: "a:1", Version: "1"}, nil, definition, &diagnostics)
	if diagnostics.HasError() {
		t.Fatalf("apply diagnostics: %v", diagnostics)
	}

	if definition.Kind != "prompt" {
		t.Errorf("kind = %q, want prompt", definition.Kind)
	}
	if applied.Model.ValueString() != "gpt-4.1" {
		t.Errorf("model = %q", applied.Model.ValueString())
	}
	if applied.Instructions.ValueString() != "Be terse." {
		t.Errorf("instructions = %q", applied.Instructions.ValueString())
	}
	// An absent description must stay null so Terraform does not report a permanent diff.
	if !applied.Description.IsNull() {
		t.Error("empty description should be null")
	}
}

func TestHostedAgentDefinitionRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	variables, diags := types.MapValueFrom(ctx, types.StringType, map[string]string{"LOG_LEVEL": "debug"})
	if diags.HasError() {
		t.Fatalf("build map: %v", diags)
	}

	model := hostedAgentModel{
		Image:                types.StringValue("registry/repo:1"),
		CPU:                  types.StringValue("1"),
		Memory:               types.StringValue("2Gi"),
		EnvironmentVariables: variables,
		ProtocolVersions: []protocolVersionModel{
			{Protocol: types.StringValue("responses"), Version: types.StringValue("1")},
		},
	}

	var diagnostics diag.Diagnostics
	encoded, err := json.Marshal(model.definition(ctx, &diagnostics))
	if err != nil {
		t.Fatalf("marshal definition: %v", err)
	}
	if diagnostics.HasError() {
		t.Fatalf("definition diagnostics: %v", diagnostics)
	}

	var definition hostedAgentDefinition
	if err := json.Unmarshal(encoded, &definition); err != nil {
		t.Fatalf("unmarshal definition: %v", err)
	}

	var applied hostedAgentModel
	applied.apply(ctx, agentVersion{ID: "a:1", Version: "1"}, nil, definition, &diagnostics)
	if diagnostics.HasError() {
		t.Fatalf("apply diagnostics: %v", diagnostics)
	}

	if definition.Kind != "hosted" {
		t.Errorf("kind = %q, want hosted", definition.Kind)
	}
	if applied.Memory.ValueString() != "2Gi" {
		t.Errorf("memory = %q", applied.Memory.ValueString())
	}
	if len(applied.ProtocolVersions) != 1 || applied.ProtocolVersions[0].Protocol.ValueString() != "responses" {
		t.Errorf("protocol versions = %+v", applied.ProtocolVersions)
	}
	if applied.EnvironmentVariables.IsNull() {
		t.Error("environment variables should be preserved")
	}
}

func TestHostedAgentEmptyEnvironmentIsNull(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var diagnostics diag.Diagnostics
	var applied hostedAgentModel
	applied.apply(ctx, agentVersion{}, nil, hostedAgentDefinition{Kind: "hosted"}, &diagnostics)

	if !applied.EnvironmentVariables.IsNull() {
		t.Error("absent environment variables should be null")
	}
}
