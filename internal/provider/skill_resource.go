package provider

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

const skillsFeature = "Skills"

// skillsFeatureName is the enable_preview value users write to opt in.
const skillsFeatureName = "skills"

var (
	_ resource.Resource                   = &skillResource{}
	_ resource.ResourceWithConfigure      = &skillResource{}
	_ resource.ResourceWithImportState    = &skillResource{}
	_ resource.ResourceWithValidateConfig = &skillResource{}
)

func NewSkillResource() resource.Resource {
	return &skillResource{previewGate: previewGate{feature: skillsFeature, name: skillsFeatureName}}
}

// skillResource is the single owner of a named skill's version chain. Create
// publishes version "1"; Update publishes a new version from changed content
// and promotes it to default, mirroring how agent_common.go's agents publish
// immutable versions via POST .../versions on update.
type skillResource struct {
	previewGate
}

type skillModel struct {
	Name           types.String `tfsdk:"name"`
	Description    types.String `tfsdk:"description"`
	Instructions   types.String `tfsdk:"instructions"`
	SourcePath     types.String `tfsdk:"source_path"`
	SourceHash     types.String `tfsdk:"source_hash"`
	ID             types.String `tfsdk:"id"`
	Version        types.String `tfsdk:"version"`
	SkillID        types.String `tfsdk:"skill_id"`
	DefaultVersion types.String `tfsdk:"default_version"`
}

type skillInlineContent struct {
	Description  string `json:"description,omitempty"`
	Instructions string `json:"instructions,omitempty"`
}

type skillVersionRequest struct {
	InlineContent *skillInlineContent `json:"inline_content,omitempty"`
}

// skillVersionResponse is the SkillVersion object returned by the create and
// get-version endpoints.
type skillVersionResponse struct {
	ID          string `json:"id"`
	SkillID     string `json:"skill_id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

// skillResponse is the Skill parent object returned by get/list/update.
type skillResponse struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	DefaultVersion string `json:"default_version"`
	LatestVersion  string `json:"latest_version"`
}

func (m *skillModel) applyVersion(version skillVersionResponse) {
	m.ID = types.StringValue(version.ID)
	m.SkillID = types.StringValue(version.SkillID)
	m.Version = types.StringValue(version.Version)
	m.Description = optionalString(version.Description)
}

func (r *skillResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_skill"
}

func (r *skillResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "~> **Preview:** This resource requires `\"skills\"` in the provider's `enable_preview` attribute. Preview features may change their inputs, outputs, or behavior in any provider release without following semantic versioning.\n\n" +
			"Manages a Foundry skill, the single owner of a named, versioned skill's content. A skill is a reusable Markdown-based behavioral guideline (a `SKILL.md` file) that agents and toolboxes can attach or download. Changing `instructions`, `description`, `source_path`, or `source_hash` publishes a new immutable skill version and promotes it to the skill's default version.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Skill name, used as the URL path key. Lowercase letters, numbers, and hyphens only; must not start or end with a hyphen. Maximum 64 characters. Changing this forces a new skill to be created.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{
					stringvalidator.LengthAtMost(64),
					stringvalidator.RegexMatches(skillNamePattern, "must be lowercase letters, numbers, and hyphens, and must not start or end with a hyphen"),
				},
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Description of the skill version, shown in skill listings. Maximum 1024 characters. Changing this publishes a new skill version.",
				Validators:          []validator.String{stringvalidator.LengthAtMost(1024)},
			},
			"instructions": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Inline Markdown instructions for the skill. Mutually exclusive with `source_path`. Changing this publishes a new skill version.",
				Validators:          []validator.String{stringvalidator.ConflictsWith(path.MatchRoot("source_path"))},
			},
			"source_path": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Local path to a `SKILL.md` file or a `.zip` archive containing one, uploaded as the skill's content. Mutually exclusive with `instructions`. Changing this or `source_hash` publishes a new skill version.",
				Validators:          []validator.String{stringvalidator.ConflictsWith(path.MatchRoot("instructions"))},
			},
			"source_hash": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Hash of the file at `source_path`, such as `filesha256(\"path\")`. Supply this to force a new skill version when the file contents change without changing `source_path`.",
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Identifier of the current skill version.",
			},
			"version": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Current skill version number.",
			},
			"skill_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Service-assigned identifier of the parent skill.",
			},
			"default_version": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Version currently promoted as the skill's default version. Matches `version` after a successful apply.",
			},
		},
	}
}

func (r *skillResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

// ValidateConfig delegates to previewGate. Go's embedding does not promote
// previewGate.ValidateResourceConfig to satisfy resource.ResourceWithValidateConfig
// on *skillResource automatically because the method is on the value receiver
// of an embedded struct with a different method name signature match; this
// thin wrapper keeps the interface satisfied explicitly.
func (r *skillResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	r.ValidateResourceConfig(ctx, req, resp)
}

func (r *skillResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan skillModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	version, err := r.createVersion(ctx, plan)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create skill", err.Error())
		return
	}
	plan.applyVersion(version)
	plan.DefaultVersion = types.StringValue(version.Version)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *skillResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state skillModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	previewCtx, err := r.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read skill", err.Error())
		return
	}

	var skill skillResponse
	err = r.client.JSON(previewCtx, http.MethodGet, parentPath("skills", state.Name.ValueString()), nil, &skill)
	if clients.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read skill", err.Error())
		return
	}

	var version skillVersionResponse
	err = r.client.JSON(previewCtx, http.MethodGet, versionPath("skills", state.Name.ValueString(), skill.DefaultVersion), nil, &version)
	if clients.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read skill version", err.Error())
		return
	}

	state.applyVersion(version)
	state.DefaultVersion = types.StringValue(skill.DefaultVersion)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *skillResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan skillModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	version, err := r.createVersion(ctx, plan)
	if err != nil {
		resp.Diagnostics.AddError("Unable to publish new skill version", err.Error())
		return
	}
	plan.applyVersion(version)
	plan.DefaultVersion = types.StringValue(version.Version)

	// Skill versions don't auto-promote on update the way the first version
	// does, so explicitly point default_version at the new version.
	if err := promoteDefaultVersion(ctx, r.previewGate, "skills", plan.Name.ValueString(), version.Version, http.MethodPost); err != nil {
		resp.Diagnostics.AddError("Unable to promote new skill version to default", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *skillResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state skillModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := deleteParent(ctx, r.previewGate, "skills", state.Name.ValueString()); err != nil {
		resp.Diagnostics.AddError("Unable to delete skill", err.Error())
	}
}

func (r *skillResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}

// createVersion publishes a new skill version, either from inline content or
// by uploading the file/archive at source_path as multipart form data.
func (r *skillResource) createVersion(ctx context.Context, plan skillModel) (skillVersionResponse, error) {
	ctx, err := r.previewContext(ctx)
	if err != nil {
		return skillVersionResponse{}, err
	}

	name := plan.Name.ValueString()
	if !plan.SourcePath.IsNull() && plan.SourcePath.ValueString() != "" {
		return r.createVersionFromFile(ctx, name, plan.SourcePath.ValueString())
	}

	var version skillVersionResponse
	err = r.client.JSON(ctx, http.MethodPost, versionsPath("skills", name), skillVersionRequest{
		InlineContent: &skillInlineContent{
			Description:  plan.Description.ValueString(),
			Instructions: plan.Instructions.ValueString(),
		},
	}, &version)
	return version, err
}

func (r *skillResource) createVersionFromFile(ctx context.Context, name, sourcePath string) (skillVersionResponse, error) {
	contents, err := os.ReadFile(sourcePath)
	if err != nil {
		return skillVersionResponse{}, fmt.Errorf("read source file: %w", err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("files", filepath.Base(sourcePath))
	if err != nil {
		return skillVersionResponse{}, fmt.Errorf("build upload request: %w", err)
	}
	if _, err := part.Write(contents); err != nil {
		return skillVersionResponse{}, fmt.Errorf("build upload request: %w", err)
	}
	if err := writer.Close(); err != nil {
		return skillVersionResponse{}, fmt.Errorf("build upload request: %w", err)
	}

	request, err := r.client.NewRequest(ctx, http.MethodPost, versionsPath("skills", name), &body)
	if err != nil {
		return skillVersionResponse{}, err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())

	response, err := r.client.Do(request)
	if err != nil {
		return skillVersionResponse{}, err
	}
	defer func() { _ = response.Body.Close() }()

	var decoded skillVersionResponse
	if err := decodeJSON(response.Body, &decoded); err != nil {
		return skillVersionResponse{}, err
	}
	return decoded, nil
}

// skillNamePattern mirrors the service's documented name validation, so
// terraform plan rejects an invalid name before any API call.
var skillNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
