package provider

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func toolOf(toolType string) toolModel {
	return toolModel{
		Type:                types.StringValue(toolType),
		Name:                types.StringNull(),
		Description:         types.StringNull(),
		ServerLabel:         types.StringNull(),
		ServerURL:           types.StringNull(),
		ConnectorID:         types.StringNull(),
		RequireApproval:     types.StringNull(),
		AllowedTools:        types.SetNull(types.StringType),
		Headers:             types.MapNull(types.StringType),
		ProjectConnectionID: types.StringNull(),
		BaseURL:             types.StringNull(),
		AgentCardPath:       types.StringNull(),
		A2AVersion:          types.StringNull(),
		SearchContextSize:   types.StringNull(),
		CustomSearch:        types.ObjectNull(customSearchAttrTypes),
		VectorStoreIDs:      types.SetNull(types.StringType),
		MaxNumResults:       types.Int64Null(),
		Index:               types.ObjectNull(indexAttrTypes),
		OpenAPI:             types.ObjectNull(openAPIAttrTypes),
	}
}

func errorSummaries(diagnostics diag.Diagnostics) string {
	var summaries []string
	for _, d := range diagnostics.Errors() {
		summaries = append(summaries, d.Summary()+": "+d.Detail())
	}
	return strings.Join(summaries, " | ")
}

// TestValidateToolsRequiredFields pins the per-type required attributes, so a
// missing one is reported at plan time instead of as a service 400.
func TestValidateToolsRequiredFields(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		tool    toolModel
		wantErr string
	}{
		"mcp without server_label": {tool: toolOf(toolTypeMCP), wantErr: "server_label"},
		"a2a without version":      {tool: toolOf(toolTypeA2A), wantErr: "a2a_version"},
		"work iq without connection": {
			tool: toolOf(toolTypeWorkIQPreview), wantErr: "project_connection_id",
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var diagnostics diag.Diagnostics
			validateTools([]toolModel{testCase.tool}, true, &diagnostics)
			if !strings.Contains(errorSummaries(diagnostics), testCase.wantErr) {
				t.Errorf("expected an error mentioning %q, got %q", testCase.wantErr, errorSummaries(diagnostics))
			}
		})
	}
}

// TestValidateToolsRejectsForeignAttributes ensures an attribute belonging to
// another tool type is reported rather than silently dropped from the request.
func TestValidateToolsRejectsForeignAttributes(t *testing.T) {
	t.Parallel()

	tool := toolOf(toolTypeWebSearch)
	tool.A2AVersion = types.StringValue("1.0")

	var diagnostics diag.Diagnostics
	validateTools([]toolModel{tool}, true, &diagnostics)

	if !strings.Contains(errorSummaries(diagnostics), "a2a_version") {
		t.Errorf("expected web_search to reject a2a_version, got %q", errorSummaries(diagnostics))
	}
}

// TestValidateToolsSingleUnnamedToolPerToolbox pins the service rule that one
// tool in the whole toolbox may omit an identifier, not one per tool type.
func TestValidateToolsSingleUnnamedToolPerToolbox(t *testing.T) {
	t.Parallel()

	var oneUnnamed diag.Diagnostics
	validateTools([]toolModel{toolOf(toolTypeWebSearch)}, true, &oneUnnamed)
	if oneUnnamed.HasError() {
		t.Errorf("a single unnamed tool should be allowed, got %q", errorSummaries(oneUnnamed))
	}

	var twoUnnamed diag.Diagnostics
	validateTools([]toolModel{toolOf(toolTypeWebSearch), toolOf(toolTypeCodeInterpreter)}, true, &twoUnnamed)
	if !strings.Contains(errorSummaries(twoUnnamed), "without a name") {
		t.Errorf("two unnamed tools of different types should be rejected, got %q", errorSummaries(twoUnnamed))
	}

	// An mcp tool is identified by server_label, so it does not count as unnamed.
	mcp := toolOf(toolTypeMCP)
	mcp.ServerLabel = types.StringValue("example")
	mcp.ServerURL = types.StringValue("https://example.com/mcp")

	var labelled diag.Diagnostics
	validateTools([]toolModel{toolOf(toolTypeWebSearch), mcp}, true, &labelled)
	if labelled.HasError() {
		t.Errorf("server_label should identify an mcp tool, got %q", errorSummaries(labelled))
	}
}

// TestValidateToolsPreviewGate ensures preview tool types are rejected unless
// the toolbox_tools preview feature is enabled.
func TestValidateToolsPreviewGate(t *testing.T) {
	t.Parallel()

	var gated diag.Diagnostics
	validateTools([]toolModel{toolOf(toolTypeReminderPreview)}, false, &gated)
	if !strings.Contains(errorSummaries(gated), "preview") {
		t.Errorf("expected a preview error, got %q", errorSummaries(gated))
	}

	var enabled diag.Diagnostics
	validateTools([]toolModel{toolOf(toolTypeReminderPreview)}, true, &enabled)
	if enabled.HasError() {
		t.Errorf("preview tool should be allowed once opted in, got %q", errorSummaries(enabled))
	}
}

// TestWarnPreviewToolsAlwaysWarns pins that preview tools warn on every plan,
// since the service is expected to rename them at general availability.
func TestWarnPreviewToolsAlwaysWarns(t *testing.T) {
	t.Parallel()

	var diagnostics diag.Diagnostics
	warnPreviewTools([]toolModel{toolOf(toolTypeWorkIQPreview), toolOf(toolTypeWebSearch)}, &diagnostics)

	if len(diagnostics.Warnings()) != 1 {
		t.Fatalf("expected exactly one warning, got %d", len(diagnostics.Warnings()))
	}
	if !strings.Contains(diagnostics.Warnings()[0].Detail(), toolTypeWorkIQPreview) {
		t.Errorf("warning should name the preview tool, got %q", diagnostics.Warnings()[0].Detail())
	}
}

// TestExpandToolsOmitsForeignFields checks that only the fields belonging to a
// tool's type reach the wire, so the service does not reject the payload.
func TestExpandToolsOmitsForeignFields(t *testing.T) {
	t.Parallel()

	mcp := toolOf(toolTypeMCP)
	mcp.ServerLabel = types.StringValue("example")
	mcp.ServerURL = types.StringValue("https://example.com/mcp")
	mcp.RequireApproval = types.StringValue("never")

	var diagnostics diag.Diagnostics
	requests := expandTools(context.Background(), []toolModel{mcp}, &diagnostics)
	if diagnostics.HasError() {
		t.Fatalf("expand: %q", errorSummaries(diagnostics))
	}

	encoded, err := json.Marshal(requests[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, key := range []string{"a2a_version", "base_url", "vector_store_ids", "azure_ai_search", "openapi"} {
		if _, present := decoded[key]; present {
			t.Errorf("mcp request should not carry %q", key)
		}
	}
	if decoded["server_label"] != "example" {
		t.Errorf("server_label = %v", decoded["server_label"])
	}
}

// TestExpandToolsBrowserAutomationNesting pins that the project connection is
// nested under browser_automation_preview.connection, which is the only tool
// that does not take project_connection_id at the top level.
func TestExpandToolsBrowserAutomationNesting(t *testing.T) {
	t.Parallel()

	tool := toolOf(toolTypeBrowserAutomation)
	tool.ProjectConnectionID = types.StringValue("playwright-conn")

	var diagnostics diag.Diagnostics
	requests := expandTools(context.Background(), []toolModel{tool}, &diagnostics)
	if diagnostics.HasError() {
		t.Fatalf("expand: %q", errorSummaries(diagnostics))
	}

	if requests[0].ProjectConnectionID != "" {
		t.Error("browser automation should not send a top-level project_connection_id")
	}
	if requests[0].BrowserAutomation == nil ||
		requests[0].BrowserAutomation.Connection.ProjectConnectionID != "playwright-conn" {
		t.Errorf("nested connection = %+v", requests[0].BrowserAutomation)
	}
}

// TestExpandOpenAPIAuth pins that each auth type carries its own security
// scheme, since the service rejects a scheme that does not match the type.
func TestExpandOpenAPIAuth(t *testing.T) {
	t.Parallel()

	newOpenAPITool := func(authType, connectionID, audience string) toolModel {
		tool := toolOf(toolTypeOpenAPI)
		tool.OpenAPI = types.ObjectValueMust(openAPIAttrTypes, map[string]attr.Value{
			"name":                  types.StringValue("api"),
			"description":           types.StringNull(),
			"spec":                  types.StringValue(`{"openapi":"3.0.0"}`),
			"auth_type":             types.StringValue(authType),
			"project_connection_id": optionalString(connectionID),
			"audience":              optionalString(audience),
		})
		return tool
	}

	var diagnostics diag.Diagnostics
	requests := expandTools(context.Background(), []toolModel{
		newOpenAPITool("anonymous", "", ""),
		newOpenAPITool("project_connection", "my-conn", ""),
		newOpenAPITool("managed_identity", "", "https://example.com"),
	}, &diagnostics)
	if diagnostics.HasError() {
		t.Fatalf("expand: %q", errorSummaries(diagnostics))
	}

	if requests[0].OpenAPI.Auth.SecurityScheme != nil {
		t.Error("anonymous auth should not carry a security scheme")
	}
	if requests[1].OpenAPI.Auth.SecurityScheme.ProjectConnectionID != "my-conn" {
		t.Errorf("project connection scheme = %+v", requests[1].OpenAPI.Auth.SecurityScheme)
	}
	if requests[2].OpenAPI.Auth.SecurityScheme.Audience != "https://example.com" {
		t.Errorf("managed identity scheme = %+v", requests[2].OpenAPI.Auth.SecurityScheme)
	}
}

// TestValidateOpenAPIIncompleteAuth ensures an auth type without its required
// companion attribute fails during plan.
func TestValidateOpenAPIIncompleteAuth(t *testing.T) {
	t.Parallel()

	tool := toolOf(toolTypeOpenAPI)
	tool.OpenAPI = types.ObjectValueMust(openAPIAttrTypes, map[string]attr.Value{
		"name":                  types.StringValue("api"),
		"description":           types.StringNull(),
		"spec":                  types.StringValue(`{"openapi":"3.0.0"}`),
		"auth_type":             types.StringValue("project_connection"),
		"project_connection_id": types.StringNull(),
		"audience":              types.StringNull(),
	})

	var diagnostics diag.Diagnostics
	validateTools([]toolModel{tool}, true, &diagnostics)
	if !strings.Contains(errorSummaries(diagnostics), "project_connection_id") {
		t.Errorf("expected a missing connection error, got %q", errorSummaries(diagnostics))
	}
}

// TestExpandSearchIndexWrapsSingleIndex pins that the index block is sent as a
// single-element indexes array, which is the shape the service requires.
func TestExpandSearchIndexWrapsSingleIndex(t *testing.T) {
	t.Parallel()

	tool := toolOf(toolTypeAzureAISearch)
	tool.Index = types.ObjectValueMust(indexAttrTypes, map[string]attr.Value{
		"project_connection_id": types.StringValue("search-conn"),
		"index_name":            types.StringValue("products"),
		"index_asset_id":        types.StringNull(),
		"query_type":            types.StringValue("semantic"),
		"top_k":                 types.Int64Value(5),
		"filter":                types.StringNull(),
	})

	var diagnostics diag.Diagnostics
	requests := expandTools(context.Background(), []toolModel{tool}, &diagnostics)
	if diagnostics.HasError() {
		t.Fatalf("expand: %q", errorSummaries(diagnostics))
	}

	indexes := requests[0].AzureAISearch.Indexes
	if len(indexes) != 1 || indexes[0].IndexName != "products" || *indexes[0].TopK != 5 {
		t.Fatalf("indexes = %+v", indexes)
	}
}

// TestFlattenToolsRoundTrip checks that a version response maps back onto the
// model without turning unset optional attributes into empty strings.
func TestFlattenToolsRoundTrip(t *testing.T) {
	t.Parallel()

	tools := flattenTools([]toolRequest{{
		Type:        toolTypeMCP,
		Name:        "example-mcp",
		ServerLabel: "example",
		ServerURL:   "https://example.com/mcp",
	}}, nil)

	if len(tools) != 1 {
		t.Fatalf("expected one tool, got %d", len(tools))
	}
	if tools[0].ServerLabel.ValueString() != "example" {
		t.Errorf("server_label = %q", tools[0].ServerLabel.ValueString())
	}
	if !tools[0].Description.IsNull() {
		t.Error("an omitted description should be null, not an empty string")
	}
	if !tools[0].Index.IsNull() || !tools[0].OpenAPI.IsNull() {
		t.Error("blocks belonging to other tool types should be null")
	}
}

// TestFlattenToolsDropsDerivedFabricIQServerLabel pins that the server_label
// the service derives from a fabric_iq_preview connection is discarded, since
// keeping it fails the apply-consistency check against a config that never set
// it.
func TestFlattenToolsDropsDerivedFabricIQServerLabel(t *testing.T) {
	t.Parallel()

	derived := flattenTools([]toolRequest{{
		Type:                toolTypeFabricIQPreview,
		Name:                "fabriciq",
		ProjectConnectionID: "my-connection",
		ServerLabel:         "my-connection",
	}}, nil)
	if !derived[0].ServerLabel.IsNull() {
		t.Errorf("derived server_label should be dropped, got %q", derived[0].ServerLabel.ValueString())
	}

	// A label the practitioner actually chose differs from the connection and
	// must survive.
	explicit := flattenTools([]toolRequest{{
		Type:                toolTypeFabricIQPreview,
		Name:                "fabriciq",
		ProjectConnectionID: "my-connection",
		ServerLabel:         "chosen-label",
	}}, nil)
	if explicit[0].ServerLabel.ValueString() != "chosen-label" {
		t.Errorf("explicit server_label should be kept, got %q", explicit[0].ServerLabel.ValueString())
	}
}
