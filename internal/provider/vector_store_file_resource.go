package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

var (
	_ resource.Resource                = &vectorStoreFileResource{}
	_ resource.ResourceWithConfigure   = &vectorStoreFileResource{}
	_ resource.ResourceWithImportState = &vectorStoreFileResource{}
)

func NewVectorStoreFileResource() resource.Resource {
	return &vectorStoreFileResource{}
}

type vectorStoreFileResource struct {
	client *clients.Client
}

type vectorStoreFileModel struct {
	VectorStoreID types.String `tfsdk:"vector_store_id"`
	FileID        types.String `tfsdk:"file_id"`
	ID            types.String `tfsdk:"id"`
	Status        types.String `tfsdk:"status"`
	UsageBytes    types.Int64  `tfsdk:"usage_bytes"`
	CreatedAt     types.Int64  `tfsdk:"created_at"`
}

type vectorStoreFileResponse struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	UsageBytes int64  `json:"usage_bytes"`
	CreatedAt  int64  `json:"created_at"`
}

func (m *vectorStoreFileModel) apply(response vectorStoreFileResponse) {
	m.ID = types.StringValue(response.ID)
	m.Status = types.StringValue(response.Status)
	m.UsageBytes = types.Int64Value(response.UsageBytes)
	m.CreatedAt = types.Int64Value(response.CreatedAt)
}

func (m vectorStoreFileModel) path() string {
	return "openai/v1/vector_stores/" + m.VectorStoreID.ValueString() + "/files/" + m.FileID.ValueString()
}

func (r *vectorStoreFileResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vector_store_file"
}

func (r *vectorStoreFileResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	requiresReplace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Attaches an existing file to a vector store so its contents are indexed for retrieval.",
		Attributes: map[string]schema.Attribute{
			"vector_store_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Identifier of the vector store the file is attached to.",
				PlanModifiers:       requiresReplace,
			},
			"file_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Identifier of the file to attach.",
				PlanModifiers:       requiresReplace,
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Service-assigned identifier of the attachment, which equals the file identifier.",
			},
			"status": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Indexing status of the attached file.",
			},
			"usage_bytes": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Bytes the attached file contributes to the vector store.",
			},
			"created_at": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Unix timestamp when the file was attached.",
			},
		},
	}
}

func (r *vectorStoreFileResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (r *vectorStoreFileResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan vectorStoreFileModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := map[string]string{"file_id": plan.FileID.ValueString()}
	var response vectorStoreFileResponse
	if err := r.client.JSON(ctx, http.MethodPost, "openai/v1/vector_stores/"+plan.VectorStoreID.ValueString()+"/files", body, &response); err != nil {
		resp.Diagnostics.AddError("Unable to attach file to vector store", err.Error())
		return
	}

	plan.apply(response)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vectorStoreFileResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state vectorStoreFileModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var response vectorStoreFileResponse
	err := r.client.JSON(ctx, http.MethodGet, state.path(), nil, &response)
	if clients.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read vector store file", err.Error())
		return
	}

	state.apply(response)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update copies the plan to state because the attachment has no mutable fields;
// changing either identifier forces the resource to be replaced.
func (r *vectorStoreFileResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan vectorStoreFileModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vectorStoreFileResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state vectorStoreFileModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.JSON(ctx, http.MethodDelete, state.path(), nil, nil)
	if err != nil && !clients.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to detach file from vector store", err.Error())
	}
}

func (r *vectorStoreFileResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	vectorStoreID, fileID, err := splitVectorStoreFileID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Unable to import vector store file", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("vector_store_id"), vectorStoreID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("file_id"), fileID)...)
}

// splitVectorStoreFileID parses a compound import ID into its vector store and file parts.
func splitVectorStoreFileID(id string) (string, string, error) {
	vectorStoreID, fileID, found := strings.Cut(id, "/")
	if !found || vectorStoreID == "" || fileID == "" {
		return "", "", fmt.Errorf("expected import ID in the form vector_store_id/file_id, got %q", id)
	}
	return vectorStoreID, fileID, nil
}
