package provider

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

var (
	_ datasource.DataSource              = &modelVersionDataSource{}
	_ datasource.DataSourceWithConfigure = &modelVersionDataSource{}
)

func NewModelVersionDataSource() datasource.DataSource {
	return &modelVersionDataSource{}
}

// modelVersionDataSource reads a single custom model version registered on the
// project. This is distinct from foundry_deployments, which lists the model
// deployments the project can call.
type modelVersionDataSource struct {
	client *clients.Client
}

type modelVersionDataSourceModel struct {
	Name        types.String `tfsdk:"name"`
	Version     types.String `tfsdk:"version"`
	ID          types.String `tfsdk:"id"`
	BlobURI     types.String `tfsdk:"blob_uri"`
	WeightType  types.String `tfsdk:"weight_type"`
	BaseModel   types.String `tfsdk:"base_model"`
	Description types.String `tfsdk:"description"`
	Tags        types.Map    `tfsdk:"tags"`
	Warnings    types.List   `tfsdk:"warnings"`
}

func (d *modelVersionDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_model_version"
}

func (d *modelVersionDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a single version of a custom model registered on the project, for referencing an artifact this configuration does not own.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the model to look up.",
			},
			"version": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Version of the model to look up.",
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Service-assigned model asset identifier.",
			},
			"blob_uri": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "URI of the model artifact in the storage the service issued for this version.",
			},
			"weight_type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "How the artifact's weights are stored: `FullWeight`, `LoRA`, or `DraftModel`.",
			},
			"base_model": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Asset ID of the base model this artifact adapts.",
			},
			"description": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Description recorded for the model version.",
			},
			"tags": schema.MapAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Key-value tags attached to the model version.",
			},
			"warnings": schema.ListAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Advisory warnings the service derived from inspecting the artifact.",
			},
		},
	}
}

func (d *modelVersionDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (d *modelVersionDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config modelVersionDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var response modelVersionResponse
	err := d.client.JSON(ctx, http.MethodGet, modelVersionModel{
		Name:    config.Name,
		Version: config.Version,
	}.path(), nil, &response)
	if clients.IsNotFound(err) {
		resp.Diagnostics.AddError("Model version not found", "No model version matched the given name and version.")
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read the model version", err.Error())
		return
	}

	config.ID = types.StringValue(response.ID)
	config.BlobURI = types.StringValue(response.BlobURI)
	config.WeightType = optionalString(response.WeightType)
	config.BaseModel = optionalString(response.BaseModel)
	config.Description = optionalString(response.Description)

	if len(response.Tags) == 0 {
		config.Tags = types.MapNull(types.StringType)
	} else {
		value, diags := types.MapValueFrom(ctx, types.StringType, response.Tags)
		resp.Diagnostics.Append(diags...)
		config.Tags = value
	}
	if len(response.Warnings) == 0 {
		config.Warnings = types.ListNull(types.StringType)
	} else {
		value, diags := types.ListValueFrom(ctx, types.StringType, response.Warnings)
		resp.Diagnostics.Append(diags...)
		config.Warnings = value
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
