package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
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
		Object:      "memory_store",
		ID:          "mem_123",
		Name:        "store1",
		Description: pointer("test store"),
		Metadata:    pointer(map[string]string{"team": "platform"}),
		CreatedAt:   1000,
		UpdatedAt:   2000,
		Definition: memoryStoreDefinitionResponse{
			Kind:           "default",
			ChatModel:      "gpt-4.1",
			EmbeddingModel: "text-embedding-3-large",
			Options: &memoryStoreDefaultOptionsResponse{
				UserProfileEnabled:      pointer(true),
				UserProfileDetails:      "preferences",
				ChatSummaryEnabled:      pointer(true),
				ProceduralMemoryEnabled: pointer(true),
				DefaultTTLSeconds:       pointer(int64(0)),
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
	if model.UpdatedAt.ValueInt64() != 2000 {
		t.Errorf("updated_at = %d", model.UpdatedAt.ValueInt64())
	}
	if model.ChatModel.ValueString() != "gpt-4.1" {
		t.Errorf("chat_model = %q", model.ChatModel.ValueString())
	}
	if model.Options.IsNull() {
		t.Fatal("options should not be null")
	}
}

func pointer[T any](value T) *T {
	return &value
}

func TestMemoryStoreUpdateCanClearMutableFields(t *testing.T) {
	t.Parallel()

	request := memoryStoreModel{
		Description: types.StringNull(),
		Metadata:    types.MapNull(types.StringType),
	}.updateRequest(context.Background(), &diag.Diagnostics{})
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if got := string(encoded); got != `{"description":"","metadata":{}}` {
		t.Fatalf("request = %s", got)
	}
}

func TestMemoryStoreApplyPreservesConfiguredEmptyValues(t *testing.T) {
	t.Parallel()

	empty := ""
	emptyMetadata := map[string]string{}
	response := memoryStoreResponse{
		Object:      "memory_store",
		Description: &empty,
		Metadata:    &emptyMetadata,
		Definition: memoryStoreDefinitionResponse{
			Kind: "default",
		},
	}

	nullModel := memoryStoreModel{
		Description: types.StringNull(),
		Metadata:    types.MapNull(types.StringType),
	}
	nullModel.apply(context.Background(), response, &diag.Diagnostics{})
	if !nullModel.Description.IsNull() || !nullModel.Metadata.IsNull() {
		t.Fatalf("null values became %q, %v", nullModel.Description.ValueString(), nullModel.Metadata)
	}

	emptyModel := memoryStoreModel{
		Description: types.StringValue(""),
		Metadata:    types.MapValueMust(types.StringType, map[string]attr.Value{}),
	}
	emptyModel.apply(context.Background(), response, &diag.Diagnostics{})
	if emptyModel.Description.IsNull() || emptyModel.Metadata.IsNull() {
		t.Fatalf("configured empty values became null: %v, %v", emptyModel.Description, emptyModel.Metadata)
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

	for _, name := range []string{"name", "description", "metadata", "chat_model", "embedding_model", "options", "id", "created_at", "updated_at"} {
		if _, ok := schemaResp.Schema.Attributes[name]; !ok {
			t.Errorf("schema missing attribute %q", name)
		}
	}
}

func TestMemoryStoreRejectsUnsupportedResponse(t *testing.T) {
	t.Parallel()

	tests := []memoryStoreResponse{
		{Object: "memory_store.deleted", Definition: memoryStoreDefinitionResponse{Kind: "default"}},
		{Object: "memory_store", Definition: memoryStoreDefinitionResponse{Kind: "external"}},
	}
	for _, response := range tests {
		var model memoryStoreModel
		var diagnostics diag.Diagnostics
		model.apply(context.Background(), response, &diagnostics)
		if !diagnostics.HasError() {
			t.Fatalf("response %+v was accepted", response)
		}
	}
}

func TestMemoryStoreDataSourceRead(t *testing.T) {
	t.Parallel()

	dataSource := NewMemoryStoreDataSource().(*memoryStoreDataSource)
	dataSource.previewGate = previewGate{
		client: newAgentTestClient(t, []string{memoryStoresPreviewFeatureName}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.EscapedPath() != "/api/projects/default/memory_stores/store%2Fone" {
				t.Errorf("request = %s %s", r.Method, r.RequestURI)
			}
			if got := r.Header.Get("Foundry-Features"); got != "MemoryStores=V1Preview" {
				t.Errorf("Foundry-Features = %q", got)
			}
			_, _ = fmt.Fprint(w, `{
						"object":"memory_store",
						"id":"mem_123",
						"created_at":1000,
						"updated_at":2000,
						"name":"store/one",
						"description":"test store",
						"metadata":{"team":"platform"},
						"definition":{
							"kind":"default",
							"chat_model":"gpt-4.1",
							"embedding_model":"text-embedding-3-large",
							"options":{
								"user_profile_enabled":true,
								"user_profile_details":"preferences",
								"chat_summary_enabled":false,
								"procedural_memory_enabled":true,
								"default_ttl_seconds":3600
							}
						}
					}`)
		})),
		feature: memoryStoresPreviewFeature,
		name:    memoryStoresPreviewFeatureName,
	}

	config := memoryStoreDataSourceModel{
		Name:           types.StringValue("store/one"),
		Object:         types.StringNull(),
		ID:             types.StringNull(),
		Description:    types.StringNull(),
		Metadata:       types.MapNull(types.StringType),
		CreatedAt:      types.Int64Null(),
		UpdatedAt:      types.Int64Null(),
		Kind:           types.StringNull(),
		ChatModel:      types.StringNull(),
		EmbeddingModel: types.StringNull(),
		Options:        types.ObjectNull(memoryStoreOptionsAttributeTypes),
	}
	state, diagnostics := readAssetDataSource(t, dataSource, config)
	if diagnostics.HasError() {
		t.Fatalf("Read diagnostics: %v", diagnostics)
	}
	if state.UpdatedAt.ValueInt64() != 2000 || state.Kind.ValueString() != "default" || state.ChatModel.ValueString() != "gpt-4.1" {
		t.Errorf("state = %+v", state)
	}
}

func TestMemoryStoreDataSourceRejectsWrongKinds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		response string
		want     string
	}{
		{name: "object", response: `{"object":"memory_store.deleted","name":"store","deleted":true}`, want: "memory_store.deleted"},
		{name: "kind", response: `{"object":"memory_store","name":"store","definition":{"kind":"external"}}`, want: "external"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dataSource := NewMemoryStoreDataSource().(*memoryStoreDataSource)
			dataSource.previewGate = previewGate{
				client: newAgentTestClient(t, []string{memoryStoresPreviewFeatureName}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					_, _ = fmt.Fprint(w, test.response)
				})),
				feature: memoryStoresPreviewFeature,
				name:    memoryStoresPreviewFeatureName,
			}
			config := memoryStoreDataSourceModel{
				Name:           types.StringValue("store"),
				Object:         types.StringNull(),
				ID:             types.StringNull(),
				Description:    types.StringNull(),
				Metadata:       types.MapNull(types.StringType),
				CreatedAt:      types.Int64Null(),
				UpdatedAt:      types.Int64Null(),
				Kind:           types.StringNull(),
				ChatModel:      types.StringNull(),
				EmbeddingModel: types.StringNull(),
				Options:        types.ObjectNull(memoryStoreOptionsAttributeTypes),
			}
			_, diagnostics := readAssetDataSource(t, dataSource, config)
			if !diagnostics.HasError() || !strings.Contains(diagnostics.Errors()[0].Detail(), test.want) {
				t.Fatalf("diagnostics = %v", diagnostics)
			}
		})
	}
}

func TestMemoryStoreDataSourceNotFound(t *testing.T) {
	t.Parallel()

	dataSource := NewMemoryStoreDataSource().(*memoryStoreDataSource)
	dataSource.previewGate = previewGate{
		client: newAgentTestClient(t, []string{memoryStoresPreviewFeatureName}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "missing", http.StatusNotFound)
		})),
		feature: memoryStoresPreviewFeature,
		name:    memoryStoresPreviewFeatureName,
	}
	config := memoryStoreDataSourceModel{
		Name:           types.StringValue("missing"),
		Object:         types.StringNull(),
		ID:             types.StringNull(),
		Description:    types.StringNull(),
		Metadata:       types.MapNull(types.StringType),
		CreatedAt:      types.Int64Null(),
		UpdatedAt:      types.Int64Null(),
		Kind:           types.StringNull(),
		ChatModel:      types.StringNull(),
		EmbeddingModel: types.StringNull(),
		Options:        types.ObjectNull(memoryStoreOptionsAttributeTypes),
	}
	_, diagnostics := readAssetDataSource(t, dataSource, config)
	if !diagnostics.HasError() || !strings.Contains(diagnostics.Errors()[0].Summary(), "not found") {
		t.Fatalf("diagnostics = %v", diagnostics)
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

func TestMemoryStoreDataSourceValidateConfig(t *testing.T) {
	t.Parallel()

	client, err := clients.New(clients.Config{
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

	dataSource := &memoryStoreDataSource{previewGate: previewGate{
		client:  client,
		feature: memoryStoresPreviewFeature,
		name:    memoryStoresPreviewFeatureName,
	}}
	response := &datasource.ValidateConfigResponse{}
	dataSource.ValidateConfig(context.Background(), datasource.ValidateConfigRequest{Config: tfsdk.Config{}}, response)
	if !response.Diagnostics.HasError() {
		t.Fatal("expected preview validation error")
	}
}
