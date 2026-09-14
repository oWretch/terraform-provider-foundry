package provider

import (
	"context"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

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
	FieldMapping   types.Object `tfsdk:"field_mapping"`
	Description    types.String `tfsdk:"description"`
	Tags           types.Map    `tfsdk:"tags"`
	ID             types.String `tfsdk:"id"`
}

type indexRequest struct {
	Type           string             `json:"type"`
	ConnectionName string             `json:"connectionName"`
	IndexName      string             `json:"indexName"`
	FieldMapping   *indexFieldMapping `json:"fieldMapping,omitempty"`
	Description    string             `json:"description,omitempty"`
	Tags           map[string]string  `json:"tags,omitempty"`
}

type indexUpdateRequest struct {
	Type           string             `json:"type"`
	ConnectionName string             `json:"connectionName"`
	IndexName      string             `json:"indexName"`
	FieldMapping   *indexFieldMapping `json:"fieldMapping,omitempty"`
	Description    *string            `json:"description"`
	Tags           map[string]string  `json:"tags"`
}

type indexResponse struct {
	Type           string             `json:"type"`
	ConnectionName *string            `json:"connectionName"`
	IndexName      *string            `json:"indexName"`
	FieldMapping   *indexFieldMapping `json:"fieldMapping"`
	VectorStoreID  *string            `json:"vectorStoreId"`
	Name           string             `json:"name"`
	Version        string             `json:"version"`
	ID             string             `json:"id"`
	Description    *string            `json:"description"`
	Tags           *map[string]string `json:"tags"`
}

type indexFieldMapping struct {
	ContentFields  []string `json:"contentFields"`
	FilepathField  string   `json:"filepathField,omitempty"`
	TitleField     string   `json:"titleField,omitempty"`
	URLField       string   `json:"urlField,omitempty"`
	VectorFields   []string `json:"vectorFields,omitempty"`
	MetadataFields []string `json:"metadataFields,omitempty"`
}

type indexFieldMappingModel struct {
	ContentFields  types.List   `tfsdk:"content_fields"`
	FilepathField  types.String `tfsdk:"filepath_field"`
	TitleField     types.String `tfsdk:"title_field"`
	URLField       types.String `tfsdk:"url_field"`
	VectorFields   types.List   `tfsdk:"vector_fields"`
	MetadataFields types.List   `tfsdk:"metadata_fields"`
}

var indexFieldMappingAttributeTypes = map[string]attr.Type{
	"content_fields":  types.ListType{ElemType: types.StringType},
	"filepath_field":  types.StringType,
	"title_field":     types.StringType,
	"url_field":       types.StringType,
	"vector_fields":   types.ListType{ElemType: types.StringType},
	"metadata_fields": types.ListType{ElemType: types.StringType},
}

func (m indexModel) request(ctx context.Context, diagnostics *diag.Diagnostics) indexRequest {
	request := indexRequest{
		Type:           m.Type.ValueString(),
		ConnectionName: m.ConnectionName.ValueString(),
		IndexName:      m.IndexName.ValueString(),
		Description:    m.Description.ValueString(),
	}
	if !m.Tags.IsNull() {
		diagnostics.Append(m.Tags.ElementsAs(ctx, &request.Tags, false)...)
	}
	if !m.FieldMapping.IsNull() && !m.FieldMapping.IsUnknown() {
		var mapping indexFieldMappingModel
		diagnostics.Append(m.FieldMapping.As(ctx, &mapping, basetypes.ObjectAsOptions{})...)
		request.FieldMapping = mapping.request(ctx, diagnostics)
	}
	return request
}

func (m indexModel) updateRequest(ctx context.Context, diagnostics *diag.Diagnostics) indexUpdateRequest {
	request := indexUpdateRequest{
		Type:           m.Type.ValueString(),
		ConnectionName: m.ConnectionName.ValueString(),
		IndexName:      m.IndexName.ValueString(),
	}
	if !m.FieldMapping.IsNull() && !m.FieldMapping.IsUnknown() {
		var mapping indexFieldMappingModel
		diagnostics.Append(m.FieldMapping.As(ctx, &mapping, basetypes.ObjectAsOptions{})...)
		request.FieldMapping = mapping.request(ctx, diagnostics)
	}
	if !m.Description.IsNull() {
		value := m.Description.ValueString()
		request.Description = &value
	}
	if !m.Tags.IsNull() {
		diagnostics.Append(m.Tags.ElementsAs(ctx, &request.Tags, false)...)
	}
	return request
}

func (m indexModel) validateCreate(diagnostics *diag.Diagnostics) {
	if m.ConnectionName.IsNull() || m.ConnectionName.IsUnknown() || m.ConnectionName.ValueString() == "" {
		diagnostics.AddError("Missing Azure AI Search connection", "connection_name is required when creating an index version.")
	}
	if m.IndexName.IsNull() || m.IndexName.IsUnknown() || m.IndexName.ValueString() == "" {
		diagnostics.AddError("Missing Azure AI Search index", "index_name is required when creating an index version.")
	}
}

func (m indexFieldMappingModel) request(ctx context.Context, diagnostics *diag.Diagnostics) *indexFieldMapping {
	mapping := &indexFieldMapping{
		FilepathField: m.FilepathField.ValueString(),
		TitleField:    m.TitleField.ValueString(),
		URLField:      m.URLField.ValueString(),
	}
	diagnostics.Append(m.ContentFields.ElementsAs(ctx, &mapping.ContentFields, false)...)
	if !m.VectorFields.IsNull() {
		diagnostics.Append(m.VectorFields.ElementsAs(ctx, &mapping.VectorFields, false)...)
	}
	if !m.MetadataFields.IsNull() {
		diagnostics.Append(m.MetadataFields.ElementsAs(ctx, &mapping.MetadataFields, false)...)
	}
	return mapping
}

func (m *indexModel) apply(ctx context.Context, response indexResponse, diagnostics *diag.Diagnostics) {
	if response.Type != "AzureSearch" {
		diagnostics.AddError("Unsupported index type", fmt.Sprintf("Foundry returned index type %q; foundry_index only manages AzureSearch indexes because updating other kinds would discard their managed configuration.", response.Type))
		return
	}

	m.Type = types.StringValue(response.Type)
	m.Name = types.StringValue(response.Name)
	m.Version = types.StringValue(response.Version)
	if response.ID == "" {
		m.ID = types.StringValue(response.Name + ":" + response.Version)
	} else {
		m.ID = types.StringValue(response.ID)
	}
	if response.Description != nil {
		m.Description = applyOptionalAssetString(m.Description, *response.Description)
	}
	if response.Tags != nil {
		if len(*response.Tags) != 0 || !m.Tags.IsNull() {
			value, diags := types.MapValueFrom(ctx, types.StringType, *response.Tags)
			diagnostics.Append(diags...)
			m.Tags = value
		}
	}
	if response.ConnectionName != nil {
		m.ConnectionName = optionalString(*response.ConnectionName)
	}
	if response.IndexName != nil {
		m.IndexName = optionalString(*response.IndexName)
	}
	if response.FieldMapping != nil {
		m.FieldMapping = response.FieldMapping.value(ctx, diagnostics)
	} else if m.FieldMapping.IsUnknown() {
		m.FieldMapping = types.ObjectNull(indexFieldMappingAttributeTypes)
	}
}

func (m indexFieldMapping) value(ctx context.Context, diagnostics *diag.Diagnostics) types.Object {
	contentFields, diags := types.ListValueFrom(ctx, types.StringType, m.ContentFields)
	diagnostics.Append(diags...)
	vectorFields := types.ListNull(types.StringType)
	if m.VectorFields != nil {
		vectorFields, diags = types.ListValueFrom(ctx, types.StringType, m.VectorFields)
		diagnostics.Append(diags...)
	}
	metadataFields := types.ListNull(types.StringType)
	if m.MetadataFields != nil {
		metadataFields, diags = types.ListValueFrom(ctx, types.StringType, m.MetadataFields)
		diagnostics.Append(diags...)
	}
	value, diags := types.ObjectValue(indexFieldMappingAttributeTypes, map[string]attr.Value{
		"content_fields":  contentFields,
		"filepath_field":  optionalString(m.FilepathField),
		"title_field":     optionalString(m.TitleField),
		"url_field":       optionalString(m.URLField),
		"vector_fields":   vectorFields,
		"metadata_fields": metadataFields,
	})
	diagnostics.Append(diags...)
	return value
}

func (m indexModel) path() string {
	return assetVersionPath("indexes", m.Name.ValueString(), m.Version.ValueString())
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
				Validators:          []validator.String{stringvalidator.OneOf("AzureSearch")},
			},
			"connection_name": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Name of the Azure AI Search connection backing the index. Required when creating a version; optional only so an imported version can remain managed when the service omits this create-only field. Changing this forces a new index version.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"index_name": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Name of the underlying Azure AI Search index. Required when creating a version; optional only so an imported version can remain managed when the service omits this create-only field. Changing this forces a new index version.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"field_mapping": schema.SingleNestedAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Field mapping used by the Azure AI Search index. Changing this forces a new index version.",
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.RequiresReplace(),
					objectplanmodifier.UseStateForUnknown(),
				},
				Attributes: map[string]schema.Attribute{
					"content_fields": schema.ListAttribute{
						Required:            true,
						ElementType:         types.StringType,
						MarkdownDescription: "Fields containing text content.",
						Validators:          []validator.List{listvalidator.SizeAtLeast(1)},
					},
					"filepath_field": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "Field containing the source file path.",
						Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
					},
					"title_field": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "Field containing the document title.",
						Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
					},
					"url_field": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "Field containing the document URL.",
						Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
					},
					"vector_fields": schema.ListAttribute{
						Optional:            true,
						ElementType:         types.StringType,
						MarkdownDescription: "Fields containing vector content.",
						Validators:          []validator.List{listvalidator.SizeAtLeast(1)},
					},
					"metadata_fields": schema.ListAttribute{
						Optional:            true,
						ElementType:         types.StringType,
						MarkdownDescription: "Fields containing metadata.",
						Validators:          []validator.List{listvalidator.SizeAtLeast(1)},
					},
				},
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Description of the index version.",
			},
			"tags": schema.MapAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Key-value tags attached to the index version.",
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Service-assigned index asset identifier, or `name:version` when the service omits one.",
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
	plan.validateCreate(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	var response indexResponse
	request := plan.request(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.JSONWithContentType(ctx, http.MethodPatch, plan.path(), "application/merge-patch+json", request, &response); err != nil {
		resp.Diagnostics.AddError("Unable to create index", err.Error())
		return
	}

	plan.apply(ctx, response, &resp.Diagnostics)
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

	state.apply(ctx, response, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *indexResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan indexModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var response indexResponse
	request := plan.updateRequest(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.JSONWithContentType(ctx, http.MethodPatch, plan.path(), "application/merge-patch+json", request, &response); err != nil {
		resp.Diagnostics.AddError("Unable to update index", err.Error())
		return
	}

	plan.apply(ctx, response, &resp.Diagnostics)
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
