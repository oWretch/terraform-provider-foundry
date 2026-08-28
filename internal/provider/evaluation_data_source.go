package provider

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource                   = &evaluationDataSource{}
	_ datasource.DataSourceWithConfigure      = &evaluationDataSource{}
	_ datasource.DataSourceWithValidateConfig = &evaluationDataSource{}
)

func NewEvaluationDataSource() datasource.DataSource {
	return &evaluationDataSource{}
}

type evaluationDataSource struct {
	previewGate
}

func (d *evaluationDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_evaluation"
}

func (d *evaluationDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: evaluationsPreviewNoteDataSource + "Looks up a Foundry evaluation definition by ID.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Service-assigned evaluation identifier.",
			},
			"name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Name of the evaluation.",
			},
			"data_source_item_schema": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "JSON schema (as a JSON-encoded string) describing a single evaluation data item.",
			},
			"metadata": schema.MapAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Key-value metadata attached to the evaluation.",
			},
			"testing_criteria": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Testing criteria evaluated against each data source item.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name":              schema.StringAttribute{Computed: true, MarkdownDescription: "Name of this testing criterion within the evaluation."},
						"evaluator_name":    schema.StringAttribute{Computed: true, MarkdownDescription: "Name of the referenced evaluator."},
						"evaluator_version": schema.StringAttribute{Computed: true, MarkdownDescription: "Evaluator version pinned by this criterion, if any."},
						"data_mapping": schema.MapAttribute{
							Computed:            true,
							ElementType:         types.StringType,
							MarkdownDescription: "Maps evaluator input field names to data item field references.",
						},
					},
				},
			},
		},
	}
}

func (d *evaluationDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	d.ValidateDataSourceConfig(ctx, req, resp)
}

func (d *evaluationDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	client := clientFromProviderData(req.ProviderData, &resp.Diagnostics)
	d.previewGate = previewGate{client: client, feature: evaluationsPreviewFeature, name: evaluationsPreviewFeatureName}
}

func (d *evaluationDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config evaluationModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, err := d.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Preview feature not enabled", err.Error())
		return
	}

	var response evaluationResponse
	if err := d.client.JSON(ctx, http.MethodGet, config.path(), nil, &response); err != nil {
		resp.Diagnostics.AddError("Unable to read evaluation", err.Error())
		return
	}

	config.apply(ctx, response, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
