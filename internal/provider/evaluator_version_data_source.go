package provider

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource                   = &evaluatorVersionDataSource{}
	_ datasource.DataSourceWithConfigure      = &evaluatorVersionDataSource{}
	_ datasource.DataSourceWithValidateConfig = &evaluatorVersionDataSource{}
)

func NewEvaluatorVersionDataSource() datasource.DataSource {
	return &evaluatorVersionDataSource{}
}

type evaluatorVersionDataSource struct {
	previewGate
}

func (d *evaluatorVersionDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_evaluator_version"
}

func (d *evaluatorVersionDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: evaluationsPreviewNoteDataSource + "Looks up a single version of a Foundry evaluator.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the evaluator.",
			},
			"version": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Version of the evaluator to look up.",
			},
			"description": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Description of the evaluator version.",
			},
			"type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Evaluator kind: `prompt`, `code`, `rubric`, or `endpoint`.",
			},
			"prompt_text": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "LLM judge prompt template, when `type` is `prompt`.",
			},
			"code_text": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Inline Python evaluator source, when `type` is `code`.",
			},
			"blob_uri": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Blob URI of the Python evaluator source, when `type` is `code`.",
			},
			"dimensions": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Scoring dimensions, when `type` is `rubric`.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":          schema.StringAttribute{Computed: true, MarkdownDescription: "Stable identifier of the dimension."},
						"description": schema.StringAttribute{Computed: true, MarkdownDescription: "Description of the dimension shown to the LLM judge."},
						"weight":      schema.Float64Attribute{Computed: true, MarkdownDescription: "Relative weight of the dimension in the overall rubric score."},
					},
				},
			},
			"connection_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Name of the connection the endpoint evaluator calls, when `type` is `endpoint`.",
			},
			"metrics": schema.MapAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Metric names produced by this evaluator.",
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Service-assigned identifier of this evaluator version.",
			},
			"display_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Display name assigned to the evaluator version.",
			},
			"created_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Timestamp when the evaluator version was created.",
			},
			"modified_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Timestamp when the evaluator version was last modified.",
			},
		},
	}
}

// ValidateConfig delegates to previewGate. Go's embedding does not promote
// previewGate.ValidateDataSourceConfig to satisfy
// datasource.DataSourceWithValidateConfig on *evaluatorVersionDataSource
// automatically because the method has a pointer receiver-shaped signature
// mismatch; declare it explicitly instead.
func (d *evaluatorVersionDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	d.ValidateDataSourceConfig(ctx, req, resp)
}

func (d *evaluatorVersionDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	client := clientFromProviderData(req.ProviderData, &resp.Diagnostics)
	d.previewGate = previewGate{client: client, feature: evaluationsPreviewFeature, name: evaluationsPreviewFeatureName}
}

func (d *evaluatorVersionDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config evaluatorVersionModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, err := d.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Preview feature not enabled", err.Error())
		return
	}

	var response evaluatorVersionResponse
	if err := d.client.JSON(ctx, http.MethodGet, config.versionPath(), nil, &response); err != nil {
		resp.Diagnostics.AddError("Unable to read evaluator version", err.Error())
		return
	}

	config.Name = types.StringValue(response.Name)
	config.apply(ctx, response, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
