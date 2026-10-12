package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

var (
	_ resource.Resource                   = &agentRoutingResource{}
	_ resource.ResourceWithConfigure      = &agentRoutingResource{}
	_ resource.ResourceWithImportState    = &agentRoutingResource{}
	_ resource.ResourceWithValidateConfig = &agentRoutingResource{}
)

func NewAgentRoutingResource() resource.Resource {
	return &agentRoutingResource{}
}

type agentRoutingResource struct {
	previewGate
}

type agentRoutingModel struct {
	ID                    types.String `tfsdk:"id"`
	AgentName             types.String `tfsdk:"agent_name"`
	VersionSelectionRules types.List   `tfsdk:"version_selection_rules"`
	ActiveVersion         types.String `tfsdk:"active_version"`
}

type agentRoutingRuleModel struct {
	AgentVersion types.String `tfsdk:"agent_version"`
}

var agentRoutingRuleAttrTypes = map[string]attr.Type{
	"agent_version": types.StringType,
}

func (r *agentRoutingResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_agent_routing"
}

func (r *agentRoutingResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "~> **Preview:** requires `enable_preview` to include `\"agent_endpoints\"`.\n\n" +
			"Manages routing on an existing agent's stable endpoint, independently of its definition. Configure exactly one `version_selection_rules` block. The provider routes 100% of traffic to its target with a `FixedRatio` rule internally; the rule type and percentages are not configurable. Split or non-100% routing cannot be managed by this resource. Inspect the read-only endpoint metadata on agent resources or data sources for all service-returned rules and percentages. Only one routing resource should manage a given agent. Destroying this resource leaves routing unchanged and removes it from Terraform state; it does not delete the agent or reset traffic.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true, MarkdownDescription: "Agent name, used as the routing resource identifier.",
			},
			"agent_name": schema.StringAttribute{
				Required: true, MarkdownDescription: "Name of the existing agent. Changing this relinquishes management of the old agent's routing and manages the new agent's routing.",
				Validators:    agentNameValidators,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"active_version": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Version receiving all stable-endpoint traffic. Resolves `@latest` to the latest version number.",
			},
		},
		Blocks: map[string]schema.Block{
			"version_selection_rules": schema.ListNestedBlock{
				MarkdownDescription: "Exactly one routing rule is required. Use an explicit version to pin traffic or `@latest` to automatically serve new versions. The provider sends a `FixedRatio` rule at 100% internally; neither the rule type nor the percentage is configurable. Routing updates do not create agent versions.",
				Validators:          []validator.List{listvalidator.SizeBetween(1, 1)},
				NestedObject: schema.NestedBlockObject{Attributes: map[string]schema.Attribute{
					"agent_version": schema.StringAttribute{
						Required: true, MarkdownDescription: "Existing agent version or `@latest`.",
						Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
					},
				}},
			},
		},
	}
}

func (r *agentRoutingResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.previewGate = previewGate{
		client: clientFromProviderData(req.ProviderData, &resp.Diagnostics),
		name:   "agent_endpoints", feature: "AgentEndpoints",
	}
}

func (r *agentRoutingResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	r.ValidateResourceConfig(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		return
	}
	var config agentRoutingModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if !resp.Diagnostics.HasError() {
		validateOrdinaryAgentRouting(config.VersionSelectionRules, &resp.Diagnostics)
	}
}

func validateOrdinaryAgentRouting(rules types.List, diagnostics *diag.Diagnostics) {
	if rules.IsUnknown() {
		return
	}
	if rules.IsNull() || len(rules.Elements()) != 1 {
		diagnostics.AddError("Invalid routing rule count", "Configure exactly one version_selection_rules block. Multiple routing targets are not supported yet.")
	}
}

func (m *agentRoutingModel) apply(ctx context.Context, agent agentResponse, diagnostics *diag.Diagnostics) {
	m.ID = m.AgentName
	m.ActiveVersion = activeAgentVersion(agent.Versions.Latest.Version, agent.AgentEndpoint)
	if agent.AgentEndpoint == nil {
		diagnostics.AddError("Agent routing unavailable", "The service did not return an agent endpoint. No routing rules can be read.")
		return
	}
	if rules := agent.AgentEndpoint.VersionSelector.VersionSelectionRules; len(rules) != 1 ||
		rules[0].Type != "FixedRatio" || rules[0].TrafficPercentage != 100 || rules[0].AgentVersion == "" {
		diagnostics.AddError("Unsupported agent routing", "This resource can only manage a single FixedRatio rule at 100%. Read the agent's endpoint metadata through an agent resource or data source to inspect other routing configurations.")
		return
	}
	rules := make([]agentRoutingRuleModel, 0, len(agent.AgentEndpoint.VersionSelector.VersionSelectionRules))
	for _, rule := range agent.AgentEndpoint.VersionSelector.VersionSelectionRules {
		rules = append(rules, agentRoutingRuleModel{
			AgentVersion: types.StringValue(rule.AgentVersion),
		})
	}
	value, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: agentRoutingRuleAttrTypes}, rules)
	diagnostics.Append(diags...)
	m.VersionSelectionRules = value
}

func (r *agentRoutingResource) read(ctx context.Context, name string) (agentResponse, error) {
	var agent agentResponse
	err := r.client.JSON(ctx, http.MethodGet, "agents/"+url.PathEscape(name), nil, &agent)
	return agent, err
}

func (r *agentRoutingResource) patch(ctx context.Context, plan agentRoutingModel, diagnostics *diag.Diagnostics) (agentResponse, error) {
	ctx, err := r.previewContext(ctx)
	if err != nil {
		return agentResponse{}, err
	}
	validateOrdinaryAgentRouting(plan.VersionSelectionRules, diagnostics)
	var rules []agentRoutingRuleModel
	diagnostics.Append(plan.VersionSelectionRules.ElementsAs(ctx, &rules, false)...)
	if diagnostics.HasError() {
		return agentResponse{}, fmt.Errorf("invalid agent routing configuration")
	}
	if len(rules) != 1 || rules[0].AgentVersion.IsUnknown() ||
		rules[0].AgentVersion.IsNull() || rules[0].AgentVersion.ValueString() == "" {
		return agentResponse{}, fmt.Errorf("routing requires one rule with a known, nonempty target version")
	}
	current, err := r.read(ctx, plan.AgentName.ValueString())
	if err != nil {
		return agentResponse{}, err
	}
	if current.AgentEndpoint == nil {
		return agentResponse{}, fmt.Errorf("the service did not return an agent endpoint")
	}
	if selector := current.AgentEndpoint.VersionSelector; selector.ExperimentID != "" || selector.Salt != "" {
		return agentResponse{}, fmt.Errorf("this agent has service-owned experiment routing; it cannot be overwritten by ordinary routing management")
	}
	body := struct {
		AgentEndpoint struct {
			VersionSelector struct {
				VersionSelectionRules []agentEndpointRule `json:"version_selection_rules"`
			} `json:"version_selector"`
		} `json:"agent_endpoint"`
	}{}
	for _, rule := range rules {
		body.AgentEndpoint.VersionSelector.VersionSelectionRules = append(body.AgentEndpoint.VersionSelector.VersionSelectionRules, agentEndpointRule{
			Type: "FixedRatio", AgentVersion: rule.AgentVersion.ValueString(), TrafficPercentage: 100,
		})
	}
	if err := r.client.JSONWithContentType(ctx, http.MethodPatch, "agents/"+url.PathEscape(plan.AgentName.ValueString()), "application/merge-patch+json", body, nil); err != nil {
		return agentResponse{}, err
	}
	updated, err := r.read(ctx, plan.AgentName.ValueString())
	if err != nil {
		return agentResponse{}, err
	}
	if updated.AgentEndpoint == nil || len(updated.AgentEndpoint.VersionSelector.VersionSelectionRules) != len(body.AgentEndpoint.VersionSelector.VersionSelectionRules) {
		return agentResponse{}, fmt.Errorf("the service did not return the configured routing rules after PATCH")
	}
	for i, rule := range body.AgentEndpoint.VersionSelector.VersionSelectionRules {
		if updated.AgentEndpoint.VersionSelector.VersionSelectionRules[i] != rule {
			return agentResponse{}, fmt.Errorf("the service returned different routing rules after PATCH")
		}
	}
	return updated, nil
}

func (r *agentRoutingResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan agentRoutingModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	agent, err := r.patch(ctx, plan, &resp.Diagnostics)
	if err != nil {
		resp.Diagnostics.AddError("Unable to configure agent routing", err.Error())
		return
	}
	plan.apply(ctx, agent, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *agentRoutingResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan agentRoutingModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	agent, err := r.patch(ctx, plan, &resp.Diagnostics)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update agent routing", err.Error())
		return
	}
	plan.apply(ctx, agent, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *agentRoutingResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state agentRoutingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, err := r.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Preview feature not enabled", err.Error())
		return
	}
	agent, err := r.read(ctx, state.AgentName.ValueString())
	if clients.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read agent routing", err.Error())
		return
	}
	state.apply(ctx, agent, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *agentRoutingResource) Delete(ctx context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.State.RemoveResource(ctx)
}

func (r *agentRoutingResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("agent_name"), req, resp)
}
