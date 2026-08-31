package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

// agentCommon holds the attributes shared by every agent kind. Each kind embeds
// this and contributes only its own definition fields.
type agentCommon struct {
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	ID          types.String `tfsdk:"id"`
	Version     types.String `tfsdk:"version"`
	AgentGUID   types.String `tfsdk:"agent_guid"`
	PrincipalID types.String `tfsdk:"principal_id"`
	ClientID    types.String `tfsdk:"client_id"`
	// Draft is a preview-gated ("draft_agents") field. When true, publishing a new
	// definition creates a mutable draft version (id like "name:draft-<timestamp>")
	// instead of an immutable numbered version. Verified live: the service accepts
	// "draft": true without any Foundry-Features header, but this is documented as
	// preview, so the provider still requires enable_preview to include
	// "draft_agents" before setting it.
	Draft types.Bool `tfsdk:"draft"`
	// AgentEndpoint is a preview-gated ("agent_endpoints") computed-only field that
	// surfaces the agent's built-in endpoint, verified live to be present on every
	// agent regardless of preview opt-in. This provider exposes it read-only:
	// version_selector traffic rules and the protocol list. Deeper sub-fields
	// (protocol_configuration, authorization_schemes) use discriminated unions this
	// provider version does not model, and are left out of scope.
	AgentEndpoint types.Object `tfsdk:"agent_endpoint"`
}

var agentEndpointRuleAttrTypes = map[string]attr.Type{
	"type":               types.StringType,
	"agent_version":      types.StringType,
	"traffic_percentage": types.Int64Type,
}

var agentEndpointAttrTypes = map[string]attr.Type{
	"protocols":               types.ListType{ElemType: types.StringType},
	"version_selection_rules": types.ListType{ElemType: types.ObjectType{AttrTypes: agentEndpointRuleAttrTypes}},
}

var raiConfigAttrTypes = map[string]attr.Type{
	"rai_policy_name": types.StringType,
}

var agentNameValidators = []validator.String{
	stringvalidator.LengthAtMost(63),
	stringvalidator.RegexMatches(
		regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?$`),
		"must start and end with an alphanumeric character and contain only alphanumeric characters and middle hyphens",
	),
}

// agentCommonSchema returns the attributes every agent resource exposes.
func agentCommonSchema() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"name": schema.StringAttribute{
			Required:            true,
			MarkdownDescription: "Agent name. Changing this forces a new agent to be created.",
			PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			Validators:          agentNameValidators,
		},
		"description": schema.StringAttribute{
			Optional:            true,
			MarkdownDescription: "Description of the agent version.",
		},
		"id": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Identifier of the current agent version, in `name:version` form.",
		},
		"version": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Current agent version number.",
		},
		"agent_guid": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Service-assigned unique identifier for the agent.",
		},
		"principal_id": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Principal ID of the agent instance identity.",
		},
		"client_id": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Client ID of the agent instance identity.",
		},
		"draft": schema.BoolAttribute{
			Optional: true,
			Computed: true,
			MarkdownDescription: "~> **Preview:** requires `enable_preview` to include `\"draft_agents\"`. Preview features may change in any provider release without following semantic versioning.\n\n" +
				"When `true`, publishing a new definition creates a mutable draft version instead of an immutable numbered version. Defaults to `false`.",
		},
		"agent_endpoint": schema.SingleNestedAttribute{
			Computed: true,
			MarkdownDescription: "~> **Preview:** requires `enable_preview` to include `\"agent_endpoints\"`. Preview features may change in any provider release without following semantic versioning.\n\n" +
				"The agent's built-in endpoint, including how traffic is routed across published versions. Read-only; the service manages the remaining endpoint sub-fields (protocol configuration, authorization schemes) outside this provider.",
			Attributes: map[string]schema.Attribute{
				"protocols": schema.ListAttribute{
					Computed:            true,
					ElementType:         types.StringType,
					MarkdownDescription: "Protocols the endpoint serves, such as `responses`.",
				},
				"version_selection_rules": schema.ListNestedAttribute{
					Computed:            true,
					MarkdownDescription: "Traffic routing rules applied to agent versions.",
					NestedObject: schema.NestedAttributeObject{
						Attributes: map[string]schema.Attribute{
							"type": schema.StringAttribute{
								Computed:            true,
								MarkdownDescription: "Routing rule type, such as `FixedRatio`.",
							},
							"agent_version": schema.StringAttribute{
								Computed:            true,
								MarkdownDescription: "Agent version the rule applies to, or `@latest`.",
							},
							"traffic_percentage": schema.Int64Attribute{
								Computed:            true,
								MarkdownDescription: "Percentage of traffic routed to this version.",
							},
						},
					},
				},
			},
		},
	}
}

type agentIdentity struct {
	PrincipalID string `json:"principal_id"`
	ClientID    string `json:"client_id"`
}

type raiConfig struct {
	PolicyName string `json:"rai_policy_name"`
}

func raiConfigDefinition(value types.Object) *raiConfig {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	policyName, ok := value.Attributes()["rai_policy_name"].(types.String)
	if !ok || policyName.IsNull() || policyName.IsUnknown() {
		return nil
	}
	return &raiConfig{PolicyName: policyName.ValueString()}
}

func raiConfigValue(value *raiConfig, diagnostics *diag.Diagnostics) types.Object {
	if value == nil {
		return types.ObjectNull(raiConfigAttrTypes)
	}
	result, diags := types.ObjectValue(raiConfigAttrTypes, map[string]attr.Value{
		"rai_policy_name": types.StringValue(value.PolicyName),
	})
	diagnostics.Append(diags...)
	return result
}

type agentEndpointRule struct {
	Type              string `json:"type"`
	AgentVersion      string `json:"agent_version"`
	TrafficPercentage int64  `json:"traffic_percentage"`
}

type agentEndpoint struct {
	Protocols       []string `json:"protocols"`
	VersionSelector struct {
		VersionSelectionRules []agentEndpointRule `json:"version_selection_rules"`
	} `json:"version_selector"`
}

// agentVersion is the service representation of a single immutable agent version.
// Definition stays raw so each kind can decode only the fields it models.
type agentVersion struct {
	ID                 string            `json:"id"`
	Name               string            `json:"name"`
	Version            string            `json:"version"`
	Description        string            `json:"description"`
	Metadata           map[string]string `json:"metadata"`
	CreatedAt          int64             `json:"created_at"`
	Status             string            `json:"status"`
	Definition         json.RawMessage   `json:"definition"`
	Blueprint          json.RawMessage   `json:"blueprint"`
	BlueprintReference json.RawMessage   `json:"blueprint_reference"`
	AgentGUID          string            `json:"agent_guid"`
	Identity           *agentIdentity    `json:"instance_identity"`
	Draft              bool              `json:"draft"`
}

type agentResponse struct {
	Name               string          `json:"name"`
	Identity           *agentIdentity  `json:"instance_identity"`
	Blueprint          json.RawMessage `json:"blueprint"`
	BlueprintReference json.RawMessage `json:"blueprint_reference"`
	Versions           struct {
		Latest agentVersion `json:"latest"`
	} `json:"versions"`
	AgentEndpoint *agentEndpoint `json:"agent_endpoint"`
}

func (r agentResponse) latestVersion() agentVersion {
	version := r.Versions.Latest
	if version.Name == "" {
		version.Name = r.Name
	}
	if version.Identity == nil {
		version.Identity = r.Identity
	}
	if !hasJSONValue(version.Blueprint) {
		version.Blueprint = r.Blueprint
	}
	if !hasJSONValue(version.BlueprintReference) {
		version.BlueprintReference = r.BlueprintReference
	}
	return version
}

type agentRequest struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Definition  any    `json:"definition"`
	Draft       bool   `json:"draft,omitempty"`
}

// applyVersion copies the service-assigned fields of version into the common attributes.
func (c *agentCommon) applyVersion(version agentVersion) {
	c.ID = types.StringValue(version.ID)
	c.Version = types.StringValue(version.Version)
	c.AgentGUID = types.StringValue(version.AgentGUID)
	c.Description = optionalString(version.Description)
	c.Draft = types.BoolValue(version.Draft)
	if version.Identity != nil {
		c.PrincipalID = types.StringValue(version.Identity.PrincipalID)
		c.ClientID = types.StringValue(version.Identity.ClientID)
	} else {
		c.PrincipalID = types.StringNull()
		c.ClientID = types.StringNull()
	}
}

// applyEndpoint copies the service-reported agent_endpoint into the computed
// agent_endpoint attribute. A nil endpoint (e.g. an offline unit test fixture)
// results in a null object rather than an error.
func (c *agentCommon) applyEndpoint(ctx context.Context, endpoint *agentEndpoint, diagnostics *diag.Diagnostics) {
	if endpoint == nil {
		c.AgentEndpoint = types.ObjectNull(agentEndpointAttrTypes)
		return
	}

	protocols, diags := types.ListValueFrom(ctx, types.StringType, endpoint.Protocols)
	diagnostics.Append(diags...)

	rules := make([]attr.Value, 0, len(endpoint.VersionSelector.VersionSelectionRules))
	for _, rule := range endpoint.VersionSelector.VersionSelectionRules {
		value, diags := types.ObjectValue(agentEndpointRuleAttrTypes, map[string]attr.Value{
			"type":               types.StringValue(rule.Type),
			"agent_version":      types.StringValue(rule.AgentVersion),
			"traffic_percentage": types.Int64Value(rule.TrafficPercentage),
		})
		diagnostics.Append(diags...)
		rules = append(rules, value)
	}
	rulesList, diags := types.ListValue(types.ObjectType{AttrTypes: agentEndpointRuleAttrTypes}, rules)
	diagnostics.Append(diags...)

	value, diags := types.ObjectValue(agentEndpointAttrTypes, map[string]attr.Value{
		"protocols":               protocols,
		"version_selection_rules": rulesList,
	})
	diagnostics.Append(diags...)
	c.AgentEndpoint = value
}

// optionalString keeps an unset optional attribute null rather than empty, so
// Terraform does not report a permanent diff against an absent API field.
func optionalString(value string) types.String {
	if value == "" {
		return types.StringNull()
	}
	return types.StringValue(value)
}

// createAgent publishes the first version of an agent and returns it, along
// with the agent's endpoint metadata.
func createAgent(ctx context.Context, client *clients.Client, name, description string, definition any, draft bool) (agentVersion, *agentEndpoint, error) {
	var created agentResponse
	err := client.JSON(ctx, http.MethodPost, "agents", agentRequest{
		Name:        name,
		Description: description,
		Definition:  definition,
		Draft:       draft,
	}, &created)
	return created.latestVersion(), created.AgentEndpoint, err
}

// readAgent returns the latest version of an agent, along with its endpoint metadata.
func readAgent(ctx context.Context, client *clients.Client, name string) (agentVersion, *agentEndpoint, error) {
	var agent agentResponse
	err := client.JSON(ctx, http.MethodGet, "agents/"+url.PathEscape(name), nil, &agent)
	return agent.latestVersion(), agent.AgentEndpoint, err
}

// updateAgent publishes a new version, because the service treats each agent
// version as immutable and PATCH does not change the definition.
func updateAgent(ctx context.Context, client *clients.Client, name, description string, definition any, draft bool) (agentVersion, error) {
	var version agentVersion
	err := client.JSON(ctx, http.MethodPost, "agents/"+url.PathEscape(name)+"/versions", agentRequest{
		Description: description,
		Definition:  definition,
		Draft:       draft,
	}, &version)
	return version, err
}

func validateAgentBeforeUpdate(ctx context.Context, client *clients.Client, name string, target supportedAgentDefinition, diagnostics *diag.Diagnostics) bool {
	version, _, err := readAgent(ctx, client, name)
	if err != nil {
		diagnostics.AddError("Unable to read "+target.expectedKind()+" agent before update", err.Error())
		return false
	}
	return decodeManagedDefinition(version, target, diagnostics)
}

// deleteAgent removes an agent, treating an already-absent agent as success.
func deleteAgent(ctx context.Context, client *clients.Client, name string) error {
	err := client.JSON(ctx, http.MethodDelete, "agents/"+url.PathEscape(name), nil, nil)
	if clients.IsNotFound(err) {
		return nil
	}
	return err
}

// clientFromProviderData resolves the configured client shared by every resource and data source.
func clientFromProviderData(providerData any, diagnostics *diag.Diagnostics) *clients.Client {
	if providerData == nil {
		return nil
	}
	client, ok := providerData.(*clients.Client)
	if !ok {
		diagnostics.AddError("Unexpected provider data", fmt.Sprintf("Expected *clients.Client, got %T.", providerData))
		return nil
	}
	return client
}

// validateDraftPreview rejects an explicit draft = true when the "draft_agents"
// preview feature was not enabled, so terraform validate/plan catches misuse
// before the API call (the service itself accepts draft without any
// Foundry-Features header, so this check is enforced by the provider only).
func validateDraftPreview(client *clients.Client, draft types.Bool, diagnostics *diag.Diagnostics) {
	if client == nil || draft.IsUnknown() || draft.IsNull() || !draft.ValueBool() {
		return
	}
	if !client.PreviewEnabled("draft_agents") {
		diagnostics.AddError(
			"Preview feature not enabled",
			`Setting "draft" requires the "draft_agents" preview feature. Add it to the provider's enable_preview attribute to use it.`,
		)
	}
}

type supportedAgentDefinition interface {
	expectedKind() string
	validateSupported() error
}

// decodeDefinition unmarshals an agent definition after verifying its kind.
func decodeDefinition(version agentVersion, target supportedAgentDefinition, diagnostics *diag.Diagnostics) bool {
	var header struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(version.Definition, &header); err != nil {
		diagnostics.AddError("Unable to decode agent definition", err.Error())
		return false
	}
	if header.Kind != target.expectedKind() {
		diagnostics.AddError(
			"Unexpected agent definition kind",
			fmt.Sprintf("Expected %q, but the service returned %q.", target.expectedKind(), header.Kind),
		)
		return false
	}
	if err := json.Unmarshal(version.Definition, target); err != nil {
		diagnostics.AddError("Unable to decode agent definition", err.Error())
		return false
	}
	return true
}

// decodeManagedDefinition rejects fields a resource would discard when it
// publishes the next immutable agent version.
func decodeManagedDefinition(version agentVersion, target supportedAgentDefinition, diagnostics *diag.Diagnostics) bool {
	if !decodeDefinition(version, target, diagnostics) {
		return false
	}
	if err := target.validateSupported(); err != nil {
		diagnostics.AddError("Unsupported "+target.expectedKind()+" agent definition", err.Error())
		return false
	}
	var fields []string
	if len(version.Metadata) > 0 {
		fields = append(fields, "metadata")
	}
	if hasJSONValue(version.Blueprint) {
		fields = append(fields, "blueprint")
	}
	if hasJSONValue(version.BlueprintReference) {
		fields = append(fields, "blueprint_reference")
	}
	if len(fields) > 0 {
		diagnostics.AddError(
			"Unsupported agent version fields",
			fmt.Sprintf(
				"The service returned unsupported version fields (%s); remove them before managing this agent because an update would otherwise discard them.",
				strings.Join(fields, ", "),
			),
		)
		return false
	}
	return true
}

func hasJSONValue(value json.RawMessage) bool {
	value = bytes.TrimSpace(value)
	return len(value) > 0 && !bytes.Equal(value, []byte("null"))
}

func hasMaterialJSON(value json.RawMessage) bool {
	if !hasJSONValue(value) {
		return false
	}
	value = bytes.TrimSpace(value)
	return !bytes.Equal(value, []byte("{}")) && !bytes.Equal(value, []byte("[]"))
}
