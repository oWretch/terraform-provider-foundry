package provider

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
)

var (
	_ datasource.DataSource                   = &evaluationRuleDataSource{}
	_ datasource.DataSourceWithConfigure      = &evaluationRuleDataSource{}
	_ datasource.DataSourceWithValidateConfig = &evaluationRuleDataSource{}
)

func NewEvaluationRuleDataSource() datasource.DataSource {
	return &evaluationRuleDataSource{}
}

type evaluationRuleDataSource struct {
	previewGate
}

func (d *evaluationRuleDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_evaluation_rule"
}

func (d *evaluationRuleDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: evaluationsPreviewNoteDataSource + "Looks up a Foundry evaluation rule by ID.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Identifier of the evaluation rule.",
			},
			"display_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Display name of the evaluation rule.",
			},
			"description": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Description of the evaluation rule.",
			},
			"event_type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Event that triggers the rule.",
			},
			"agent_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Name of the Foundry agent this rule is scoped to.",
			},
			"eval_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "ID of the evaluation run when the rule triggers.",
			},
		},
	}
}

func (d *evaluationRuleDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	d.ValidateDataSourceConfig(ctx, req, resp)
}

func (d *evaluationRuleDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	client := clientFromProviderData(req.ProviderData, &resp.Diagnostics)
	d.previewGate = previewGate{client: client, feature: evaluationsPreviewFeature, name: evaluationsPreviewFeatureName}
}

func (d *evaluationRuleDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config evaluationRuleModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, err := d.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Preview feature not enabled", err.Error())
		return
	}

	var response evaluationRuleResponse
	if err := d.client.JSON(ctx, http.MethodGet, config.path(), nil, &response); err != nil {
		resp.Diagnostics.AddError("Unable to read evaluation rule", err.Error())
		return
	}

	config.apply(response)
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
