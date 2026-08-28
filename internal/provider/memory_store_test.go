package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

func TestMemoryStoreCreateRequestRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	metadata, diags := types.MapValueFrom(ctx, types.StringType, map[string]string{"team": "platform"})
	if diags.HasError() {
		t.Fatalf("build metadata: %v", diags)
	}
	options, diags := types.ObjectValue(memoryStoreOptionsAttributeTypes, map[string]attr.Value{
		"user_profile_enabled":      types.BoolValue(true),
		"user_profile_details":      types.StringValue("preferences"),
		"chat_summary_enabled":      types.BoolValue(false),
		"procedural_memory_enabled": types.BoolValue(true),
		"default_ttl_seconds":       types.Int64Value(3600),
	})
	if diags.HasError() {
		t.Fatalf("build options: %v", diags)
	}

	model := memoryStoreModel{
		Name:           types.StringValue("store1"),
		Description:    types.StringValue("test store"),
		Metadata:       metadata,
		ChatModel:      types.StringValue("gpt-4.1"),
		EmbeddingModel: types.StringValue("text-embedding-3-large"),
		Options:        options,
	}

	var createDiags diag.Diagnostics
	request := model.createRequest(ctx, &createDiags)
	if createDiags.HasError() {
		t.Fatalf("createRequest diagnostics: %v", createDiags)
	}

	if request.Name != "store1" {
		t.Errorf("name = %q", request.Name)
	}
	if request.Definition.Kind != "default" {
		t.Errorf("kind = %q, want default", request.Definition.Kind)
	}
	if request.Definition.ChatModel != "gpt-4.1" {
		t.Errorf("chat_model = %q", request.Definition.ChatModel)
	}
	if request.Metadata["team"] != "platform" {
		t.Errorf("metadata[team] = %q", request.Metadata["team"])
	}
	if request.Definition.Options == nil {
		t.Fatal("options was not set")
	}
	if request.Definition.Options.ChatSummaryEnabled == nil || *request.Definition.Options.ChatSummaryEnabled {
		t.Errorf("chat_summary_enabled = %v, want false", request.Definition.Options.ChatSummaryEnabled)
	}
	if request.Definition.Options.DefaultTTLSeconds == nil || *request.Definition.Options.DefaultTTLSeconds != 3600 {
		t.Errorf("default_ttl_seconds = %v, want 3600", request.Definition.Options.DefaultTTLSeconds)
	}
}

func TestMemoryStoreApplyRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	response := memoryStoreResponse{
		ID:          "mem_123",
		Name:        "store1",
		Description: "test store",
		Metadata:    map[string]string{"team": "platform"},
		CreatedAt:   1000,
		Definition: memoryStoreDefinitionResponse{
			Kind:           "default",
			ChatModel:      "gpt-4.1",
			EmbeddingModel: "text-embedding-3-large",
			Options: &memoryStoreDefaultOptionsResponse{
				UserProfileEnabled:      true,
				UserProfileDetails:      "preferences",
				ChatSummaryEnabled:      true,
				ProceduralMemoryEnabled: true,
				DefaultTTLSeconds:       0,
			},
		},
	}

	var model memoryStoreModel
	var applyDiags diag.Diagnostics
	model.apply(ctx, response, &applyDiags)
	if applyDiags.HasError() {
		t.Fatalf("apply diagnostics: %v", applyDiags)
	}

	if model.ID.ValueString() != "mem_123" {
		t.Errorf("id = %q", model.ID.ValueString())
	}
	if model.CreatedAt.ValueInt64() != 1000 {
		t.Errorf("created_at = %d", model.CreatedAt.ValueInt64())
	}
	if model.ChatModel.ValueString() != "gpt-4.1" {
		t.Errorf("chat_model = %q", model.ChatModel.ValueString())
	}
	if model.Options.IsNull() {
		t.Fatal("options should not be null")
	}
}

func TestMemoryStoreSchemaAttributes(t *testing.T) {
	t.Parallel()

	r := NewMemoryStoreResource()
	var schemaResp resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %v", schemaResp.Diagnostics)
	}

	for _, name := range []string{"name", "description", "metadata", "chat_model", "embedding_model", "options", "id", "created_at"} {
		if _, ok := schemaResp.Schema.Attributes[name]; !ok {
			t.Errorf("schema missing attribute %q", name)
		}
	}
}

func TestMemoryStoreValidateConfig(t *testing.T) {
	t.Parallel()

	enabledClient, err := clients.New(clients.Config{
		AccountName:     "example",
		ProjectName:     "demo",
		Environment:     clients.EnvironmentPublic,
		PreviewFeatures: []string{memoryStoresPreviewFeatureName},
		Auth: clients.Authentication{
			Method: clients.AuthenticationAPIKey,
			APIKey: "secret",
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	disabledClient, err := clients.New(clients.Config{
		AccountName: "example",
		ProjectName: "demo",
		Environment: clients.EnvironmentPublic,
		Auth: clients.Authentication{
			Method: clients.AuthenticationAPIKey,
			APIKey: "secret",
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tests := []struct {
		name      string
		client    *clients.Client
		wantError bool
	}{
		{name: "enabled", client: enabledClient, wantError: false},
		{name: "disabled", client: disabledClient, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &memoryStoreResource{previewGate: previewGate{client: tt.client, feature: memoryStoresPreviewFeature, name: memoryStoresPreviewFeatureName}}
			resp := &resource.ValidateConfigResponse{}
			r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config{}}, resp)

			if resp.Diagnostics.HasError() != tt.wantError {
				t.Errorf("HasError() = %v, want %v (%v)", resp.Diagnostics.HasError(), tt.wantError, resp.Diagnostics)
			}
		})
	}
}
