package provider

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

var (
	_ resource.Resource                = &modelVersionResource{}
	_ resource.ResourceWithConfigure   = &modelVersionResource{}
	_ resource.ResourceWithImportState = &modelVersionResource{}
)

func NewModelVersionResource() resource.Resource {
	return &modelVersionResource{}
}

// modelVersionResource registers a custom model artifact as a project model
// asset.
//
// The artifact cannot be referenced from arbitrary storage. The service resolves
// blobUri through the model registry's asset store, which only issues a
// container SAS for storage it manages, so a URI pointing at the practitioner's
// own account is rejected with "Invalid containerUri" even when the account is
// registered as a project connection and the workspace identity can read it.
// The artifact is therefore uploaded to a service-issued temporary container
// first, which is what startPendingUpload exists to provide. This is the
// opposite of foundry_dataset, which does reference practitioner storage
// directly through connectionName.
type modelVersionResource struct {
	client *clients.Client
}

type modelVersionModel struct {
	Name        types.String `tfsdk:"name"`
	Version     types.String `tfsdk:"version"`
	SourcePath  types.String `tfsdk:"source_path"`
	SourceHash  types.String `tfsdk:"source_hash"`
	WeightType  types.String `tfsdk:"weight_type"`
	BaseModel   types.String `tfsdk:"base_model"`
	LoraConfig  types.Object `tfsdk:"lora_config"`
	Description types.String `tfsdk:"description"`
	Tags        types.Map    `tfsdk:"tags"`
	ID          types.String `tfsdk:"id"`
	BlobURI     types.String `tfsdk:"blob_uri"`
	Warnings    types.List   `tfsdk:"warnings"`
}

type loraConfigRequest struct {
	Rank          *int64   `json:"rank,omitempty"`
	Alpha         *int64   `json:"alpha,omitempty"`
	TargetModules []string `json:"targetModules,omitempty"`
	Dropout       *float64 `json:"dropout,omitempty"`
}

type modelVersionRequest struct {
	BlobURI     string             `json:"blobUri"`
	WeightType  string             `json:"weightType,omitempty"`
	BaseModel   string             `json:"baseModel,omitempty"`
	LoraConfig  *loraConfigRequest `json:"loraConfig,omitempty"`
	Description string             `json:"description,omitempty"`
	Tags        map[string]string  `json:"tags,omitempty"`
}

// modelVersionUpdateRequest carries the only two fields the service accepts on
// a merge patch. Everything else about a version is immutable, so the schema
// forces replacement instead of sending a request the service would ignore.
type modelVersionUpdateRequest struct {
	Description string            `json:"description,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
}

type modelVersionResponse struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Version     string             `json:"version"`
	BlobURI     string             `json:"blobUri"`
	WeightType  string             `json:"weightType"`
	BaseModel   string             `json:"baseModel"`
	LoraConfig  *loraConfigRequest `json:"loraConfig"`
	Description string             `json:"description"`
	Tags        map[string]string  `json:"tags"`
	Warnings    []string           `json:"warnings"`
}

type pendingUploadRequest struct {
	PendingUploadType string `json:"pendingUploadType"`
}

type pendingUploadResponse struct {
	PendingUploadID string `json:"pendingUploadId"`
	BlobReference   struct {
		BlobURI    string `json:"blobUri"`
		Credential struct {
			SasURI string `json:"sasUri"`
		} `json:"credential"`
	} `json:"blobReference"`
}

type operationLocation struct {
	Location string `json:"location"`
}

func (r *modelVersionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_model_version"
}

func (r *modelVersionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	requiresReplace := []planmodifier.String{stringplanmodifier.RequiresReplace()}

	resp.Schema = schema.Schema{
		MarkdownDescription: "Registers a custom model artifact as a project model asset, so that it can be deployed or referenced by other Foundry resources.\n\n" +
			"The artifact is uploaded from a local path into storage the service issues for the version. Referencing an existing blob in your own storage account is not supported by the service: the model registry only resolves artifacts held in storage it manages. Use `foundry_dataset` when you need to reference your own storage instead.\n\n" +
			"Only `description` and `tags` can be changed in place. Changing the artifact or any of its metadata publishes a new version.\n\n" +
			"~> **Consider whether Terraform is the right tool for publishing the artifact.** A model artifact is a build output. It is produced by a training or fine-tuning pipeline, is identified by its contents, and is promoted rather than converged. Terraform's model is to make reality match a declaration, which fits deployment configuration better than it fits shipping a large binary. Publishing from the pipeline that produced the artifact, and using the `foundry_model_version` data source to reference the published version from Terraform, keeps the upload out of `terraform apply`.\n\n" +
			"That said, this resource is a reasonable way to publish a model from a CI or CD job when you would rather keep one tool in the pipeline, particularly for small artifacts and for environments rebuilt from scratch. Be aware that the whole artifact is uploaded during `terraform apply`, so apply time grows with artifact size, and that a change to the artifact replaces the version rather than updating it.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the model. Changing this forces a new model version to be created.",
				PlanModifiers:       requiresReplace,
			},
			"version": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Caller-supplied version identifier. Changing this forces a new model version to be created.",
				PlanModifiers:       requiresReplace,
			},
			"source_path": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Local path of the model artifact to upload. A directory is uploaded in full, preserving its layout, which is what a model with separate weight, tokenizer, and configuration files needs. Required when creating a version; it is optional only so that an imported version, whose local path the service cannot report, does not plan a replacement. Changing this forces a new model version to be created.",
				PlanModifiers:       requiresReplace,
			},
			"source_hash": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Hash of the artifact contents, such as `filesha256(\"model.safetensors\")`. Supply this to force a new version when the artifact changes but its path does not. Changing this forces a new model version to be created.",
				PlanModifiers:       requiresReplace,
			},
			"weight_type": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "How the artifact's weights are stored: `FullWeight`, `LoRA`, or `DraftModel`. Changing this forces a new model version to be created.",
				PlanModifiers:       requiresReplace,
				Validators:          []validator.String{stringvalidator.OneOf("FullWeight", "LoRA", "DraftModel")},
			},
			"base_model": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Asset ID of the base model this artifact adapts. Changing this forces a new model version to be created.",
				PlanModifiers:       requiresReplace,
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Description of the model version.",
			},
			"tags": schema.MapAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Key-value tags attached to the model version.",
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Service-assigned model asset identifier.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"blob_uri": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "URI of the uploaded artifact in the storage the service issued for this version.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"warnings": schema.ListAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Advisory warnings the service derived from inspecting the artifact, such as a missing configuration file.",
			},
		},
		Blocks: map[string]schema.Block{
			"lora_config": schema.SingleNestedBlock{
				MarkdownDescription: "Adapter configuration, which the serving engine requires when `weight_type` is `LoRA` and ignores otherwise. Changing any value forces a new model version to be created.",
				PlanModifiers:       []planmodifier.Object{objectplanmodifier.RequiresReplace()},
				Attributes: map[string]schema.Attribute{
					"rank": schema.Int64Attribute{
						Optional:            true,
						MarkdownDescription: "LoRA rank (`r`), commonly 8, 16, 32, or 64.",
					},
					"alpha": schema.Int64Attribute{
						Optional:            true,
						MarkdownDescription: "LoRA scaling factor, typically twice the rank.",
					},
					"target_modules": schema.ListAttribute{
						Optional:            true,
						ElementType:         types.StringType,
						MarkdownDescription: "Model layers the adapter modifies, such as `q_proj` and `v_proj`.",
					},
					"dropout": schema.Float64Attribute{
						Optional:            true,
						MarkdownDescription: "Dropout rate used during training. Recorded for reference; the serving engine does not apply it.",
					},
				},
			},
		},
	}
}

func (r *modelVersionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (m modelVersionModel) path() string {
	return fmt.Sprintf("models/%s/versions/%s", m.Name.ValueString(), m.Version.ValueString())
}

func (m modelVersionModel) request(ctx context.Context, blobURI string, diagnostics *diag.Diagnostics) modelVersionRequest {
	request := modelVersionRequest{
		BlobURI:     blobURI,
		WeightType:  m.WeightType.ValueString(),
		BaseModel:   m.BaseModel.ValueString(),
		Description: m.Description.ValueString(),
	}
	if !m.Tags.IsNull() && !m.Tags.IsUnknown() {
		diagnostics.Append(m.Tags.ElementsAs(ctx, &request.Tags, false)...)
	}
	if !m.LoraConfig.IsNull() && !m.LoraConfig.IsUnknown() {
		var config struct {
			Rank          types.Int64   `tfsdk:"rank"`
			Alpha         types.Int64   `tfsdk:"alpha"`
			TargetModules types.List    `tfsdk:"target_modules"`
			Dropout       types.Float64 `tfsdk:"dropout"`
		}
		diagnostics.Append(m.LoraConfig.As(ctx, &config, basetypes.ObjectAsOptions{})...)

		lora := &loraConfigRequest{}
		if !config.Rank.IsNull() {
			value := config.Rank.ValueInt64()
			lora.Rank = &value
		}
		if !config.Alpha.IsNull() {
			value := config.Alpha.ValueInt64()
			lora.Alpha = &value
		}
		if !config.Dropout.IsNull() {
			value := config.Dropout.ValueFloat64()
			lora.Dropout = &value
		}
		if !config.TargetModules.IsNull() && !config.TargetModules.IsUnknown() {
			diagnostics.Append(config.TargetModules.ElementsAs(ctx, &lora.TargetModules, false)...)
		}
		request.LoraConfig = lora
	}
	return request
}

func (m *modelVersionModel) apply(ctx context.Context, response modelVersionResponse, diagnostics *diag.Diagnostics) {
	m.ID = types.StringValue(response.ID)
	m.Name = types.StringValue(response.Name)
	m.Version = types.StringValue(response.Version)
	m.BlobURI = types.StringValue(response.BlobURI)
	m.WeightType = optionalString(response.WeightType)
	m.BaseModel = optionalString(response.BaseModel)
	m.Description = optionalString(response.Description)

	if len(response.Tags) == 0 {
		m.Tags = types.MapNull(types.StringType)
	} else {
		value, diags := types.MapValueFrom(ctx, types.StringType, response.Tags)
		diagnostics.Append(diags...)
		m.Tags = value
	}

	if len(response.Warnings) == 0 {
		m.Warnings = types.ListNull(types.StringType)
	} else {
		value, diags := types.ListValueFrom(ctx, types.StringType, response.Warnings)
		diagnostics.Append(diags...)
		m.Warnings = value
	}
}

func (r *modelVersionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan modelVersionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if plan.SourcePath.IsNull() || plan.SourcePath.ValueString() == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("source_path"),
			"Missing model artifact",
			"source_path is required when creating a model version. It is optional in the schema only so that importing an existing version, whose local path the service cannot report, does not plan a replacement.",
		)
		return
	}

	var upload pendingUploadResponse
	if err := r.client.JSON(ctx, http.MethodPost, plan.path()+"/startPendingUpload",
		pendingUploadRequest{PendingUploadType: "TemporaryBlobReference"}, &upload); err != nil {
		resp.Diagnostics.AddError("Unable to start the model artifact upload", err.Error())
		return
	}

	if err := uploadArtifact(ctx, upload.BlobReference.Credential.SasURI, plan.SourcePath.ValueString()); err != nil {
		resp.Diagnostics.AddError("Unable to upload the model artifact", err.Error())
		return
	}

	request := plan.request(ctx, upload.BlobReference.BlobURI, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	var location operationLocation
	if err := r.client.JSON(ctx, http.MethodPost, plan.path()+"/createAsync", request, &location); err != nil {
		resp.Diagnostics.AddError("Unable to create the model version", err.Error())
		return
	}

	var response modelVersionResponse
	if err := r.client.WaitForOperation(ctx, location.Location, &response); err != nil {
		resp.Diagnostics.AddError("Model version was not registered", err.Error())
		return
	}

	plan.apply(ctx, response, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *modelVersionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state modelVersionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var response modelVersionResponse
	err := r.client.JSON(ctx, http.MethodGet, state.path(), nil, &response)
	if clients.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read the model version", err.Error())
		return
	}

	state.apply(ctx, response, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *modelVersionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan modelVersionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	request := modelVersionUpdateRequest{Description: plan.Description.ValueString()}
	if !plan.Tags.IsNull() && !plan.Tags.IsUnknown() {
		resp.Diagnostics.Append(plan.Tags.ElementsAs(ctx, &request.Tags, false)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	var response modelVersionResponse
	if err := r.client.JSONWithContentType(ctx, http.MethodPatch, plan.path(),
		"application/merge-patch+json", request, &response); err != nil {
		resp.Diagnostics.AddError("Unable to update the model version", err.Error())
		return
	}

	plan.apply(ctx, response, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *modelVersionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state modelVersionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.JSON(ctx, http.MethodDelete, state.path(), nil, nil)
	if err != nil && !clients.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete the model version", err.Error())
	}
}

func (r *modelVersionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	name, version, found := strings.Cut(req.ID, "/")
	if !found || name == "" || version == "" {
		resp.Diagnostics.AddError(
			"Unexpected import identifier",
			fmt.Sprintf("Expected an identifier of the form \"name/version\", got %q.", req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("version"), version)...)
}

// uploadArtifact copies the local artifact into the container the service
// issued for this version. The SAS grants container-level write, so each file
// is written under the container with its path relative to source_path
// preserved, which keeps a multi-file model's layout intact.
func uploadArtifact(ctx context.Context, containerSAS, sourcePath string) error {
	base, query, found := strings.Cut(containerSAS, "?")
	if !found {
		return fmt.Errorf("upload location did not include a shared access signature")
	}

	info, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", sourcePath, err)
	}

	if !info.IsDir() {
		return uploadFile(ctx, base, query, sourcePath, filepath.Base(sourcePath))
	}

	return filepath.WalkDir(sourcePath, func(name string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		relative, err := filepath.Rel(sourcePath, name)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", name, err)
		}
		return uploadFile(ctx, base, query, name, filepath.ToSlash(relative))
	})
}

// uploadFile writes a single blob. The credential is a container SAS supplied
// by the service rather than by the client, so the request is made directly
// instead of through the Foundry client, which would attach the project
// credential to a storage host.
func uploadFile(ctx context.Context, base, query, localPath, blobName string) error {
	contents, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", localPath, err)
	}

	target := strings.TrimRight(base, "/") + "/" + escapeBlobPath(blobName) + "?" + query
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, target, bytes.NewReader(contents))
	if err != nil {
		return fmt.Errorf("create upload request for %s: %w", blobName, err)
	}
	request.Header.Set("x-ms-blob-type", "BlockBlob")
	request.ContentLength = int64(len(contents))

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("upload %s: %w", blobName, err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("upload %s returned %s: %s", blobName, http.StatusText(response.StatusCode), strings.TrimSpace(string(body)))
	}
	return nil
}

// escapeBlobPath percent-encodes each path segment while leaving the separators
// intact, so a file name containing a space or a hash does not truncate the
// blob path or collide with the shared access signature's query string.
func escapeBlobPath(name string) string {
	segments := strings.Split(name, "/")
	for index, segment := range segments {
		segments[index] = url.PathEscape(segment)
	}
	return strings.Join(segments, "/")
}
