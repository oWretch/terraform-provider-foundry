package provider

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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
	_ resource.Resource                   = &evaluationRuleResource{}
	_ resource.ResourceWithConfigure      = &evaluationRuleResource{}
	_ resource.ResourceWithImportState    = &evaluationRuleResource{}
	_ resource.ResourceWithValidateConfig = &evaluationRuleResource{}
)

func NewEvaluationRuleResource() resource.Resource {
	return &evaluationRuleResource{}
}

type evaluationRuleResource struct {
	previewGate
}

// evaluationRuleModel models an evaluation rule: a trigger (event_type and
// agent filter) paired with an action that starts a continuous evaluation
// run for a foundry_evaluation. Execution of the resulting runs is left
// entirely to the service; Terraform manages only the rule definition.
type evaluationRuleModel struct {
	ID          types.String `tfsdk:"id"`
	DisplayName types.String `tfsdk:"display_name"`
	Description types.String `tfsdk:"description"`
	EventType   types.String `tfsdk:"event_type"`
	AgentName   types.String `tfsdk:"agent_name"`
	EvalID      types.String `tfsdk:"eval_id"`
}

type evaluationRuleFilterWire struct {
	AgentName string `json:"agentName"`
}

type evaluationRuleActionWire struct {
	Type   string `json:"type"`
	EvalID string `json:"evalId"`
}

type evaluationRuleRequest struct {
	DisplayName string                   `json:"displayName"`
	Description string                   `json:"description,omitempty"`
	EventType   string                   `json:"eventType"`
	Filter      evaluationRuleFilterWire `json:"filter"`
	Action      evaluationRuleActionWire `json:"action"`
}

type evaluationRuleResponse struct {
	ID          string                   `json:"id"`
	DisplayName string                   `json:"displayName"`
	Description string                   `json:"description"`
	EventType   string                   `json:"eventType"`
	Filter      evaluationRuleFilterWire `json:"filter"`
	Action      evaluationRuleActionWire `json:"action"`
}

func (m evaluationRuleModel) request() evaluationRuleRequest {
	return evaluationRuleRequest{
		DisplayName: m.DisplayName.ValueString(),
		Description: m.Description.ValueString(),
		EventType:   m.EventType.ValueString(),
		Filter: evaluationRuleFilterWire{
			AgentName: m.AgentName.ValueString(),
		},
		Action: evaluationRuleActionWire{
			Type:   "continuousEvaluation",
			EvalID: m.EvalID.ValueString(),
		},
	}
}

func (m *evaluationRuleModel) apply(response evaluationRuleResponse) {
	m.ID = types.StringValue(response.ID)
	m.DisplayName = types.StringValue(response.DisplayName)
	m.Description = optionalString(response.Description)
	m.EventType = types.StringValue(response.EventType)
	m.AgentName = types.StringValue(response.Filter.AgentName)
	m.EvalID = types.StringValue(response.Action.EvalID)
}

func (m evaluationRuleModel) path() string {
	return "evaluationrules/" + m.ID.ValueString()
}

func (r *evaluationRuleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_evaluation_rule"
}

func (r *evaluationRuleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: evaluationsPreviewNote + "Manages a Foundry evaluation rule: a trigger, scoped to an agent, that starts a continuous evaluation run against a `foundry_evaluation` whenever the trigger event occurs. Terraform manages only the rule definition; the evaluation runs it starts are not modeled as Terraform resources.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Caller-supplied identifier for the evaluation rule.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"display_name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Display name of the evaluation rule.",
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Description of the evaluation rule.",
			},
			"event_type": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Event that triggers the rule. One of `manual` or `responseCompleted`.",
				Validators:          []validator.String{stringvalidator.OneOf("manual", "responseCompleted")},
			},
			"agent_name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the Foundry agent this rule is scoped to.",
			},
			"eval_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "ID of the `foundry_evaluation` to run when the rule triggers.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
		},
	}
}

func (r *evaluationRuleResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	r.ValidateResourceConfig(ctx, req, resp)
}

func (r *evaluationRuleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client := clientFromProviderData(req.ProviderData, &resp.Diagnostics)
	r.previewGate = previewGate{client: client, feature: evaluationsPreviewFeature, name: evaluationsPreviewFeatureName}
}

func (r *evaluationRuleResource) put(ctx context.Context, plan *evaluationRuleModel) (evaluationRuleResponse, error) {
	var response evaluationRuleResponse
	err := r.client.JSON(ctx, http.MethodPut, plan.path(), plan.request(), &response)
	return response, err
}

func (r *evaluationRuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan evaluationRuleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, err := r.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Preview feature not enabled", err.Error())
		return
	}

	response, err := r.put(ctx, &plan)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create evaluation rule", err.Error())
		return
	}

	plan.apply(response)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *evaluationRuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state evaluationRuleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, err := r.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Preview feature not enabled", err.Error())
		return
	}

	var response evaluationRuleResponse
	err = r.client.JSON(ctx, http.MethodGet, state.path(), nil, &response)
	if clients.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read evaluation rule", err.Error())
		return
	}

	state.apply(response)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *evaluationRuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan evaluationRuleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, err := r.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Preview feature not enabled", err.Error())
		return
	}

	response, err := r.put(ctx, &plan)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update evaluation rule", err.Error())
		return
	}

	plan.apply(response)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *evaluationRuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state evaluationRuleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, err := r.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Preview feature not enabled", err.Error())
		return
	}

	err = r.client.JSON(ctx, http.MethodDelete, state.path(), nil, nil)
	if err != nil && !clients.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete evaluation rule", err.Error())
	}
}

func (r *evaluationRuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
