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
	_ datasource.DataSource              = &connectionDataSource{}
	_ datasource.DataSourceWithConfigure = &connectionDataSource{}
)

func NewConnectionDataSource() datasource.DataSource {
	return &connectionDataSource{}
}

// connectionDataSource reads a project connection. Connections are created
// through Azure Resource Manager rather than the Foundry data plane, which
// exposes them as read-only: the data plane offers list and get, but no
// create, update, or delete. This data source therefore has no matching
// resource; use the AzureRM provider to manage connections, and this to
// reference one from a tool's project_connection_id.
type connectionDataSource struct {
	client *clients.Client
}

type connectionDataSourceModel struct {
	Name           types.String `tfsdk:"name"`
	ID             types.String `tfsdk:"id"`
	Type           types.String `tfsdk:"type"`
	Target         types.String `tfsdk:"target"`
	IsDefault      types.Bool   `tfsdk:"is_default"`
	CredentialType types.String `tfsdk:"credential_type"`
	Metadata       types.Map    `tfsdk:"metadata"`
}

// singleConnectionResponse mirrors the Connection object. Note the camelCase
// isDefault key, which is inconsistent with the snake_case used across the
// rest of the Foundry API.
type singleConnectionResponse struct {
	Name      string            `json:"name"`
	ID        string            `json:"id"`
	Type      string            `json:"type"`
	Target    string            `json:"target"`
	IsDefault bool              `json:"isDefault"`
	Metadata  map[string]string `json:"metadata"`

	Credentials struct {
		Type string `json:"type"`
	} `json:"credentials"`
}

func (d *connectionDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_connection"
}

func (d *connectionDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a connection in the Foundry project. Connections are created and managed in Azure Resource Manager, " +
			"for example with the AzureRM provider, because the Foundry API exposes them as read-only. Use this data source to reference " +
			"an existing connection from a toolbox tool's `project_connection_id`.\n\n" +
			"Credentials are never returned by this data source.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the connection.",
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Identifier of the connection, used as `project_connection_id` on tools that reference it.",
			},
			"type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Category of the connection, such as `CognitiveSearch`, `AzureOpenAI`, `CustomKeys`, or `ApiKey`.",
			},
			"target": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "URL of the service this connection points to.",
			},
			"is_default": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether this connection is the project's default connection for its type.",
			},
			"credential_type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Kind of credential the connection uses, such as `ApiKey`, `AAD`, `SAS`, or `None`. The credential itself is not returned.",
			},
			"metadata": schema.MapAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Metadata recorded on the connection.",
			},
		},
	}
}

func (d *connectionDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (d *connectionDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config connectionDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var connection singleConnectionResponse
	err := d.client.JSON(ctx, http.MethodGet, "connections/"+config.Name.ValueString(), nil, &connection)
	if clients.IsNotFound(err) {
		resp.Diagnostics.AddError("Connection not found", "No connection named "+config.Name.ValueString()+" exists in this project.")
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read connection", err.Error())
		return
	}

	config.ID = types.StringValue(connection.ID)
	config.Type = optionalString(connection.Type)
	config.Target = optionalString(connection.Target)
	config.IsDefault = types.BoolValue(connection.IsDefault)
	config.CredentialType = optionalString(connection.Credentials.Type)
	config.Metadata = stringMapOrNull(connection.Metadata)

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
