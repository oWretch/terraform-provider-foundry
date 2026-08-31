package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

var (
	_ resource.Resource                = &fileResource{}
	_ resource.ResourceWithConfigure   = &fileResource{}
	_ resource.ResourceWithImportState = &fileResource{}
)

func NewFileResource() resource.Resource {
	return &fileResource{}
}

type fileResource struct {
	client *clients.Client
}

type fileModel struct {
	SourcePath types.String `tfsdk:"source_path"`
	Purpose    types.String `tfsdk:"purpose"`
	SourceHash types.String `tfsdk:"source_hash"`
	ID         types.String `tfsdk:"id"`
	Filename   types.String `tfsdk:"filename"`
	Bytes      types.Int64  `tfsdk:"bytes"`
	CreatedAt  types.Int64  `tfsdk:"created_at"`
	Status     types.String `tfsdk:"status"`
}

type fileResponse struct {
	ID        string `json:"id"`
	Purpose   string `json:"purpose"`
	Filename  string `json:"filename"`
	Bytes     int64  `json:"bytes"`
	CreatedAt int64  `json:"created_at"`
	Status    string `json:"status"`
}

func (m *fileModel) apply(response fileResponse) {
	m.ID = types.StringValue(response.ID)
	m.Purpose = types.StringValue(response.Purpose)
	m.Filename = types.StringValue(response.Filename)
	m.Bytes = types.Int64Value(response.Bytes)
	m.CreatedAt = types.Int64Value(response.CreatedAt)
	m.Status = types.StringValue(response.Status)
}

func (r *fileResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_file"
}

func (r *fileResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	requiresReplace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Uploads a file for use by other Foundry resources. Files are immutable, so any change forces a new file to be uploaded.\n\n" +
			"Use this resource when the file is part of what Terraform manages, such as a document attached to a vector store an agent then searches. If the file is instead loaded once, or published by a separate data pipeline, upload it there and use the `foundry_files` data source to look up its ID: files are identified by a service-assigned ID rather than by name, so a lookup is the only way to reference a file this configuration did not create.\n\n" +
			"The file contents are read and uploaded during `terraform apply`, so apply time grows with file size.",
		Attributes: map[string]schema.Attribute{
			"source_path": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Local path of the file to upload. Required when uploading a file; it is optional only so that an imported file, whose local path the service cannot report, does not plan a replacement.",
				PlanModifiers:       requiresReplace,
			},
			"purpose": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Intended purpose of the file, such as `assistants`, `batch`, `fine-tune`, `evals`, `vision`, or `user_data`. The accepted set varies by service version. Required when uploading a file; the service reports it back, so an imported file does not need it in the configuration.",
				PlanModifiers:       requiresReplace,
			},
			"source_hash": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Hash of the file contents, such as `filesha256(\"path\")`. Supply this to force a re-upload when the file contents change.",
				PlanModifiers:       requiresReplace,
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Service-assigned file identifier.",
			},
			"filename": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Name the service recorded for the uploaded file.",
			},
			"bytes": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Size of the uploaded file in bytes.",
			},
			"created_at": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Unix timestamp when the file was uploaded.",
			},
			"status": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Processing status of the uploaded file.",
			},
		},
	}
}

func (r *fileResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (r *fileResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan fileModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if plan.SourcePath.IsNull() || plan.SourcePath.ValueString() == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("source_path"),
			"Missing file contents",
			"source_path is required when uploading a file. It is optional in the schema only so that importing an existing file, whose local path the service cannot report, does not plan a replacement.",
		)
	}
	if plan.Purpose.IsNull() || plan.Purpose.ValueString() == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("purpose"),
			"Missing file purpose",
			"purpose is required when uploading a file. It is optional in the schema only because the service reports it back, so an imported file does not need it in the configuration.",
		)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	sourcePath := plan.SourcePath.ValueString()
	contents, err := os.ReadFile(sourcePath)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read source file", err.Error())
		return
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("purpose", plan.Purpose.ValueString()); err != nil {
		resp.Diagnostics.AddError("Unable to build upload request", err.Error())
		return
	}
	part, err := writer.CreateFormFile("file", filepath.Base(sourcePath))
	if err != nil {
		resp.Diagnostics.AddError("Unable to build upload request", err.Error())
		return
	}
	if _, err := part.Write(contents); err != nil {
		resp.Diagnostics.AddError("Unable to build upload request", err.Error())
		return
	}
	if err := writer.Close(); err != nil {
		resp.Diagnostics.AddError("Unable to build upload request", err.Error())
		return
	}

	request, err := r.client.NewRequest(ctx, http.MethodPost, "openai/v1/files", &body)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create file", err.Error())
		return
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())

	response, err := r.client.Do(request)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create file", err.Error())
		return
	}
	defer func() { _ = response.Body.Close() }()

	var decoded fileResponse
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		resp.Diagnostics.AddError("Unable to decode file response", err.Error())
		return
	}

	plan.apply(decoded)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *fileResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state fileModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var response fileResponse
	err := r.client.JSON(ctx, http.MethodGet, "openai/v1/files/"+state.ID.ValueString(), nil, &response)
	if clients.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read file", err.Error())
		return
	}

	state.apply(response)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update copies the plan to state because the service has no file update
// endpoint, so every configurable change forces the resource to be replaced.
func (r *fileResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan fileModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *fileResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state fileModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.JSON(ctx, http.MethodDelete, "openai/v1/files/"+state.ID.ValueString(), nil, nil)
	if err != nil && !clients.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete file", err.Error())
	}
}

func (r *fileResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
