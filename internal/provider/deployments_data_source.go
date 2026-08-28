package provider

import (
	"context"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

var (
	_ datasource.DataSource              = &deploymentsDataSource{}
	_ datasource.DataSourceWithConfigure = &deploymentsDataSource{}
)

func NewDeploymentsDataSource() datasource.DataSource {
	return &deploymentsDataSource{}
}

type deploymentsDataSource struct {
	client *clients.Client
}

type deploymentsDataSourceModel struct {
	Deployments []deploymentModel `tfsdk:"deployments"`
}

type deploymentModel struct {
	Name           types.String `tfsdk:"name"`
	Type           types.String `tfsdk:"type"`
	ModelName      types.String `tfsdk:"model_name"`
	ModelVersion   types.String `tfsdk:"model_version"`
	ModelPublisher types.String `tfsdk:"model_publisher"`
	SKUName        types.String `tfsdk:"sku_name"`
	SKUCapacity    types.Int64  `tfsdk:"sku_capacity"`
}

type deploymentResponse struct {
	Value []struct {
		Name           string `json:"name"`
		Type           string `json:"type"`
		ModelName      string `json:"modelName"`
		ModelVersion   string `json:"modelVersion"`
		ModelPublisher string `json:"modelPublisher"`
		SKU            *struct {
			Name     string `json:"name"`
			Capacity int64  `json:"capacity"`
		} `json:"sku"`
	} `json:"value"`
}

func (d *deploymentsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_deployments"
}

func (d *deploymentsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the model deployments available to the configured Foundry project.",
		Attributes: map[string]schema.Attribute{
			"deployments": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Model deployments available to the project.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name":            schema.StringAttribute{Computed: true, MarkdownDescription: "Deployment name, used as the `model` value on an agent."},
						"type":            schema.StringAttribute{Computed: true, MarkdownDescription: "Deployment type."},
						"model_name":      schema.StringAttribute{Computed: true, MarkdownDescription: "Deployed model name."},
						"model_version":   schema.StringAttribute{Computed: true, MarkdownDescription: "Deployed model version."},
						"model_publisher": schema.StringAttribute{Computed: true, MarkdownDescription: "Publisher of the deployed model."},
						"sku_name":        schema.StringAttribute{Computed: true, MarkdownDescription: "SKU name of the deployment."},
						"sku_capacity":    schema.Int64Attribute{Computed: true, MarkdownDescription: "Provisioned SKU capacity."},
					},
				},
			},
		},
	}
}

func (d *deploymentsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*clients.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("Expected *clients.Client, got %T.", req.ProviderData))
		return
	}
	d.client = client
}

func (d *deploymentsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	var response deploymentResponse
	if err := d.client.JSON(ctx, http.MethodGet, "deployments", nil, &response); err != nil {
		resp.Diagnostics.AddError("Unable to list deployments", err.Error())
		return
	}

	state := deploymentsDataSourceModel{Deployments: make([]deploymentModel, 0, len(response.Value))}
	for _, item := range response.Value {
		deployment := deploymentModel{
			Name:           types.StringValue(item.Name),
			Type:           types.StringValue(item.Type),
			ModelName:      types.StringValue(item.ModelName),
			ModelVersion:   types.StringValue(item.ModelVersion),
			ModelPublisher: types.StringValue(item.ModelPublisher),
			SKUName:        types.StringNull(),
			SKUCapacity:    types.Int64Null(),
		}
		if item.SKU != nil {
			deployment.SKUName = types.StringValue(item.SKU.Name)
			deployment.SKUCapacity = types.Int64Value(item.SKU.Capacity)
		}
		state.Deployments = append(state.Deployments, deployment)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
