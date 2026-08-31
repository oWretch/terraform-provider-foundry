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
	_ datasource.DataSource              = &indexDataSource{}
	_ datasource.DataSourceWithConfigure = &indexDataSource{}
)

func NewIndexDataSource() datasource.DataSource {
	return &indexDataSource{}
}

type indexDataSource struct {
	client *clients.Client
}

func (d *indexDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_index"
}

func (d *indexDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up an Azure AI Search-backed index version by name. When `version` is omitted, the service's current latest version is returned. Other index kinds are rejected rather than misrepresented as Azure AI Search.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the index to look up.",
			},
			"version": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Version of the index. Omit to resolve the service's current latest version.",
			},
			"type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Index type. This data source currently accepts `AzureSearch`.",
			},
			"connection_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Azure AI Search connection name, when returned by the service.",
			},
			"index_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Underlying Azure AI Search index name, when returned by the service.",
			},
			"field_mapping": schema.SingleNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Azure AI Search field mapping, when returned by the service.",
				Attributes: map[string]schema.Attribute{
					"content_fields": schema.ListAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "Fields containing text content."},
					"filepath_field": schema.StringAttribute{Computed: true, MarkdownDescription: "Field containing the source file path."},
					"title_field":    schema.StringAttribute{Computed: true, MarkdownDescription: "Field containing the document title."},
					"url_field":      schema.StringAttribute{Computed: true, MarkdownDescription: "Field containing the document URL."},
					"vector_fields": schema.ListAttribute{
						Computed:            true,
						ElementType:         types.StringType,
						MarkdownDescription: "Fields containing vector content.",
					},
					"metadata_fields": schema.ListAttribute{
						Computed:            true,
						ElementType:         types.StringType,
						MarkdownDescription: "Fields containing metadata.",
					},
				},
			},
			"description": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Description returned for the index version, when present.",
			},
			"tags": schema.MapAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Tags returned for the index version, when present.",
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Service-assigned index asset identifier, or `name:version` when the service omits one.",
			},
		},
	}
}

func (d *indexDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (d *indexDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config indexModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var response indexResponse
	if config.Version.IsNull() {
		latest, found, err := readLatestAsset(ctx, d.client, "indexes", config.Name.ValueString(), func(item indexResponse) string {
			return item.Name
		})
		if err != nil {
			resp.Diagnostics.AddError("Unable to read index", err.Error())
			return
		}
		if !found {
			resp.Diagnostics.AddError("Index not found", "No current index matched the given name.")
			return
		}
		response = latest
	} else {
		err := d.client.JSON(ctx, http.MethodGet, assetVersionPath("indexes", config.Name.ValueString(), config.Version.ValueString()), nil, &response)
		if clients.IsNotFound(err) {
			resp.Diagnostics.AddError("Index not found", "No index matched the given name and version.")
			return
		}
		if err != nil {
			resp.Diagnostics.AddError("Unable to read index", err.Error())
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
