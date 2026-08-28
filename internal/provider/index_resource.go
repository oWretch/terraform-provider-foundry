package provider

import (
	"context"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

var (
	_ resource.Resource                = &indexResource{}
	_ resource.ResourceWithConfigure   = &indexResource{}
	_ resource.ResourceWithImportState = &indexResource{}
)

func NewIndexResource() resource.Resource {
	return &indexResource{}
}

type indexResource struct {
	client *clients.Client
}

type indexModel struct {
	Name           types.String `tfsdk:"name"`
	Version        types.String `tfsdk:"version"`
	Type           types.String `tfsdk:"type"`
	ConnectionName types.String `tfsdk:"connection_name"`
	IndexName      types.String `tfsdk:"index_name"`
	ID             types.String `tfsdk:"id"`
}

type indexRequest struct {
	Type           string `json:"type"`
	ConnectionName string `json:"connectionName"`
	IndexName      string `json:"indexName"`
	Name           string `json:"name"`
	Version        string `json:"version"`
}

type indexResponse struct {
	Type           string `json:"type"`
	ConnectionName string `json:"connectionName"`
	IndexName      string `json:"indexName"`
	Name           string `json:"name"`
	Version        string `json:"version"`
}

func (m indexModel) request() indexRequest {
	return indexRequest{
		Type:           m.Type.ValueString(),
		ConnectionName: m.ConnectionName.ValueString(),
		IndexName:      m.IndexName.ValueString(),
		Name:           m.Name.ValueString(),
		Version:        m.Version.ValueString(),
	}
}

func (m *indexModel) apply(response indexResponse) {
	m.Type = types.StringValue(response.Type)
	m.ConnectionName = types.StringValue(response.ConnectionName)
	m.IndexName = types.StringValue(response.IndexName)
	m.Name = types.StringValue(response.Name)
	m.Version = types.StringValue(response.Version)
	// The service does not return an identifier for this resource, so the
	// Terraform ID is composed from the caller-supplied name and version.
	m.ID = types.StringValue(response.Name + ":" + response.Version)
}

func (m indexModel) path() string {
	return fmt.Sprintf("indexes/%s/versions/%s", m.Name.ValueString(), m.Version.ValueString())
}

func (r *indexResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_index"
}

func (r *indexResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages an index version backed by an Azure AI Search connection. Indexes backed by a `foundry_vector_store` appear automatically and are not managed by this resource.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the index. Changing this forces a new index to be created.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"version": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Caller-supplied version identifier for the index. Changing this forces a new index version to be created.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"type": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("AzureSearch"),
				MarkdownDescription: "Index type. Defaults to `AzureSearch` for a connection-backed Azure AI Search index.",
			},
			"connection_name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the Azure AI Search connection backing the index.",
			},
			"index_name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the underlying Azure AI Search index.",
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Identifier of the index version, in `name:version` form.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *indexResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (r *indexResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan indexModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var response indexResponse
	if err := r.client.JSONWithContentType(ctx, http.MethodPatch, plan.path(), "application/merge-patch+json", plan.request(), &response); err != nil {
		resp.Diagnostics.AddError("Unable to create index", err.Error())
		return
	}

	plan.apply(response)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *indexResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state indexModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var response indexResponse
	err := r.client.JSON(ctx, http.MethodGet, state.path(), nil, &response)
	if clients.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read index", err.Error())
		return
	}

	state.apply(response)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *indexResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan indexModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var response indexResponse
	if err := r.client.JSONWithContentType(ctx, http.MethodPatch, plan.path(), "application/merge-patch+json", plan.request(), &response); err != nil {
		resp.Diagnostics.AddError("Unable to update index", err.Error())
		return
	}

	plan.apply(response)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *indexResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state indexModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.JSON(ctx, http.MethodDelete, state.path(), nil, nil)
	if err != nil && !clients.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete index", err.Error())
	}
}

func (r *indexResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	name, version, err := splitNameVersion(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Unable to import index", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("version"), version)...)
}
