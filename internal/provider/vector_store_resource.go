package provider

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

var (
	_ resource.Resource                = &vectorStoreResource{}
	_ resource.ResourceWithConfigure   = &vectorStoreResource{}
	_ resource.ResourceWithImportState = &vectorStoreResource{}
)

func NewVectorStoreResource() resource.Resource {
	return &vectorStoreResource{}
}

type vectorStoreResource struct {
	client *clients.Client
}

type vectorStoreModel struct {
	Name             types.String `tfsdk:"name"`
	Metadata         types.Map    `tfsdk:"metadata"`
	ExpiresAfterDays types.Int64  `tfsdk:"expires_after_days"`
	ID               types.String `tfsdk:"id"`
	Status           types.String `tfsdk:"status"`
	UsageBytes       types.Int64  `tfsdk:"usage_bytes"`
	CreatedAt        types.Int64  `tfsdk:"created_at"`
	FileCount        types.Int64  `tfsdk:"file_count"`
}

type vectorStoreExpiry struct {
	Anchor string `json:"anchor"`
	Days   int64  `json:"days"`
}

type vectorStoreRequest struct {
	Name         string             `json:"name,omitempty"`
	Metadata     map[string]string  `json:"metadata,omitempty"`
	ExpiresAfter *vectorStoreExpiry `json:"expires_after,omitempty"`
}

type vectorStoreResponse struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Status     string            `json:"status"`
	UsageBytes int64             `json:"usage_bytes"`
	CreatedAt  int64             `json:"created_at"`
	Metadata   map[string]string `json:"metadata"`
	FileCounts struct {
		Total int64 `json:"total"`
	} `json:"file_counts"`
}

func (m vectorStoreModel) request(ctx context.Context, diagnostics *diag.Diagnostics) vectorStoreRequest {
	request := vectorStoreRequest{Name: m.Name.ValueString()}
	if !m.Metadata.IsNull() {
		diagnostics.Append(m.Metadata.ElementsAs(ctx, &request.Metadata, false)...)
	}
	if !m.ExpiresAfterDays.IsNull() {
		request.ExpiresAfter = &vectorStoreExpiry{Anchor: "last_active_at", Days: m.ExpiresAfterDays.ValueInt64()}
	}
	return request
}

func (m *vectorStoreModel) apply(ctx context.Context, response vectorStoreResponse, diagnostics *diag.Diagnostics) {
	m.ID = types.StringValue(response.ID)
	m.Name = optionalString(response.Name)
	m.Status = types.StringValue(response.Status)
	m.UsageBytes = types.Int64Value(response.UsageBytes)
	m.CreatedAt = types.Int64Value(response.CreatedAt)
	m.FileCount = types.Int64Value(response.FileCounts.Total)

	if len(response.Metadata) == 0 {
		m.Metadata = types.MapNull(types.StringType)
	} else {
		value, diags := types.MapValueFrom(ctx, types.StringType, response.Metadata)
		diagnostics.Append(diags...)
		m.Metadata = value
	}
}

func (r *vectorStoreResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vector_store"
}

func (r *vectorStoreResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a vector store, which indexes attached files for retrieval by agents.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Name of the vector store. Changing this forces a new vector store to be created.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"metadata": schema.MapAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Key-value metadata attached to the vector store. Changing this forces a new vector store to be created.",
				PlanModifiers:       []planmodifier.Map{mapplanmodifier.RequiresReplace()},
			},
			"expires_after_days": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Number of days after the store is last active before it expires. Leave unset to keep the store indefinitely. Changing this forces a new vector store to be created.",
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.RequiresReplace()},
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Service-assigned vector store identifier.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"status": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Processing status of the vector store.",
			},
			"usage_bytes": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Total bytes used by the files in the vector store.",
			},
			"created_at": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Unix timestamp when the vector store was created.",
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"file_count": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Number of files attached to the vector store.",
			},
		},
	}
}

func (r *vectorStoreResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (r *vectorStoreResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan vectorStoreModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var response vectorStoreResponse
	if err := r.client.JSON(ctx, http.MethodPost, "openai/v1/vector_stores", plan.request(ctx, &resp.Diagnostics), &response); err != nil {
		resp.Diagnostics.AddError("Unable to create vector store", err.Error())
		return
	}

	plan.apply(ctx, response, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vectorStoreResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state vectorStoreModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var response vectorStoreResponse
	err := r.client.JSON(ctx, http.MethodGet, "openai/v1/vector_stores/"+state.ID.ValueString(), nil, &response)
	if clients.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read vector store", err.Error())
		return
	}

	state.apply(ctx, response, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update never changes the service, because the vector store update endpoint
// accepts a new name, metadata, or expiry and returns them without persisting
// them. Every configurable attribute therefore forces replacement instead.
func (r *vectorStoreResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan vectorStoreModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vectorStoreResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state vectorStoreModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.JSON(ctx, http.MethodDelete, "openai/v1/vector_stores/"+state.ID.ValueString(), nil, nil)
	if err != nil && !clients.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete vector store", err.Error())
	}
}

func (r *vectorStoreResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
