package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

var (
	_ resource.Resource                = &datasetResource{}
	_ resource.ResourceWithConfigure   = &datasetResource{}
	_ resource.ResourceWithImportState = &datasetResource{}
)

func NewDatasetResource() resource.Resource {
	return &datasetResource{}
}

type datasetResource struct {
	client *clients.Client
}

type datasetModel struct {
	Name           types.String `tfsdk:"name"`
	Version        types.String `tfsdk:"version"`
	Type           types.String `tfsdk:"type"`
	ConnectionName types.String `tfsdk:"connection_name"`
	DataURI        types.String `tfsdk:"data_uri"`
	Description    types.String `tfsdk:"description"`
	Tags           types.Map    `tfsdk:"tags"`
	ID             types.String `tfsdk:"id"`
	DisplayName    types.String `tfsdk:"display_name"`
	IsSingleFile   types.Bool   `tfsdk:"is_single_file"`
	CreatedAt      types.String `tfsdk:"created_at"`
	LastModifiedAt types.String `tfsdk:"last_modified_at"`
}

// ConnectionName is required by the service but is undocumented in the public
// API reference; omitting it returns an opaque "Invalid request when
// registering the data asset..." error with no field-level detail.
type datasetRequest struct {
	Type           string            `json:"type"`
	ConnectionName string            `json:"connectionName"`
	DataURI        string            `json:"dataUri"`
	Description    string            `json:"description,omitempty"`
	Tags           map[string]string `json:"tags,omitempty"`
}

type datasetResponse struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Version        string            `json:"version"`
	DisplayName    string            `json:"displayName"`
	Description    string            `json:"description"`
	Tags           map[string]string `json:"tags"`
	Type           string            `json:"type"`
	DataURI        string            `json:"dataUri"`
	IsSingleFile   bool              `json:"isSingleFile"`
	ConnectionName string            `json:"connectionName"`
	SystemData     struct {
		CreatedAt      string `json:"createdAt"`
		LastModifiedAt string `json:"lastModifiedAt"`
	} `json:"systemData"`
}

func (m datasetModel) request(ctx context.Context, diagnostics *diag.Diagnostics) datasetRequest {
	request := datasetRequest{
		Type:           m.Type.ValueString(),
		ConnectionName: m.ConnectionName.ValueString(),
		DataURI:        m.DataURI.ValueString(),
		Description:    m.Description.ValueString(),
	}
	if !m.Tags.IsNull() {
		diagnostics.Append(m.Tags.ElementsAs(ctx, &request.Tags, false)...)
	}
	return request
}

func (m *datasetModel) apply(ctx context.Context, response datasetResponse, diagnostics *diag.Diagnostics) {
	m.ID = types.StringValue(response.ID)
	m.Name = types.StringValue(response.Name)
	m.Version = types.StringValue(response.Version)
	m.Type = types.StringValue(response.Type)
	m.ConnectionName = types.StringValue(response.ConnectionName)
	m.DataURI = types.StringValue(response.DataURI)
	m.Description = optionalString(response.Description)
	m.DisplayName = types.StringValue(response.DisplayName)
	m.IsSingleFile = types.BoolValue(response.IsSingleFile)
	m.CreatedAt = types.StringValue(response.SystemData.CreatedAt)
	m.LastModifiedAt = types.StringValue(response.SystemData.LastModifiedAt)

	if len(response.Tags) == 0 {
		m.Tags = types.MapNull(types.StringType)
	} else {
		value, diags := types.MapValueFrom(ctx, types.StringType, response.Tags)
		diagnostics.Append(diags...)
		m.Tags = value
	}
}

func (m datasetModel) path() string {
	return fmt.Sprintf("datasets/%s/versions/%s", m.Name.ValueString(), m.Version.ValueString())
}

func (r *datasetResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dataset"
}

func (r *datasetResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a dataset version, which registers a blob file or folder for use by Foundry resources.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the dataset. Changing this forces a new dataset to be created.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"version": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Caller-supplied version identifier for the dataset. Changing this forces a new dataset version to be created.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"type": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Dataset type. `uri_file` references a single blob file; `uri_folder` references a folder or prefix. Changing this forces a new dataset version to be created.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:          []validator.String{stringvalidator.OneOf("uri_file", "uri_folder")},
			},
			"connection_name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the Azure Storage connection backing the dataset.",
			},
			"data_uri": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Blob URI of the file or folder referenced by the dataset.",
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Description of the dataset version.",
			},
			"tags": schema.MapAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Key-value tags attached to the dataset version.",
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Service-assigned dataset asset identifier.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"display_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Display name assigned to the dataset version.",
			},
			"is_single_file": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the dataset references a single file.",
			},
			"created_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "RFC 3339 timestamp when the dataset version was created.",
			},
			"last_modified_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "RFC 3339 timestamp when the dataset version was last modified.",
			},
		},
	}
}

func (r *datasetResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (r *datasetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan datasetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var response datasetResponse
	if err := r.client.JSONWithContentType(ctx, http.MethodPatch, plan.path(), "application/merge-patch+json", plan.request(ctx, &resp.Diagnostics), &response); err != nil {
		resp.Diagnostics.AddError("Unable to create dataset", err.Error())
		return
	}

	plan.apply(ctx, response, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *datasetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state datasetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var response datasetResponse
	err := r.client.JSON(ctx, http.MethodGet, state.path(), nil, &response)
	if clients.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read dataset", err.Error())
		return
	}

	state.apply(ctx, response, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *datasetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan datasetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var response datasetResponse
	if err := r.client.JSONWithContentType(ctx, http.MethodPatch, plan.path(), "application/merge-patch+json", plan.request(ctx, &resp.Diagnostics), &response); err != nil {
		resp.Diagnostics.AddError("Unable to update dataset", err.Error())
		return
	}

	plan.apply(ctx, response, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *datasetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state datasetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.JSON(ctx, http.MethodDelete, state.path(), nil, nil)
	if err != nil && !clients.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete dataset", err.Error())
	}
}

func (r *datasetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	name, version, err := splitNameVersion(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Unable to import dataset", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("version"), version)...)
}

// splitNameVersion parses a compound name/version import ID into its parts.
func splitNameVersion(id string) (string, string, error) {
	name, version, found := strings.Cut(id, "/")
	if !found || name == "" || version == "" {
		return "", "", fmt.Errorf("expected import ID in the form name/version, got %q", id)
	}
	return name, version, nil
}
