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

const routinesPreviewFeature = "Routines"

// routinesPreviewFeatureName is the enable_preview value users write to opt in.
const routinesPreviewFeatureName = "routines"

var (
	_ resource.Resource                   = &routineResource{}
	_ resource.ResourceWithConfigure      = &routineResource{}
	_ resource.ResourceWithImportState    = &routineResource{}
	_ resource.ResourceWithValidateConfig = &routineResource{}
)

func NewRoutineResource() resource.Resource {
	return &routineResource{}
}

type routineResource struct {
	previewGate
}

// routineTriggerModel models the single supported trigger of a routine. The
// service accepts exactly one trigger keyed by a caller-chosen name; this
// provider always uses the key "trigger" because Terraform models a single
// nested attribute more naturally than an arbitrary-keyed map for the common
// schedule/timer cases this resource supports.
type routineTriggerModel struct {
	Type           types.String `tfsdk:"type"`
	CronExpression types.String `tfsdk:"cron_expression"`
	TimeZone       types.String `tfsdk:"time_zone"`
	At             types.String `tfsdk:"at"`
}

type routineModel struct {
	Name        types.String        `tfsdk:"name"`
	Description types.String        `tfsdk:"description"`
	Enabled     types.Bool          `tfsdk:"enabled"`
	Trigger     routineTriggerModel `tfsdk:"trigger"`
	ActionType  types.String        `tfsdk:"action_type"`
	AgentName   types.String        `tfsdk:"agent_name"`
	Input       types.String        `tfsdk:"input"`
	SessionID   types.String        `tfsdk:"session_id"`
	CreatedAt   types.Int64         `tfsdk:"created_at"`
	UpdatedAt   types.Int64         `tfsdk:"updated_at"`
}

// routineTriggerRequest is the wire shape of a single trigger entry.
type routineTriggerRequest struct {
	Type           string `json:"type"`
	CronExpression string `json:"cron_expression,omitempty"`
	TimeZone       string `json:"time_zone,omitempty"`
	At             string `json:"at,omitempty"`
}

type routineActionRequest struct {
	Type      string `json:"type"`
	AgentName string `json:"agent_name"`
	Input     string `json:"input,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}

type routineRequest struct {
	Description string                           `json:"description,omitempty"`
	Enabled     bool                             `json:"enabled"`
	Triggers    map[string]routineTriggerRequest `json:"triggers"`
	Action      routineActionRequest             `json:"action"`
}

type routineResponse struct {
	Name        string                           `json:"name"`
	Description string                           `json:"description"`
	Enabled     bool                             `json:"enabled"`
	Triggers    map[string]routineTriggerRequest `json:"triggers"`
	Action      routineActionRequest             `json:"action"`
	CreatedAt   int64                            `json:"created_at"`
	UpdatedAt   int64                            `json:"updated_at"`
}

// routineTriggerName is the key this provider always uses for a routine's
// single trigger. The service keys triggers by an arbitrary caller-chosen
// name; a fixed key keeps the Terraform schema flat for the single-trigger
// case this resource supports.
const routineTriggerName = "trigger"

func (m routineModel) request() routineRequest {
	request := routineRequest{
		Description: m.Description.ValueString(),
		Enabled:     m.Enabled.ValueBool(),
		Triggers: map[string]routineTriggerRequest{
			routineTriggerName: {
				Type:           m.Trigger.Type.ValueString(),
				CronExpression: m.Trigger.CronExpression.ValueString(),
				TimeZone:       m.Trigger.TimeZone.ValueString(),
				At:             m.Trigger.At.ValueString(),
			},
		},
		Action: routineActionRequest{
			Type:      m.ActionType.ValueString(),
			AgentName: m.AgentName.ValueString(),
			Input:     m.Input.ValueString(),
			SessionID: m.SessionID.ValueString(),
		},
	}
	return request
}

func (m *routineModel) apply(response routineResponse) {
	m.Name = types.StringValue(response.Name)
	m.Description = optionalString(response.Description)
	m.Enabled = types.BoolValue(response.Enabled)
	m.ActionType = types.StringValue(response.Action.Type)
	m.AgentName = types.StringValue(response.Action.AgentName)
	m.Input = optionalString(response.Action.Input)
	m.SessionID = optionalString(response.Action.SessionID)
	m.CreatedAt = types.Int64Value(response.CreatedAt)
	m.UpdatedAt = types.Int64Value(response.UpdatedAt)

	trigger := response.Triggers[routineTriggerName]
	// Fall back to the single entry present when the response used a
	// different key than routineTriggerName (e.g. import of a routine
	// created outside this provider).
	if trigger.Type == "" {
		for _, t := range response.Triggers {
			trigger = t
			break
		}
	}
	m.Trigger = routineTriggerModel{
		Type:           types.StringValue(trigger.Type),
		CronExpression: optionalString(trigger.CronExpression),
		TimeZone:       optionalString(trigger.TimeZone),
		At:             optionalString(trigger.At),
	}
}

func (m routineModel) path() string {
	return "routines/" + m.Name.ValueString()
}

func (r *routineResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_routine"
}

func (r *routineResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "~> **Preview:** This resource requires `enable_preview = [\"routines\"]` on the provider. Preview features may change or be removed in any provider release without following semantic versioning.\n\n" +
			"Manages a routine, a named automation rule that triggers an agent on a schedule, at a specific time, or in response to an external event. Only a schedule (cron) or timer trigger is currently supported.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the routine. Changing this forces a new routine to be created.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Description of the routine.",
			},
			"enabled": schema.BoolAttribute{
				Required:            true,
				MarkdownDescription: "Whether the routine is enabled and will fire on its trigger.",
			},
			"trigger": schema.SingleNestedAttribute{
				Required:            true,
				MarkdownDescription: "The routine's trigger. The service currently supports only one trigger per routine, and the trigger cannot be changed after creation; changing it forces a new routine to be created.",
				Attributes: map[string]schema.Attribute{
					"type": schema.StringAttribute{
						Required:            true,
						MarkdownDescription: "Trigger type. `schedule` fires repeatedly on a cron expression (minimum interval of five minutes). `timer` fires once at a specific future date and time.",
						Validators:          []validator.String{stringvalidator.OneOf("schedule", "timer")},
					},
					"cron_expression": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "Cron expression defining the trigger schedule. Required when `type` is `schedule`.",
					},
					"time_zone": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "IANA time zone used to interpret `cron_expression`. Defaults to UTC when unset.",
					},
					"at": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "ISO 8601 timestamp with an explicit UTC offset at which the routine fires once. Required when `type` is `timer`.",
					},
				},
			},
			"action_type": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Action invoked when the routine fires. `invoke_agent_responses_api` invokes the agent through the Responses API. `invoke_agent_invocations_api` invokes the agent through the Invocations API.",
				Validators:          []validator.String{stringvalidator.OneOf("invoke_agent_responses_api", "invoke_agent_invocations_api")},
			},
			"agent_name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the agent invoked when the routine fires. The agent must authenticate through its own configured identity; routines cannot invoke an agent that requires an end-user identity.",
			},
			"input": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Input passed to the agent when the routine fires.",
			},
			"session_id": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Session identifier used for `invoke_agent_invocations_api` actions.",
			},
			"created_at": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Unix timestamp when the routine was created.",
			},
			"updated_at": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Unix timestamp when the routine was last updated.",
			},
		},
	}
}

func (r *routineResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client := clientFromProviderData(req.ProviderData, &resp.Diagnostics)
	r.previewGate = previewGate{client: client, feature: routinesPreviewFeature, name: routinesPreviewFeatureName}
}

func (r *routineResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan routineModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, err := r.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create routine", err.Error())
		return
	}

	var response routineResponse
	if err := r.client.JSON(ctx, http.MethodPut, plan.path(), plan.request(), &response); err != nil {
		resp.Diagnostics.AddError("Unable to create routine", err.Error())
		return
	}

	plan.apply(response)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *routineResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state routineModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, err := r.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read routine", err.Error())
		return
	}

	var response routineResponse
	err = r.client.JSON(ctx, http.MethodGet, state.path(), nil, &response)
	if clients.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read routine", err.Error())
		return
	}

	state.apply(response)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *routineResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan routineModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, err := r.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update routine", err.Error())
		return
	}

	var response routineResponse
	if err := r.client.JSON(ctx, http.MethodPut, plan.path(), plan.request(), &response); err != nil {
		resp.Diagnostics.AddError("Unable to update routine", err.Error())
		return
	}

	plan.apply(response)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *routineResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state routineModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, err := r.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to delete routine", err.Error())
		return
	}

	err = r.client.JSON(ctx, http.MethodDelete, state.path(), nil, nil)
	if err != nil && !clients.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete routine", err.Error())
	}
}

func (r *routineResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
}

// ValidateConfig delegates to previewGate. Go's embedding does not promote
// previewGate.ValidateResourceConfig to satisfy
// resource.ResourceWithValidateConfig on *routineResource automatically in
// all toolchains, so this thin wrapper makes the interface assertion
// explicit and stable.
func (r *routineResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	r.ValidateResourceConfig(ctx, req, resp)
}
