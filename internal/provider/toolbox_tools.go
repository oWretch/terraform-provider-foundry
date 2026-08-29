package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Toolbox tool types, as defined by the service's ToolboxToolType enum. The
// preview entries carry a literal "_preview" suffix on the wire, and are NOT
// interchangeable with their unsuffixed counterparts: a2a requires
// a2a_version while a2a_preview rejects it, so the provider passes the
// configured type through verbatim rather than rewriting the suffix.
const (
	toolTypeCodeInterpreter   = "code_interpreter"
	toolTypeFileSearch        = "file_search"
	toolTypeWebSearch         = "web_search"
	toolTypeMCP               = "mcp"
	toolTypeAzureAISearch     = "azure_ai_search"
	toolTypeOpenAPI           = "openapi"
	toolTypeA2A               = "a2a"
	toolTypeToolboxSearch     = "toolbox_search"
	toolTypeA2APreview        = "a2a_preview"
	toolTypeBrowserAutomation = "browser_automation_preview"
	toolTypeReminderPreview   = "reminder_preview"
	toolTypeWorkIQPreview     = "work_iq_preview"
	toolTypeFabricIQPreview   = "fabric_iq_preview"
	toolTypeToolboxSearchPrev = "toolbox_search_preview"
)

// previewToolTypes are the tool types that are still in preview. They are
// gated behind the "toolbox_tools" enable_preview opt-in and warn on every
// plan, because the service may rename them (dropping the _preview suffix)
// or change their schema when they reach general availability.
// toolboxToolsPreviewFeatureName is the enable_preview value that opts in to
// preview tool types inside a toolbox. It is separate from "toolboxes", which
// gates the resource itself, so that a practitioner can manage toolboxes built
// from generally available tools without also accepting preview tool churn.
const toolboxToolsPreviewFeatureName = "toolbox_tools"

var previewToolTypes = map[string]struct{}{
	toolTypeA2APreview:        {},
	toolTypeBrowserAutomation: {},
	toolTypeReminderPreview:   {},
	toolTypeWorkIQPreview:     {},
	toolTypeFabricIQPreview:   {},
	toolTypeToolboxSearchPrev: {},
}

func toolTypeValues() []string {
	return []string{
		toolTypeCodeInterpreter, toolTypeFileSearch, toolTypeWebSearch,
		toolTypeMCP, toolTypeAzureAISearch, toolTypeOpenAPI, toolTypeA2A,
		toolTypeToolboxSearch, toolTypeA2APreview, toolTypeBrowserAutomation,
		toolTypeReminderPreview, toolTypeWorkIQPreview, toolTypeFabricIQPreview,
		toolTypeToolboxSearchPrev,
	}
}

// toolModel is the flattened Terraform representation of a toolbox tool. The
// service models each tool type as a distinct schema, but they share the
// type/name/description envelope, so this is expressed as one block type with
// per-type attributes validated in validateTool rather than as fourteen
// mutually exclusive blocks.
type toolModel struct {
	Type        types.String `tfsdk:"type"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`

	// mcp, fabric_iq_preview
	ServerLabel     types.String `tfsdk:"server_label"`
	ServerURL       types.String `tfsdk:"server_url"`
	ConnectorID     types.String `tfsdk:"connector_id"`
	RequireApproval types.String `tfsdk:"require_approval"`
	AllowedTools    types.Set    `tfsdk:"allowed_tools"`
	Headers         types.Map    `tfsdk:"headers"`

	// mcp, a2a, work_iq_preview, fabric_iq_preview, browser_automation_preview
	ProjectConnectionID types.String `tfsdk:"project_connection_id"`

	// a2a
	BaseURL       types.String `tfsdk:"base_url"`
	AgentCardPath types.String `tfsdk:"agent_card_path"`
	A2AVersion    types.String `tfsdk:"a2a_version"`

	// web_search
	SearchContextSize types.String `tfsdk:"search_context_size"`
	CustomSearch      types.Object `tfsdk:"custom_search_configuration"`

	// file_search
	VectorStoreIDs types.Set   `tfsdk:"vector_store_ids"`
	MaxNumResults  types.Int64 `tfsdk:"max_num_results"`

	// azure_ai_search
	Index types.Object `tfsdk:"index"`

	// openapi
	OpenAPI types.Object `tfsdk:"openapi"`
}

var (
	customSearchAttrTypes = map[string]attr.Type{
		"project_connection_id": types.StringType,
		"instance_name":         types.StringType,
	}
	indexAttrTypes = map[string]attr.Type{
		"project_connection_id": types.StringType,
		"index_name":            types.StringType,
		"index_asset_id":        types.StringType,
		"query_type":            types.StringType,
		"top_k":                 types.Int64Type,
		"filter":                types.StringType,
	}
	openAPIAttrTypes = map[string]attr.Type{
		"name":                  types.StringType,
		"description":           types.StringType,
		"spec":                  types.StringType,
		"auth_type":             types.StringType,
		"project_connection_id": types.StringType,
		"audience":              types.StringType,
	}
)

func toolsSchema() schema.ListNestedBlock {
	return schema.ListNestedBlock{
		MarkdownDescription: "Tools exposed by this toolbox version. At least one tool is required. " +
			"A toolbox allows only one tool of a given type without a `name`, so set a unique `name` on each instance when using the same type more than once.",
		NestedObject: schema.NestedBlockObject{
			Attributes: map[string]schema.Attribute{
				"type": schema.StringAttribute{
					Required: true,
					MarkdownDescription: "Tool type. One of `" + strings.Join(toolTypeValues(), "`, `") + "`. " +
						"Types ending in `_preview` are in preview and require `\"toolbox_tools\"` in the provider's `enable_preview` attribute.",
					Validators: []validator.String{stringvalidator.OneOf(toolTypeValues()...)},
				},
				"name": schema.StringAttribute{
					Optional:            true,
					MarkdownDescription: "Name for this tool instance, used to disambiguate multiple tools of the same type.",
				},
				"description": schema.StringAttribute{
					Optional:            true,
					MarkdownDescription: "Description of this tool instance, used by the model to select the right tool.",
				},
				"server_label": schema.StringAttribute{
					Optional:            true,
					MarkdownDescription: "Label identifying the MCP server. Required for `mcp`; optional for `fabric_iq_preview`.",
				},
				"server_url": schema.StringAttribute{
					Optional:            true,
					MarkdownDescription: "URL of the MCP server. For `mcp`, supply exactly one of `server_url`, `connector_id`, or `project_connection_id`.",
				},
				"connector_id": schema.StringAttribute{
					Optional: true,
					MarkdownDescription: "Identifier of a Microsoft first-party connector for an `mcp` tool, such as `connector_sharepoint` or `connector_microsoftteams`. " +
						"The service treats this as an open set, so values beyond the documented connectors are accepted.",
				},
				"require_approval": schema.StringAttribute{
					Optional:            true,
					MarkdownDescription: "Whether tool calls require approval, either `always` or `never`. Applies to `mcp` and `fabric_iq_preview`. Defaults to `always` at the service.",
					Validators:          []validator.String{stringvalidator.OneOf("always", "never")},
				},
				"allowed_tools": schema.SetAttribute{
					Optional:            true,
					ElementType:         types.StringType,
					MarkdownDescription: "Restricts an `mcp` tool to these tool names. Omit to allow every tool the server exposes.",
				},
				"headers": schema.MapAttribute{
					Optional:            true,
					ElementType:         types.StringType,
					MarkdownDescription: "Additional HTTP headers sent to an `mcp` server. Avoid placing secrets here, since values are stored in state.",
				},
				"project_connection_id": schema.StringAttribute{
					Optional: true,
					MarkdownDescription: "Project connection supplying credentials for this tool. Required for `work_iq_preview`, `fabric_iq_preview`, and `browser_automation_preview`; " +
						"one option among several for `mcp` and `a2a`. Connections are created outside this provider, in Azure Resource Manager.",
				},
				"base_url": schema.StringAttribute{
					Optional:            true,
					MarkdownDescription: "Base URL of the remote agent, for `a2a` and `a2a_preview`. Supply at least one of `base_url` or `project_connection_id`.",
				},
				"agent_card_path": schema.StringAttribute{
					Optional:            true,
					MarkdownDescription: "Path to the remote agent's agent card, such as `/.well-known/agent-card.json`.",
				},
				"a2a_version": schema.StringAttribute{
					Optional:            true,
					MarkdownDescription: "A2A protocol version. Required for `a2a`, which currently supports only `1.0`. Not accepted by `a2a_preview`.",
				},
				"search_context_size": schema.StringAttribute{
					Optional:            true,
					MarkdownDescription: "Amount of search context retrieved for a `web_search` tool: `low`, `medium`, or `high`.",
					Validators:          []validator.String{stringvalidator.OneOf("low", "medium", "high")},
				},
				"vector_store_ids": schema.SetAttribute{
					Optional:            true,
					ElementType:         types.StringType,
					MarkdownDescription: "Vector stores searched by a `file_search` tool.",
				},
				"max_num_results": schema.Int64Attribute{
					Optional:            true,
					MarkdownDescription: "Maximum number of results returned by a `file_search` tool.",
				},
			},
			Blocks: map[string]schema.Block{
				"custom_search_configuration": schema.SingleNestedBlock{
					MarkdownDescription: "Grounds a `web_search` tool in a Bing Custom Search instance covering selected domains.",
					Attributes: map[string]schema.Attribute{
						"project_connection_id": schema.StringAttribute{
							Optional:            true,
							MarkdownDescription: "Project connection for the Bing Custom Search resource.",
						},
						"instance_name": schema.StringAttribute{
							Optional:            true,
							MarkdownDescription: "Custom Search instance name.",
						},
					},
				},
				"index": schema.SingleNestedBlock{
					MarkdownDescription: "Index searched by an `azure_ai_search` tool. Supply either `project_connection_id` with `index_name`, or `index_asset_id`. The service accepts a single index per tool.",
					Attributes: map[string]schema.Attribute{
						"project_connection_id": schema.StringAttribute{
							Optional:            true,
							MarkdownDescription: "Project connection for the Azure AI Search service.",
						},
						"index_name": schema.StringAttribute{
							Optional:            true,
							MarkdownDescription: "Name of the search index.",
						},
						"index_asset_id": schema.StringAttribute{
							Optional:            true,
							MarkdownDescription: "Identifier of an index asset, used instead of a connection and index name.",
						},
						"query_type": schema.StringAttribute{
							Optional:            true,
							MarkdownDescription: "Query strategy: `simple`, `semantic`, `vector`, `vector_simple_hybrid`, or `vector_semantic_hybrid`.",
						},
						"top_k": schema.Int64Attribute{
							Optional:            true,
							MarkdownDescription: "Number of results retrieved from the index.",
						},
						"filter": schema.StringAttribute{
							Optional:            true,
							MarkdownDescription: "OData filter applied to the index query.",
						},
					},
				},
				"openapi": schema.SingleNestedBlock{
					MarkdownDescription: "OpenAPI specification exposed by an `openapi` tool.",
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Optional:            true,
							MarkdownDescription: "Name of the OpenAPI function.",
						},
						"description": schema.StringAttribute{
							Optional:            true,
							MarkdownDescription: "Description of the OpenAPI function.",
						},
						"spec": schema.StringAttribute{
							Optional:            true,
							MarkdownDescription: "JSON-encoded OpenAPI specification, for example `file(\"api.json\")` or `jsonencode({ ... })`.",
						},
						"auth_type": schema.StringAttribute{
							Optional:            true,
							MarkdownDescription: "Authentication for the API: `anonymous`, `project_connection`, or `managed_identity`.",
							Validators:          []validator.String{stringvalidator.OneOf("anonymous", "project_connection", "managed_identity")},
						},
						"project_connection_id": schema.StringAttribute{
							Optional:            true,
							MarkdownDescription: "Project connection supplying API credentials. Required when `auth_type` is `project_connection`.",
						},
						"audience": schema.StringAttribute{
							Optional:            true,
							MarkdownDescription: "Token audience for managed identity authentication. Required when `auth_type` is `managed_identity`.",
						},
					},
				},
			},
		},
	}
}

// toolRequest is the wire form of a toolbox tool. Every field is omitempty so
// that a single struct can serialize all fourteen tool types without emitting
// keys the service rejects for a given type.
type toolRequest struct {
	Type        string `json:"type"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`

	ServerLabel     string            `json:"server_label,omitempty"`
	ServerURL       string            `json:"server_url,omitempty"`
	ConnectorID     string            `json:"connector_id,omitempty"`
	RequireApproval string            `json:"require_approval,omitempty"`
	AllowedTools    []string          `json:"allowed_tools,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`

	ProjectConnectionID string `json:"project_connection_id,omitempty"`

	BaseURL       string `json:"base_url,omitempty"`
	AgentCardPath string `json:"agent_card_path,omitempty"`
	A2AVersion    string `json:"a2a_version,omitempty"`

	SearchContextSize string                   `json:"search_context_size,omitempty"`
	CustomSearch      *webSearchConfiguration  `json:"custom_search_configuration,omitempty"`
	VectorStoreIDs    []string                 `json:"vector_store_ids,omitempty"`
	MaxNumResults     *int64                   `json:"max_num_results,omitempty"`
	AzureAISearch     *azureAISearchResource   `json:"azure_ai_search,omitempty"`
	OpenAPI           *openAPIFunction         `json:"openapi,omitempty"`
	BrowserAutomation *browserAutomationParams `json:"browser_automation_preview,omitempty"`
}

type webSearchConfiguration struct {
	ProjectConnectionID string `json:"project_connection_id"`
	InstanceName        string `json:"instance_name"`
}

type azureAISearchResource struct {
	Indexes []azureAISearchIndex `json:"indexes"`
}

type azureAISearchIndex struct {
	ProjectConnectionID string `json:"project_connection_id,omitempty"`
	IndexName           string `json:"index_name,omitempty"`
	IndexAssetID        string `json:"index_asset_id,omitempty"`
	QueryType           string `json:"query_type,omitempty"`
	TopK                *int64 `json:"top_k,omitempty"`
	Filter              string `json:"filter,omitempty"`
}

type openAPIFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Spec        json.RawMessage `json:"spec"`
	Auth        openAPIAuth     `json:"auth"`
}

type openAPIAuth struct {
	Type           string                 `json:"type"`
	SecurityScheme *openAPISecurityScheme `json:"security_scheme,omitempty"`
}

type openAPISecurityScheme struct {
	ProjectConnectionID string `json:"project_connection_id,omitempty"`
	Audience            string `json:"audience,omitempty"`
}

type browserAutomationParams struct {
	Connection browserAutomationConnection `json:"connection"`
}

type browserAutomationConnection struct {
	ProjectConnectionID string `json:"project_connection_id"`
}

// requiredToolFields maps a tool type to the attributes the service requires
// for it. Enforcing this in the provider turns an opaque apply-time 400 into a
// plan-time error naming the missing attribute.
var requiredToolFields = map[string][]string{
	toolTypeMCP:               {"server_label"},
	toolTypeAzureAISearch:     {"index"},
	toolTypeOpenAPI:           {"openapi"},
	toolTypeA2A:               {"a2a_version"},
	toolTypeWorkIQPreview:     {"project_connection_id"},
	toolTypeFabricIQPreview:   {"project_connection_id"},
	toolTypeBrowserAutomation: {"project_connection_id"},
}

// allowedToolFields maps a tool type to every attribute it accepts, so that
// attributes belonging to a different tool type are rejected at plan time
// rather than being silently dropped from the request.
var allowedToolFields = map[string]map[string]bool{
	toolTypeCodeInterpreter:   set(),
	toolTypeReminderPreview:   set(),
	toolTypeToolboxSearch:     set(),
	toolTypeToolboxSearchPrev: set(),
	toolTypeFileSearch:        set("vector_store_ids", "max_num_results"),
	toolTypeWebSearch:         set("search_context_size", "custom_search_configuration"),
	toolTypeAzureAISearch:     set("index"),
	toolTypeOpenAPI:           set("openapi"),
	toolTypeMCP: set("server_label", "server_url", "connector_id", "require_approval",
		"allowed_tools", "headers", "project_connection_id"),
	toolTypeA2A:               set("base_url", "agent_card_path", "project_connection_id", "a2a_version"),
	toolTypeA2APreview:        set("base_url", "agent_card_path", "project_connection_id"),
	toolTypeWorkIQPreview:     set("project_connection_id"),
	toolTypeFabricIQPreview:   set("project_connection_id", "server_label", "server_url", "require_approval"),
	toolTypeBrowserAutomation: set("project_connection_id"),
}

func set(values ...string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}

// setToolFields reports which per-type attributes the practitioner configured.
func (t toolModel) setToolFields() map[string]bool {
	configured := map[string]bool{
		"server_label":                notEmpty(t.ServerLabel),
		"server_url":                  notEmpty(t.ServerURL),
		"connector_id":                notEmpty(t.ConnectorID),
		"require_approval":            notEmpty(t.RequireApproval),
		"allowed_tools":               !t.AllowedTools.IsNull() && !t.AllowedTools.IsUnknown(),
		"headers":                     !t.Headers.IsNull() && !t.Headers.IsUnknown(),
		"project_connection_id":       notEmpty(t.ProjectConnectionID),
		"base_url":                    notEmpty(t.BaseURL),
		"agent_card_path":             notEmpty(t.AgentCardPath),
		"a2a_version":                 notEmpty(t.A2AVersion),
		"search_context_size":         notEmpty(t.SearchContextSize),
		"custom_search_configuration": !t.CustomSearch.IsNull() && !t.CustomSearch.IsUnknown(),
		"vector_store_ids":            !t.VectorStoreIDs.IsNull() && !t.VectorStoreIDs.IsUnknown(),
		"max_num_results":             !t.MaxNumResults.IsNull() && !t.MaxNumResults.IsUnknown(),
		"index":                       !t.Index.IsNull() && !t.Index.IsUnknown(),
		"openapi":                     !t.OpenAPI.IsNull() && !t.OpenAPI.IsUnknown(),
	}
	for key, value := range configured {
		if !value {
			delete(configured, key)
		}
	}
	return configured
}

func notEmpty(value types.String) bool {
	return !value.IsNull() && !value.IsUnknown() && value.ValueString() != ""
}

// warnPreviewTools emits a warning for every preview tool type in use. The
// warning is unconditional rather than tied to a deprecation, because these
// types carry a literal "_preview" suffix that the service is expected to drop
// at general availability: the replacement type is a different enum member and
// may take different fields, so configurations will need editing by hand.
func warnPreviewTools(tools []toolModel, diagnostics *diag.Diagnostics) {
	warned := map[string]bool{}
	for _, tool := range tools {
		toolType := tool.Type.ValueString()
		if _, preview := previewToolTypes[toolType]; !preview || warned[toolType] {
			continue
		}
		warned[toolType] = true
		diagnostics.AddWarning(
			"Preview tool type in use",
			fmt.Sprintf("The %q tool type is in preview. Its configuration and behavior may change, and reaching general availability is expected to rename it, "+
				"which will require updating this configuration by hand.", toolType),
		)
	}
}

// validateTools checks tool types, per-type required and allowed attributes,
// and the service's rule that at most one tool of a type may omit a name.
func validateTools(tools []toolModel, previewEnabled bool, diagnostics *diag.Diagnostics) {
	if len(tools) == 0 {
		diagnostics.AddError("No tools configured", "A toolbox version must contain at least one tool.")
		return
	}

	// The service allows a single tool without an identifier across the whole
	// toolbox, not one per type: a name (or server_label, for mcp) is what
	// disambiguates tools at call time.
	var unnamed []string
	for _, tool := range tools {
		toolType := tool.Type.ValueString()
		if tool.Type.IsUnknown() || toolType == "" {
			continue
		}
		allowed, known := allowedToolFields[toolType]
		if !known {
			continue // the type validator already reported this
		}

		if _, preview := previewToolTypes[toolType]; preview && !previewEnabled {
			diagnostics.AddError(
				"Preview feature not enabled",
				fmt.Sprintf("The %q tool type is in preview and requires %q in the provider's enable_preview attribute.", toolType, toolboxToolsPreviewFeatureName),
			)
		}

		for _, required := range requiredToolFields[toolType] {
			if !tool.setToolFields()[required] {
				diagnostics.AddError(
					"Missing required tool attribute",
					fmt.Sprintf("The %q tool requires %q to be set.", toolType, required),
				)
			}
		}

		var unexpected []string
		for field := range tool.setToolFields() {
			if !allowed[field] {
				unexpected = append(unexpected, field)
			}
		}
		if len(unexpected) > 0 {
			sort.Strings(unexpected)
			diagnostics.AddError(
				"Unsupported tool attribute",
				fmt.Sprintf("The %q tool does not accept %s.", toolType, strings.Join(quoteAll(unexpected), ", ")),
			)
		}

		if !notEmpty(tool.Name) && (toolType != toolTypeMCP || !notEmpty(tool.ServerLabel)) {
			unnamed = append(unnamed, toolType)
		}
	}

	if len(unnamed) > 1 {
		sort.Strings(unnamed)
		diagnostics.AddError(
			"Multiple tools without a name",
			fmt.Sprintf("A toolbox allows only one tool without a name, but %s are all unnamed. Set a unique name on all but one of them.",
				strings.Join(quoteAll(unnamed), ", ")),
		)
	}

	validateMCPTarget(tools, diagnostics)
	validateA2ATarget(tools, diagnostics)
	validateOpenAPIAuth(tools, diagnostics)
	validateSearchIndex(tools, diagnostics)
}

func validateMCPTarget(tools []toolModel, diagnostics *diag.Diagnostics) {
	for _, tool := range tools {
		if tool.Type.ValueString() != toolTypeMCP {
			continue
		}
		fields := tool.setToolFields()
		if !fields["server_url"] && !fields["connector_id"] && !fields["project_connection_id"] {
			diagnostics.AddError(
				"Incomplete mcp tool",
				`An "mcp" tool requires one of "server_url", "connector_id", or "project_connection_id".`,
			)
		}
	}
}

func validateA2ATarget(tools []toolModel, diagnostics *diag.Diagnostics) {
	for _, tool := range tools {
		toolType := tool.Type.ValueString()
		if toolType != toolTypeA2A && toolType != toolTypeA2APreview {
			continue
		}
		fields := tool.setToolFields()
		if !fields["base_url"] && !fields["project_connection_id"] {
			diagnostics.AddError(
				"Incomplete a2a tool",
				fmt.Sprintf("An %q tool requires at least one of \"base_url\" or \"project_connection_id\".", toolType),
			)
		}
	}
}

func validateOpenAPIAuth(tools []toolModel, diagnostics *diag.Diagnostics) {
	for _, tool := range tools {
		if tool.Type.ValueString() != toolTypeOpenAPI || tool.OpenAPI.IsNull() || tool.OpenAPI.IsUnknown() {
			continue
		}
		attributes := tool.OpenAPI.Attributes()
		authType := stringAttr(attributes, "auth_type")
		switch authType {
		case "project_connection":
			if stringAttr(attributes, "project_connection_id") == "" {
				diagnostics.AddError(
					"Incomplete openapi tool",
					`An "openapi" tool with auth_type "project_connection" requires "project_connection_id".`,
				)
			}
		case "managed_identity":
			if stringAttr(attributes, "audience") == "" {
				diagnostics.AddError(
					"Incomplete openapi tool",
					`An "openapi" tool with auth_type "managed_identity" requires "audience".`,
				)
			}
		}
		if spec := stringAttr(attributes, "spec"); spec != "" && !json.Valid([]byte(spec)) {
			diagnostics.AddError("Invalid openapi spec", `The "spec" attribute must contain valid JSON.`)
		}
	}
}

func validateSearchIndex(tools []toolModel, diagnostics *diag.Diagnostics) {
	for _, tool := range tools {
		if tool.Type.ValueString() != toolTypeAzureAISearch || tool.Index.IsNull() || tool.Index.IsUnknown() {
			continue
		}
		attributes := tool.Index.Attributes()
		hasPair := stringAttr(attributes, "project_connection_id") != "" && stringAttr(attributes, "index_name") != ""
		if !hasPair && stringAttr(attributes, "index_asset_id") == "" {
			diagnostics.AddError(
				"Incomplete azure_ai_search tool",
				`An "azure_ai_search" index requires either "project_connection_id" with "index_name", or "index_asset_id".`,
			)
		}
	}
}

func stringAttr(attributes map[string]attr.Value, key string) string {
	value, ok := attributes[key].(types.String)
	if !ok || value.IsNull() || value.IsUnknown() {
		return ""
	}
	return value.ValueString()
}

func int64Attr(attributes map[string]attr.Value, key string) *int64 {
	value, ok := attributes[key].(types.Int64)
	if !ok || value.IsNull() || value.IsUnknown() {
		return nil
	}
	result := value.ValueInt64()
	return &result
}

func quoteAll(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, `"`+value+`"`)
	}
	return out
}

// expandTools converts the configured tools into their wire form, emitting
// only the fields that belong to each tool's type.
func expandTools(ctx context.Context, tools []toolModel, diagnostics *diag.Diagnostics) []toolRequest {
	requests := make([]toolRequest, 0, len(tools))
	for _, tool := range tools {
		toolType := tool.Type.ValueString()
		request := toolRequest{
			Type:        toolType,
			Name:        tool.Name.ValueString(),
			Description: tool.Description.ValueString(),
		}
		allowed := allowedToolFields[toolType]

		if allowed["server_label"] {
			request.ServerLabel = tool.ServerLabel.ValueString()
		}
		if allowed["server_url"] {
			request.ServerURL = tool.ServerURL.ValueString()
		}
		if allowed["connector_id"] {
			request.ConnectorID = tool.ConnectorID.ValueString()
		}
		if allowed["require_approval"] {
			request.RequireApproval = tool.RequireApproval.ValueString()
		}
		if allowed["project_connection_id"] && toolType != toolTypeBrowserAutomation {
			request.ProjectConnectionID = tool.ProjectConnectionID.ValueString()
		}
		if allowed["base_url"] {
			request.BaseURL = tool.BaseURL.ValueString()
		}
		if allowed["agent_card_path"] {
			request.AgentCardPath = tool.AgentCardPath.ValueString()
		}
		if allowed["a2a_version"] {
			request.A2AVersion = tool.A2AVersion.ValueString()
		}
		if allowed["search_context_size"] {
			request.SearchContextSize = tool.SearchContextSize.ValueString()
		}
		if allowed["allowed_tools"] && !tool.AllowedTools.IsNull() {
			diagnostics.Append(tool.AllowedTools.ElementsAs(ctx, &request.AllowedTools, false)...)
		}
		if allowed["headers"] && !tool.Headers.IsNull() {
			diagnostics.Append(tool.Headers.ElementsAs(ctx, &request.Headers, false)...)
		}
		if allowed["vector_store_ids"] && !tool.VectorStoreIDs.IsNull() {
			diagnostics.Append(tool.VectorStoreIDs.ElementsAs(ctx, &request.VectorStoreIDs, false)...)
		}
		if allowed["max_num_results"] && !tool.MaxNumResults.IsNull() && !tool.MaxNumResults.IsUnknown() {
			value := tool.MaxNumResults.ValueInt64()
			request.MaxNumResults = &value
		}
		if allowed["custom_search_configuration"] && !tool.CustomSearch.IsNull() && !tool.CustomSearch.IsUnknown() {
			attributes := tool.CustomSearch.Attributes()
			request.CustomSearch = &webSearchConfiguration{
				ProjectConnectionID: stringAttr(attributes, "project_connection_id"),
				InstanceName:        stringAttr(attributes, "instance_name"),
			}
		}
		if allowed["index"] && !tool.Index.IsNull() && !tool.Index.IsUnknown() {
			attributes := tool.Index.Attributes()
			request.AzureAISearch = &azureAISearchResource{Indexes: []azureAISearchIndex{{
				ProjectConnectionID: stringAttr(attributes, "project_connection_id"),
				IndexName:           stringAttr(attributes, "index_name"),
				IndexAssetID:        stringAttr(attributes, "index_asset_id"),
				QueryType:           stringAttr(attributes, "query_type"),
				TopK:                int64Attr(attributes, "top_k"),
				Filter:              stringAttr(attributes, "filter"),
			}}}
		}
		if allowed["openapi"] && !tool.OpenAPI.IsNull() && !tool.OpenAPI.IsUnknown() {
			attributes := tool.OpenAPI.Attributes()
			auth := openAPIAuth{Type: stringAttr(attributes, "auth_type")}
			switch auth.Type {
			case "project_connection":
				auth.SecurityScheme = &openAPISecurityScheme{ProjectConnectionID: stringAttr(attributes, "project_connection_id")}
			case "managed_identity":
				auth.SecurityScheme = &openAPISecurityScheme{Audience: stringAttr(attributes, "audience")}
			}
			request.OpenAPI = &openAPIFunction{
				Name:        stringAttr(attributes, "name"),
				Description: stringAttr(attributes, "description"),
				Spec:        json.RawMessage(stringAttr(attributes, "spec")),
				Auth:        auth,
			}
		}
		if toolType == toolTypeBrowserAutomation {
			request.BrowserAutomation = &browserAutomationParams{
				Connection: browserAutomationConnection{ProjectConnectionID: tool.ProjectConnectionID.ValueString()},
			}
		}
		requests = append(requests, request)
	}
	return requests
}

// flattenTools converts a version response back into model form. Values the
// practitioner did not configure are preserved as null rather than adopting
// service defaults, so that an unset optional attribute does not show a diff.
func flattenTools(responses []toolRequest, prior []toolModel) []toolModel {
	tools := make([]toolModel, 0, len(responses))
	for index, response := range responses {
		var previous *toolModel
		if index < len(prior) {
			previous = &prior[index]
		}
		tool := toolModel{
			Type:        types.StringValue(response.Type),
			Name:        optionalString(response.Name),
			Description: optionalString(response.Description),

			ServerLabel:         optionalString(response.ServerLabel),
			ServerURL:           optionalString(response.ServerURL),
			ConnectorID:         optionalString(response.ConnectorID),
			RequireApproval:     optionalString(response.RequireApproval),
			ProjectConnectionID: optionalString(response.ProjectConnectionID),
			BaseURL:             optionalString(response.BaseURL),
			AgentCardPath:       optionalString(response.AgentCardPath),
			A2AVersion:          optionalString(response.A2AVersion),
			SearchContextSize:   optionalString(response.SearchContextSize),

			AllowedTools:   stringSetOrNull(response.AllowedTools),
			VectorStoreIDs: stringSetOrNull(response.VectorStoreIDs),
			Headers:        stringMapOrNull(response.Headers),
			MaxNumResults:  int64OrNull(response.MaxNumResults),

			CustomSearch: flattenCustomSearch(response.CustomSearch),
			Index:        flattenIndex(response.AzureAISearch),
			OpenAPI:      flattenOpenAPI(response.OpenAPI, previous),
		}
		if response.BrowserAutomation != nil {
			tool.ProjectConnectionID = optionalString(response.BrowserAutomation.Connection.ProjectConnectionID)
		}
		tools = append(tools, tool)
	}
	return tools
}

func flattenCustomSearch(configuration *webSearchConfiguration) types.Object {
	if configuration == nil {
		return types.ObjectNull(customSearchAttrTypes)
	}
	return types.ObjectValueMust(customSearchAttrTypes, map[string]attr.Value{
		"project_connection_id": optionalString(configuration.ProjectConnectionID),
		"instance_name":         optionalString(configuration.InstanceName),
	})
}

func flattenIndex(resource *azureAISearchResource) types.Object {
	if resource == nil || len(resource.Indexes) == 0 {
		return types.ObjectNull(indexAttrTypes)
	}
	index := resource.Indexes[0]
	return types.ObjectValueMust(indexAttrTypes, map[string]attr.Value{
		"project_connection_id": optionalString(index.ProjectConnectionID),
		"index_name":            optionalString(index.IndexName),
		"index_asset_id":        optionalString(index.IndexAssetID),
		"query_type":            optionalString(index.QueryType),
		"top_k":                 int64OrNull(index.TopK),
		"filter":                optionalString(index.Filter),
	})
}

// flattenOpenAPI rebuilds the openapi block. The spec is taken from prior
// state when the two are semantically equal, because the service reformats
// the JSON it echoes back.
func flattenOpenAPI(function *openAPIFunction, previous *toolModel) types.Object {
	if function == nil {
		return types.ObjectNull(openAPIAttrTypes)
	}
	spec := optionalString(string(function.Spec))
	connectionID, audience := types.StringNull(), types.StringNull()
	if function.Auth.SecurityScheme != nil {
		connectionID = optionalString(function.Auth.SecurityScheme.ProjectConnectionID)
		audience = optionalString(function.Auth.SecurityScheme.Audience)
	}
	if previous != nil && !previous.OpenAPI.IsNull() && !previous.OpenAPI.IsUnknown() {
		priorSpec := stringAttr(previous.OpenAPI.Attributes(), "spec")
		if priorSpec != "" && jsonSemanticallyEqual([]byte(priorSpec), function.Spec) {
			spec = types.StringValue(priorSpec)
		}
	}
	return types.ObjectValueMust(openAPIAttrTypes, map[string]attr.Value{
		"name":                  optionalString(function.Name),
		"description":           optionalString(function.Description),
		"spec":                  spec,
		"auth_type":             optionalString(function.Auth.Type),
		"project_connection_id": connectionID,
		"audience":              audience,
	})
}

func stringSetOrNull(values []string) types.Set {
	if len(values) == 0 {
		return types.SetNull(types.StringType)
	}
	elements := make([]attr.Value, 0, len(values))
	for _, value := range values {
		elements = append(elements, types.StringValue(value))
	}
	return types.SetValueMust(types.StringType, elements)
}

func stringMapOrNull(values map[string]string) types.Map {
	if len(values) == 0 {
		return types.MapNull(types.StringType)
	}
	elements := make(map[string]attr.Value, len(values))
	for key, value := range values {
		elements[key] = types.StringValue(value)
	}
	return types.MapValueMust(types.StringType, elements)
}

func int64OrNull(value *int64) types.Int64 {
	if value == nil {
		return types.Int64Null()
	}
	return types.Int64Value(*value)
}
