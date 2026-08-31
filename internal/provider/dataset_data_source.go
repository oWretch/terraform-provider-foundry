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
	_ datasource.DataSource              = &datasetDataSource{}
	_ datasource.DataSourceWithConfigure = &datasetDataSource{}
)

func NewDatasetDataSource() datasource.DataSource {
	return &datasetDataSource{}
}

type datasetDataSource struct {
	client *clients.Client
}

func (d *datasetDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dataset"
}

func (d *datasetDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a dataset version by name. When `version` is omitted, the service's current latest version is returned.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the dataset to look up.",
			},
			"version": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Version of the dataset. Omit to resolve the service's current latest version.",
			},
			"type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Dataset type, `uri_file` or `uri_folder`.",
			},
			"connection_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Azure Storage connection associated with the dataset, when present.",
			},
			"data_uri": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "URI of the dataset file or folder.",
			},
			"description": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Description returned for the dataset version, when present.",
			},
			"tags": schema.MapAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Tags returned for the dataset version, when present.",
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Service-assigned dataset asset identifier.",
			},
			"is_reference": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the dataset references external storage.",
			},
		},
	}
}

func (d *datasetDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (d *datasetDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config datasetModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var response datasetResponse
	if config.Version.IsNull() {
		latest, found, err := readLatestAsset(ctx, d.client, "datasets", config.Name.ValueString(), func(item datasetResponse) string {
			return item.Name
		})
		if err != nil {
			resp.Diagnostics.AddError("Unable to read dataset", err.Error())
			return
		}
		if !found {
			resp.Diagnostics.AddError("Dataset not found", "No current dataset matched the given name.")
			return
		}
		response = latest
	} else {
		err := d.client.JSON(ctx, http.MethodGet, assetVersionPath("datasets", config.Name.ValueString(), config.Version.ValueString()), nil, &response)
		if clients.IsNotFound(err) {
			resp.Diagnostics.AddError("Dataset not found", "No dataset matched the given name and version.")
			return
		}
		if err != nil {
			resp.Diagnostics.AddError("Unable to read dataset", err.Error())
			return
		}
	}

	config.apply(ctx, response, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if response.Description == nil {
		config.Description = types.StringNull()
	} else {
		config.Description = optionalString(*response.Description)
	}
	if response.Tags == nil || len(*response.Tags) == 0 {
		config.Tags = types.MapNull(types.StringType)
	} else {
		value, diags := types.MapValueFrom(ctx, types.StringType, *response.Tags)
		resp.Diagnostics.Append(diags...)
		config.Tags = value
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
