package provider

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

var (
	_ datasource.DataSource              = &routineDataSource{}
	_ datasource.DataSourceWithConfigure = &routineDataSource{}
)

func NewRoutineDataSource() datasource.DataSource {
	return &routineDataSource{}
}

type routineDataSource struct {
	client *clients.Client
}

func (d *routineDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_routine"
}

func (d *routineDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up an existing routine by name.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the routine to look up.",
			},
			"description": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Description of the routine.",
			},
			"enabled": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the routine is enabled and will fire on its trigger.",
			},
			"trigger": schema.SingleNestedAttribute{
				Computed:            true,
				MarkdownDescription: "The routine's trigger.",
				Attributes: map[string]schema.Attribute{
					"type": schema.StringAttribute{
						Computed:            true,
						MarkdownDescription: "Trigger type, `schedule` or `timer`.",
					},
					"cron_expression": schema.StringAttribute{
						Computed:            true,
						MarkdownDescription: "Cron expression defining the trigger schedule, set when `type` is `schedule`.",
					},
					"time_zone": schema.StringAttribute{
						Computed:            true,
						MarkdownDescription: "IANA time zone used to interpret `cron_expression`.",
					},
					"at": schema.StringAttribute{
						Computed:            true,
						MarkdownDescription: "ISO 8601 timestamp at which the routine fires once, set when `type` is `timer`.",
					},
				},
			},
			"action_type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Action invoked when the routine fires.",
			},
			"agent_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Name of the agent invoked when the routine fires.",
			},
			"input": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Input passed to the agent when the routine fires.",
			},
			"session_id": schema.StringAttribute{
				Computed:            true,
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

func (d *routineDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (d *routineDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config routineModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var response routineResponse
	if err := d.client.JSON(ctx, http.MethodGet, config.path(), nil, &response); err != nil {
		resp.Diagnostics.AddError("Unable to read routine", err.Error())
		return
	}

	config.apply(response)
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
