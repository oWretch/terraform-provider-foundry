package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
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

// agentCommonSchema returns the attributes every agent resource exposes.
func agentCommonSchema() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"name": schema.StringAttribute{
			Required:            true,
			MarkdownDescription: "Agent name. Changing this forces a new agent to be created.",
			PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
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
	ID          string          `json:"id"`
	Version     string          `json:"version"`
	Description string          `json:"description"`
	Definition  json.RawMessage `json:"definition"`
	AgentGUID   string          `json:"agent_guid"`
	Identity    *agentIdentity  `json:"instance_identity"`
	Draft       bool            `json:"draft"`
}

type agentResponse struct {
	Versions struct {
		Latest agentVersion `json:"latest"`
	} `json:"versions"`
	AgentEndpoint *agentEndpoint `json:"agent_endpoint"`
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
	return created.Versions.Latest, created.AgentEndpoint, err
}

// readAgent returns the latest version of an agent, along with its endpoint metadata.
func readAgent(ctx context.Context, client *clients.Client, name string) (agentVersion, *agentEndpoint, error) {
	var agent agentResponse
	err := client.JSON(ctx, http.MethodGet, "agents/"+name, nil, &agent)
	return agent.Versions.Latest, agent.AgentEndpoint, err
}

// updateAgent publishes a new version, because the service treats each agent
// version as immutable and PATCH does not change the definition.
func updateAgent(ctx context.Context, client *clients.Client, name, description string, definition any, draft bool) (agentVersion, error) {
	var version agentVersion
	err := client.JSON(ctx, http.MethodPost, "agents/"+name+"/versions", agentRequest{
		Description: description,
		Definition:  definition,
		Draft:       draft,
	}, &version)
	return version, err
}

// deleteAgent removes an agent, treating an already-absent agent as success.
func deleteAgent(ctx context.Context, client *clients.Client, name string) error {
	err := client.JSON(ctx, http.MethodDelete, "agents/"+name, nil, nil)
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

// decodeDefinition unmarshals an agent definition into a kind-specific struct.
func decodeDefinition(version agentVersion, target any, diagnostics *diag.Diagnostics) bool {
	if err := json.Unmarshal(version.Definition, target); err != nil {
		diagnostics.AddError("Unable to decode agent definition", err.Error())
		return false
	}
	return true
}
