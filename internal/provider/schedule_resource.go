package provider

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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

const schedulesPreviewFeature = "Schedules"

// schedulesPreviewFeatureName is the enable_preview value users write to opt in.
const schedulesPreviewFeatureName = "schedules"

var (
	_ resource.Resource                   = &scheduleResource{}
	_ resource.ResourceWithConfigure      = &scheduleResource{}
	_ resource.ResourceWithImportState    = &scheduleResource{}
	_ resource.ResourceWithValidateConfig = &scheduleResource{}
)

func NewScheduleResource() resource.Resource {
	return &scheduleResource{}
}

type scheduleResource struct {
	previewGate
}

// scheduleTriggerModel models the schedule's trigger. Exactly one of the
// three trigger kinds (Cron, Recurrence, OneTime) is used, selected by
// `type`. Fields not applicable to the selected type stay null.
type scheduleTriggerModel struct {
	Type           types.String `tfsdk:"type"`
	CronExpression types.String `tfsdk:"cron_expression"`
	StartTime      types.String `tfsdk:"start_time"`
	EndTime        types.String `tfsdk:"end_time"`
	TimeZone       types.String `tfsdk:"time_zone"`
	Interval       types.Int64  `tfsdk:"interval"`
	RecurrenceType types.String `tfsdk:"recurrence_type"`
	Hours          types.List   `tfsdk:"hours"`
	DaysOfWeek     types.List   `tfsdk:"days_of_week"`
	DaysOfMonth    types.List   `tfsdk:"days_of_month"`
	TriggerAt      types.String `tfsdk:"trigger_at"`
}

// scheduleTaskModel models the schedule's task. The service supports
// Evaluation and Insight task types; this provider models the Evaluation
// task fully and passes Insight's payload through as a raw JSON string,
// because the Insight task schema is not yet stable in the public API
// reference.
type scheduleTaskModel struct {
	Type          types.String `tfsdk:"type"`
	EvaluationID  types.String `tfsdk:"evaluation_id"`
	EvaluationRun types.String `tfsdk:"evaluation_run"`
	Insight       types.String `tfsdk:"insight"`
}

type scheduleModel struct {
	ID                 types.String         `tfsdk:"id"`
	DisplayName        types.String         `tfsdk:"display_name"`
	Description        types.String         `tfsdk:"description"`
	Enabled            types.Bool           `tfsdk:"enabled"`
	Trigger            scheduleTriggerModel `tfsdk:"trigger"`
	Task               scheduleTaskModel    `tfsdk:"task"`
	Tags               types.Map            `tfsdk:"tags"`
	Properties         types.Map            `tfsdk:"properties"`
	ProvisioningStatus types.String         `tfsdk:"provisioning_status"`
}

type scheduleTriggerRequest struct {
	Type       string                             `json:"type"`
	Expression string                             `json:"expression,omitempty"`
	StartTime  string                             `json:"startTime,omitempty"`
	EndTime    string                             `json:"endTime,omitempty"`
	TimeZone   string                             `json:"timeZone,omitempty"`
	Interval   int64                              `json:"interval,omitempty"`
	Schedule   *scheduleRecurrenceScheduleRequest `json:"schedule,omitempty"`
	TriggerAt  string                             `json:"triggerAt,omitempty"`
}

type scheduleRecurrenceScheduleRequest struct {
	Type        string   `json:"type"`
	Hours       []int64  `json:"hours,omitempty"`
	DaysOfWeek  []string `json:"daysOfWeek,omitempty"`
	DaysOfMonth []int64  `json:"daysOfMonth,omitempty"`
}

type scheduleTaskRequest struct {
	Type    string          `json:"type"`
	EvalID  string          `json:"evalId,omitempty"`
	EvalRun json.RawMessage `json:"evalRun,omitempty"`
	Insight json.RawMessage `json:"insight,omitempty"`
}

type scheduleRequest struct {
	DisplayName string                 `json:"displayName,omitempty"`
	Description string                 `json:"description,omitempty"`
	Enabled     bool                   `json:"enabled"`
	Trigger     scheduleTriggerRequest `json:"trigger"`
	Task        scheduleTaskRequest    `json:"task"`
	Tags        map[string]string      `json:"tags,omitempty"`
	Properties  map[string]string      `json:"properties,omitempty"`
}

type scheduleResponse struct {
	ID                 string                 `json:"id"`
	DisplayName        string                 `json:"displayName"`
	Description        string                 `json:"description"`
	Enabled            bool                   `json:"enabled"`
	ProvisioningStatus string                 `json:"provisioningStatus"`
	Trigger            scheduleTriggerRequest `json:"trigger"`
	Task               scheduleTaskRequest    `json:"task"`
	Tags               map[string]string      `json:"tags"`
	Properties         map[string]string      `json:"properties"`
}

func (m scheduleModel) request(ctx context.Context, diagnostics *diag.Diagnostics) scheduleRequest {
	request := scheduleRequest{
		DisplayName: m.DisplayName.ValueString(),
		Description: m.Description.ValueString(),
		Enabled:     m.Enabled.ValueBool(),
		Trigger:     m.Trigger.request(ctx, diagnostics),
		Task:        m.Task.request(diagnostics),
	}
	if !m.Tags.IsNull() {
		diagnostics.Append(m.Tags.ElementsAs(ctx, &request.Tags, false)...)
	}
	if !m.Properties.IsNull() {
		diagnostics.Append(m.Properties.ElementsAs(ctx, &request.Properties, false)...)
	}
	return request
}

func (t scheduleTriggerModel) request(ctx context.Context, diagnostics *diag.Diagnostics) scheduleTriggerRequest {
	request := scheduleTriggerRequest{
		Type:       t.Type.ValueString(),
		Expression: t.CronExpression.ValueString(),
		StartTime:  t.StartTime.ValueString(),
		EndTime:    t.EndTime.ValueString(),
		TimeZone:   t.TimeZone.ValueString(),
		Interval:   t.Interval.ValueInt64(),
		TriggerAt:  t.TriggerAt.ValueString(),
	}
	if !t.RecurrenceType.IsNull() {
		recurrence := &scheduleRecurrenceScheduleRequest{Type: t.RecurrenceType.ValueString()}
		if !t.Hours.IsNull() {
			diagnostics.Append(t.Hours.ElementsAs(ctx, &recurrence.Hours, false)...)
		}
		if !t.DaysOfWeek.IsNull() {
			diagnostics.Append(t.DaysOfWeek.ElementsAs(ctx, &recurrence.DaysOfWeek, false)...)
		}
		if !t.DaysOfMonth.IsNull() {
			diagnostics.Append(t.DaysOfMonth.ElementsAs(ctx, &recurrence.DaysOfMonth, false)...)
		}
		request.Schedule = recurrence
	}
	return request
}

func (t scheduleTaskModel) request(diagnostics *diag.Diagnostics) scheduleTaskRequest {
	request := scheduleTaskRequest{
		Type:   t.Type.ValueString(),
		EvalID: t.EvaluationID.ValueString(),
	}
	if !t.EvaluationRun.IsNull() && t.EvaluationRun.ValueString() != "" {
		request.EvalRun = json.RawMessage(t.EvaluationRun.ValueString())
	}
	if !t.Insight.IsNull() && t.Insight.ValueString() != "" {
		request.Insight = json.RawMessage(t.Insight.ValueString())
	}
	return request
}

func (m *scheduleModel) apply(ctx context.Context, response scheduleResponse, diagnostics *diag.Diagnostics) {
	m.ID = types.StringValue(response.ID)
	m.DisplayName = optionalString(response.DisplayName)
	m.Description = optionalString(response.Description)
	m.Enabled = types.BoolValue(response.Enabled)
	m.ProvisioningStatus = optionalString(response.ProvisioningStatus)

	m.Trigger = scheduleTriggerModel{
		Type:           types.StringValue(response.Trigger.Type),
		CronExpression: optionalString(response.Trigger.Expression),
		StartTime:      optionalString(response.Trigger.StartTime),
		EndTime:        optionalString(response.Trigger.EndTime),
		TimeZone:       optionalString(response.Trigger.TimeZone),
		TriggerAt:      optionalString(response.Trigger.TriggerAt),
	}
	if response.Trigger.Interval != 0 {
		m.Trigger.Interval = types.Int64Value(response.Trigger.Interval)
	} else {
		m.Trigger.Interval = types.Int64Null()
	}
	if response.Trigger.Schedule != nil {
		m.Trigger.RecurrenceType = types.StringValue(response.Trigger.Schedule.Type)
		m.Trigger.Hours = scheduleInt64ListOrNull(ctx, response.Trigger.Schedule.Hours, diagnostics)
		m.Trigger.DaysOfWeek = scheduleStringListOrNull(response.Trigger.Schedule.DaysOfWeek)
		m.Trigger.DaysOfMonth = scheduleInt64ListOrNull(ctx, response.Trigger.Schedule.DaysOfMonth, diagnostics)
	} else {
		m.Trigger.RecurrenceType = types.StringNull()
		m.Trigger.Hours = types.ListNull(types.Int64Type)
		m.Trigger.DaysOfWeek = types.ListNull(types.StringType)
		m.Trigger.DaysOfMonth = types.ListNull(types.Int64Type)
	}

	m.Task = scheduleTaskModel{
		Type:         types.StringValue(response.Task.Type),
		EvaluationID: optionalString(response.Task.EvalID),
	}
	if len(response.Task.EvalRun) > 0 {
		m.Task.EvaluationRun = types.StringValue(string(response.Task.EvalRun))
	} else {
		m.Task.EvaluationRun = types.StringNull()
	}
	if len(response.Task.Insight) > 0 {
		m.Task.Insight = types.StringValue(string(response.Task.Insight))
	} else {
		m.Task.Insight = types.StringNull()
	}

	if len(response.Tags) == 0 {
		m.Tags = types.MapNull(types.StringType)
	} else {
		value, diags := types.MapValueFrom(ctx, types.StringType, response.Tags)
		diagnostics.Append(diags...)
		m.Tags = value
	}
	if len(response.Properties) == 0 {
		m.Properties = types.MapNull(types.StringType)
	} else {
		value, diags := types.MapValueFrom(ctx, types.StringType, response.Properties)
		diagnostics.Append(diags...)
		m.Properties = value
	}
}

func scheduleInt64ListOrNull(ctx context.Context, values []int64, diagnostics *diag.Diagnostics) types.List {
	if len(values) == 0 {
		return types.ListNull(types.Int64Type)
	}
	value, diags := types.ListValueFrom(ctx, types.Int64Type, values)
	diagnostics.Append(diags...)
	return value
}

func scheduleStringListOrNull(values []string) types.List {
	return stringListOrNull(values)
}

func (m scheduleModel) path() string {
	return "schedules/" + m.ID.ValueString()
}

func (r *scheduleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_schedule"
}

func (r *scheduleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "~> **Preview:** This resource requires `enable_preview = [\"schedules\"]` on the provider. Preview features may change or be removed in any provider release without following semantic versioning.\n\n" +
			"Manages a schedule that runs an evaluation or insight task on a cron, recurrence, or one-time trigger.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Identifier of the schedule. Changing this forces a new schedule to be created.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"display_name": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Display name of the schedule.",
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Description of the schedule.",
			},
			"enabled": schema.BoolAttribute{
				Required:            true,
				MarkdownDescription: "Whether the schedule is enabled.",
			},
			"trigger": schema.SingleNestedAttribute{
				Required:            true,
				MarkdownDescription: "The schedule's trigger.",
				Attributes: map[string]schema.Attribute{
					"type": schema.StringAttribute{
						Required:            true,
						MarkdownDescription: "Trigger type. `Cron` fires on a cron expression. `Recurrence` fires on an interval and recurrence schedule. `OneTime` fires once at `trigger_at`.",
						Validators:          []validator.String{stringvalidator.OneOf("Cron", "Recurrence", "OneTime")},
					},
					"cron_expression": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "Cron expression defining the schedule frequency. Required when `type` is `Cron`.",
					},
					"start_time": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "ISO 8601 start time for `Cron` or `Recurrence` triggers.",
					},
					"end_time": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "ISO 8601 end time for `Cron` or `Recurrence` triggers.",
					},
					"time_zone": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "Time zone for the trigger. Defaults to `UTC` when unset.",
					},
					"interval": schema.Int64Attribute{
						Optional:            true,
						MarkdownDescription: "Interval for a `Recurrence` trigger. Required when `type` is `Recurrence`.",
					},
					"recurrence_type": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "Recurrence pattern for a `Recurrence` trigger: `Hourly`, `Daily`, `Weekly`, or `Monthly`. Required when `type` is `Recurrence`.",
						Validators:          []validator.String{stringvalidator.OneOf("Hourly", "Daily", "Weekly", "Monthly")},
					},
					"hours": schema.ListAttribute{
						Optional:            true,
						ElementType:         types.Int64Type,
						MarkdownDescription: "Hours of the day for a `Daily` recurrence schedule.",
					},
					"days_of_week": schema.ListAttribute{
						Optional:            true,
						ElementType:         types.StringType,
						MarkdownDescription: "Days of the week for a `Weekly` recurrence schedule.",
					},
					"days_of_month": schema.ListAttribute{
						Optional:            true,
						ElementType:         types.Int64Type,
						MarkdownDescription: "Days of the month for a `Monthly` recurrence schedule.",
					},
					"trigger_at": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "ISO 8601 timestamp at which a `OneTime` trigger fires. Required when `type` is `OneTime`.",
					},
				},
			},
			"task": schema.SingleNestedAttribute{
				Required:            true,
				MarkdownDescription: "The schedule's task.",
				Attributes: map[string]schema.Attribute{
					"type": schema.StringAttribute{
						Required:            true,
						MarkdownDescription: "Task type, `Evaluation` or `Insight`.",
						Validators:          []validator.String{stringvalidator.OneOf("Evaluation", "Insight")},
					},
					"evaluation_id": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "Identifier of the evaluation group. Required when `type` is `Evaluation`.",
					},
					"evaluation_run": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "JSON-encoded evaluation run payload. Required when `type` is `Evaluation`.",
					},
					"insight": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "JSON-encoded insight payload. Required when `type` is `Insight`.",
					},
				},
			},
			"tags": schema.MapAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Key-value tags attached to the schedule. Unlike `properties`, tags are fully mutable.",
			},
			"properties": schema.MapAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Key-value properties attached to the schedule. Properties are add-only; once added, a property cannot be removed.",
			},
			"provisioning_status": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Provisioning status of the schedule: `Creating`, `Updating`, `Deleting`, `Succeeded`, or `Failed`.",
			},
		},
	}
}

func (r *scheduleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client := clientFromProviderData(req.ProviderData, &resp.Diagnostics)
	r.previewGate = previewGate{client: client, feature: schedulesPreviewFeature, name: schedulesPreviewFeatureName}
}

func (r *scheduleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan scheduleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, err := r.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create schedule", err.Error())
		return
	}

	var response scheduleResponse
	if err := r.client.JSON(ctx, http.MethodPut, plan.path(), plan.request(ctx, &resp.Diagnostics), &response); err != nil {
		resp.Diagnostics.AddError("Unable to create schedule", err.Error())
		return
	}

	plan.apply(ctx, response, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *scheduleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state scheduleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, err := r.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read schedule", err.Error())
		return
	}

	var response scheduleResponse
	err = r.client.JSON(ctx, http.MethodGet, state.path(), nil, &response)
	if clients.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read schedule", err.Error())
		return
	}

	state.apply(ctx, response, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *scheduleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan scheduleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, err := r.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update schedule", err.Error())
		return
	}

	var response scheduleResponse
	if err := r.client.JSON(ctx, http.MethodPut, plan.path(), plan.request(ctx, &resp.Diagnostics), &response); err != nil {
		resp.Diagnostics.AddError("Unable to update schedule", err.Error())
		return
	}

	plan.apply(ctx, response, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *scheduleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state scheduleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, err := r.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to delete schedule", err.Error())
		return
	}

	err = r.client.JSON(ctx, http.MethodDelete, state.path(), nil, nil)
	if err != nil && !clients.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete schedule", err.Error())
	}
}

func (r *scheduleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// ValidateConfig delegates to previewGate. Go's embedding does not promote
// previewGate.ValidateResourceConfig to satisfy
// resource.ResourceWithValidateConfig on *scheduleResource automatically in
// all toolchains, so this thin wrapper makes the interface assertion
// explicit and stable.
func (r *scheduleResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	r.ValidateResourceConfig(ctx, req, resp)
}
