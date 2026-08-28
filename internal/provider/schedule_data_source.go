package provider

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource                   = &scheduleDataSource{}
	_ datasource.DataSourceWithConfigure      = &scheduleDataSource{}
	_ datasource.DataSourceWithValidateConfig = &scheduleDataSource{}
)

func NewScheduleDataSource() datasource.DataSource {
	return &scheduleDataSource{}
}

type scheduleDataSource struct {
	previewGate
}

func (d *scheduleDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_schedule"
}

func (d *scheduleDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "~> **Preview:** This data source requires `enable_preview = [\"schedules\"]` on the provider. Preview features may change or be removed in any provider release without following semantic versioning.\n\n" +
			"Looks up an existing schedule by ID.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Identifier of the schedule to look up.",
			},
			"display_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Display name of the schedule.",
			},
			"description": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Description of the schedule.",
			},
			"enabled": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the schedule is enabled.",
			},
			"trigger": schema.SingleNestedAttribute{
				Computed:            true,
				MarkdownDescription: "The schedule's trigger.",
				Attributes: map[string]schema.Attribute{
					"type": schema.StringAttribute{
						Computed:            true,
						MarkdownDescription: "Trigger type: `Cron`, `Recurrence`, or `OneTime`.",
					},
					"cron_expression": schema.StringAttribute{
						Computed:            true,
						MarkdownDescription: "Cron expression defining the schedule frequency, set when `type` is `Cron`.",
					},
					"start_time": schema.StringAttribute{
						Computed:            true,
						MarkdownDescription: "ISO 8601 start time for `Cron` or `Recurrence` triggers.",
					},
					"end_time": schema.StringAttribute{
						Computed:            true,
						MarkdownDescription: "ISO 8601 end time for `Cron` or `Recurrence` triggers.",
					},
					"time_zone": schema.StringAttribute{
						Computed:            true,
						MarkdownDescription: "Time zone for the trigger.",
					},
					"interval": schema.Int64Attribute{
						Computed:            true,
						MarkdownDescription: "Interval for a `Recurrence` trigger.",
					},
					"recurrence_type": schema.StringAttribute{
						Computed:            true,
						MarkdownDescription: "Recurrence pattern for a `Recurrence` trigger.",
					},
					"hours": schema.ListAttribute{
						Computed:            true,
						ElementType:         types.Int64Type,
						MarkdownDescription: "Hours of the day for a `Daily` recurrence schedule.",
					},
					"days_of_week": schema.ListAttribute{
						Computed:            true,
						ElementType:         types.StringType,
						MarkdownDescription: "Days of the week for a `Weekly` recurrence schedule.",
					},
					"days_of_month": schema.ListAttribute{
						Computed:            true,
						ElementType:         types.Int64Type,
						MarkdownDescription: "Days of the month for a `Monthly` recurrence schedule.",
					},
					"trigger_at": schema.StringAttribute{
						Computed:            true,
						MarkdownDescription: "ISO 8601 timestamp at which a `OneTime` trigger fires.",
					},
				},
			},
			"task": schema.SingleNestedAttribute{
				Computed:            true,
				MarkdownDescription: "The schedule's task.",
				Attributes: map[string]schema.Attribute{
					"type": schema.StringAttribute{
						Computed:            true,
						MarkdownDescription: "Task type, `Evaluation` or `Insight`.",
					},
					"evaluation_id": schema.StringAttribute{
						Computed:            true,
						MarkdownDescription: "Identifier of the evaluation group, set when `type` is `Evaluation`.",
					},
					"evaluation_run": schema.StringAttribute{
						Computed:            true,
						MarkdownDescription: "JSON-encoded evaluation run payload, set when `type` is `Evaluation`.",
					},
					"insight": schema.StringAttribute{
						Computed:            true,
						MarkdownDescription: "JSON-encoded insight payload, set when `type` is `Insight`.",
					},
				},
			},
			"tags": schema.MapAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Key-value tags attached to the schedule.",
			},
			"properties": schema.MapAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Key-value properties attached to the schedule.",
			},
			"provisioning_status": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Provisioning status of the schedule: `Creating`, `Updating`, `Deleting`, `Succeeded`, or `Failed`.",
			},
		},
	}
}

func (d *scheduleDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	client := clientFromProviderData(req.ProviderData, &resp.Diagnostics)
	d.previewGate = previewGate{client: client, feature: schedulesPreviewFeature, name: schedulesPreviewFeatureName}
}

func (d *scheduleDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config scheduleModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, err := d.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read schedule", err.Error())
		return
	}

	var response scheduleResponse
	if err := d.client.JSON(ctx, http.MethodGet, config.path(), nil, &response); err != nil {
		resp.Diagnostics.AddError("Unable to read schedule", err.Error())
		return
	}

	config.apply(ctx, response, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

// ValidateConfig delegates to previewGate. Go's embedding does not promote
// previewGate.ValidateDataSourceConfig to satisfy
// datasource.DataSourceWithValidateConfig on *scheduleDataSource
// automatically in all toolchains, so this thin wrapper makes the interface
// assertion explicit and stable.
func (d *scheduleDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	d.ValidateDataSourceConfig(ctx, req, resp)
}
