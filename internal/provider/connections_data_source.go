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
	_ datasource.DataSource              = &connectionsDataSource{}
	_ datasource.DataSourceWithConfigure = &connectionsDataSource{}
)

func NewConnectionsDataSource() datasource.DataSource {
	return &connectionsDataSource{}
}

type connectionsDataSource struct {
	client *clients.Client
}

type connectionsDataSourceModel struct {
	Connections []connectionModel `tfsdk:"connections"`
}

type connectionModel struct {
	Name      types.String `tfsdk:"name"`
	ID        types.String `tfsdk:"id"`
	Type      types.String `tfsdk:"type"`
	Target    types.String `tfsdk:"target"`
	IsDefault types.Bool   `tfsdk:"is_default"`
}

type connectionResponse struct {
	Value []struct {
		Name      string `json:"name"`
		ID        string `json:"id"`
		Type      string `json:"type"`
		Target    string `json:"target"`
		IsDefault bool   `json:"isDefault"`
	} `json:"value"`
}

func (d *connectionsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_connections"
}

func (d *connectionsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the connections configured on the Foundry account and project. Connections are created and managed outside this provider, typically with `azurerm_ai_foundry` or `azurerm_ai_services_connection`. Use this data source to look up a connection name for the `connection_name` attribute of `foundry_dataset` and `foundry_index`.",
		Attributes: map[string]schema.Attribute{
			"connections": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Connections available to the project.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name":       schema.StringAttribute{Computed: true, MarkdownDescription: "Connection name, used as `connection_name` on datasets and indexes."},
						"id":         schema.StringAttribute{Computed: true, MarkdownDescription: "Azure Resource Manager ID of the connection."},
						"type":       schema.StringAttribute{Computed: true, MarkdownDescription: "Connection type, such as `AzureStorageAccount` or `CognitiveSearch`."},
						"target":     schema.StringAttribute{Computed: true, MarkdownDescription: "Endpoint or resource ID the connection points to."},
						"is_default": schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether this is the default connection of its type for the project."},
					},
				},
			},
		},
	}
}

func (d *connectionsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (d *connectionsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	var response connectionResponse
	if err := d.client.JSON(ctx, http.MethodGet, "connections", nil, &response); err != nil {
		resp.Diagnostics.AddError("Unable to list connections", err.Error())
		return
	}

	state := connectionsDataSourceModel{Connections: make([]connectionModel, 0, len(response.Value))}
	for _, item := range response.Value {
		state.Connections = append(state.Connections, connectionModel{
			Name:      types.StringValue(item.Name),
			ID:        types.StringValue(item.ID),
			Type:      types.StringValue(item.Type),
			Target:    types.StringValue(item.Target),
			IsDefault: types.BoolValue(item.IsDefault),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
