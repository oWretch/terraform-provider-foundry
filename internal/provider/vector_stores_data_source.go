package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

var (
	_ datasource.DataSource              = &vectorStoresDataSource{}
	_ datasource.DataSourceWithConfigure = &vectorStoresDataSource{}
)

func NewVectorStoresDataSource() datasource.DataSource {
	return &vectorStoresDataSource{}
}

type vectorStoresDataSource struct {
	client *clients.Client
}

type vectorStoresDataSourceModel struct {
	Name         types.String           `tfsdk:"name"`
	VectorStores []vectorStoreListModel `tfsdk:"vector_stores"`
}

type vectorStoreListModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	Status       types.String `tfsdk:"status"`
	UsageBytes   types.Int64  `tfsdk:"usage_bytes"`
	CreatedAt    types.Int64  `tfsdk:"created_at"`
	FileCount    types.Int64  `tfsdk:"file_count"`
	LastActiveAt types.Int64  `tfsdk:"last_active_at"`
}

type vectorStoreListItem struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	UsageBytes int64  `json:"usage_bytes"`
	CreatedAt  int64  `json:"created_at"`
	FileCounts struct {
		Total int64 `json:"total"`
	} `json:"file_counts"`
	LastActiveAt int64 `json:"last_active_at"`
}

func (d *vectorStoresDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vector_stores"
}

func (d *vectorStoresDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the vector stores in the project. Vector stores are identified by a service-assigned ID rather than by name, so this data source is the way to find the ID of a vector store created outside this configuration. Vector store names are not unique, so `name` is a filter rather than a key and can match more than one store.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Return only vector stores with this exact name. Omit to return every vector store. Names are not unique, so this can still match more than one store.",
			},
			"vector_stores": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Vector stores available to the project.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":             schema.StringAttribute{Computed: true, MarkdownDescription: "Service-assigned vector store identifier, used by `foundry_vector_store_file` and by the `file_search` tool."},
						"name":           schema.StringAttribute{Computed: true, MarkdownDescription: "Vector store name. Not unique."},
						"status":         schema.StringAttribute{Computed: true, MarkdownDescription: "Vector store status, such as `completed` or `in_progress`."},
						"usage_bytes":    schema.Int64Attribute{Computed: true, MarkdownDescription: "Storage consumed by the vector store in bytes."},
						"created_at":     schema.Int64Attribute{Computed: true, MarkdownDescription: "Unix timestamp when the vector store was created."},
						"file_count":     schema.Int64Attribute{Computed: true, MarkdownDescription: "Total number of files attached to the vector store, in any state."},
						"last_active_at": schema.Int64Attribute{Computed: true, MarkdownDescription: "Unix timestamp when the vector store was last used."},
					},
				},
			},
		},
	}
}

func (d *vectorStoresDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (d *vectorStoresDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config vectorStoresDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	items, err := listAllPages[vectorStoreListItem](ctx, d.client, "openai/v1/vector_stores")
	if err != nil {
		resp.Diagnostics.AddError("Unable to list vector stores", err.Error())
		return
	}

	name := config.Name.ValueString()
	config.VectorStores = make([]vectorStoreListModel, 0, len(items))
	for _, item := range items {
		if name != "" && item.Name != name {
			continue
		}
		config.VectorStores = append(config.VectorStores, vectorStoreListModel{
			ID:           types.StringValue(item.ID),
			Name:         types.StringValue(item.Name),
			Status:       types.StringValue(item.Status),
			UsageBytes:   types.Int64Value(item.UsageBytes),
			CreatedAt:    types.Int64Value(item.CreatedAt),
			FileCount:    types.Int64Value(item.FileCounts.Total),
			LastActiveAt: types.Int64Value(item.LastActiveAt),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
