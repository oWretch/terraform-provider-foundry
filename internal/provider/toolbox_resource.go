package provider

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

const toolboxesFeature = "Toolboxes"

// toolboxesFeatureName is the enable_preview value users write to opt in.
const toolboxesFeatureName = "toolboxes"

var (
	_ resource.Resource                   = &toolboxResource{}
	_ resource.ResourceWithConfigure      = &toolboxResource{}
	_ resource.ResourceWithImportState    = &toolboxResource{}
	_ resource.ResourceWithValidateConfig = &toolboxResource{}
)

func NewToolboxResource() resource.Resource {
	return &toolboxResource{previewGate: previewGate{feature: toolboxesFeature, name: toolboxesFeatureName}}
}

// toolboxResource is the single owner of a named toolbox's version chain.
// Create publishes version "1", which the service auto-promotes to default.
// Update publishes a new version from the changed tool/skill configuration
// and then explicitly promotes it, because unlike the first version, later
// toolbox versions do not auto-promote. This mirrors how agent_common.go's
// agents publish immutable versions via POST .../versions on update.
type toolboxResource struct {
	previewGate
}

type toolboxSkillReferenceModel struct {
	Name    types.String `tfsdk:"name"`
	Version types.String `tfsdk:"version"`
}

type toolboxModel struct {
	Name           types.String                 `tfsdk:"name"`
	Description    types.String                 `tfsdk:"description"`
	Tools          types.String                 `tfsdk:"tools_json"`
	Skills         []toolboxSkillReferenceModel `tfsdk:"skills"`
	ID             types.String                 `tfsdk:"id"`
	Version        types.String                 `tfsdk:"version"`
	DefaultVersion types.String                 `tfsdk:"default_version"`
}

type toolboxSkillReference struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type toolboxVersionRequest struct {
	Description string                  `json:"description,omitempty"`
	Tools       json.RawMessage         `json:"tools"`
	Skills      []toolboxSkillReference `json:"skills,omitempty"`
}

// toolboxVersionResponse is the ToolboxVersionObject returned by the create
// and get-version endpoints.
type toolboxVersionResponse struct {
	ID          string                  `json:"id"`
	Name        string                  `json:"name"`
	Version     string                  `json:"version"`
	Description string                  `json:"description"`
	Tools       json.RawMessage         `json:"tools"`
	Skills      []toolboxSkillReference `json:"skills"`
}

// toolboxResponse is the ToolboxObject parent returned by get/update.
type toolboxResponse struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	DefaultVersion string `json:"default_version"`
}

func (m toolboxModel) skillReferences() []toolboxSkillReference {
	if len(m.Skills) == 0 {
		return nil
	}
	references := make([]toolboxSkillReference, 0, len(m.Skills))
	for _, skill := range m.Skills {
		references = append(references, toolboxSkillReference{
			Type:    "skill_reference",
			Name:    skill.Name.ValueString(),
			Version: skill.Version.ValueString(),
		})
	}
	return references
}

func (m *toolboxModel) applyVersion(version toolboxVersionResponse) {
	m.ID = types.StringValue(version.ID)
	m.Version = types.StringValue(version.Version)
	m.Description = optionalString(version.Description)
	m.Tools = stringFromRawJSON(version.Tools)

	if len(version.Skills) == 0 {
		m.Skills = nil
	} else {
		skills := make([]toolboxSkillReferenceModel, 0, len(version.Skills))
		for _, skill := range version.Skills {
			skills = append(skills, toolboxSkillReferenceModel{
				Name:    types.StringValue(skill.Name),
				Version: optionalString(skill.Version),
			})
		}
		m.Skills = skills
	}
}

func (r *toolboxResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_toolbox"
}

func (r *toolboxResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "~> **Preview:** This resource requires `\"toolboxes\"` in the provider's `enable_preview` attribute. Preview features may change their inputs, outputs, or behavior in any provider release without following semantic versioning.\n\n" +
			"Manages a Foundry toolbox, the single owner of a named, versioned bundle of tools exposed through a single MCP endpoint. Changing `tools_json`, `skills`, or `description` publishes a new immutable toolbox version and promotes it to the toolbox's default version.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Toolbox name. Changing this forces a new toolbox to be created.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Description of the toolbox version. Changing this publishes a new toolbox version.",
			},
			"tools_json": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "JSON-encoded array of tool configuration objects, matching the toolbox version `tools` field (for example `jsonencode([{ type = \"web_search\" }])`). At least one of `tools_json` or `skills` must be set. Changing this publishes a new toolbox version.",
			},
			"skills": schema.ListNestedAttribute{
				Optional:            true,
				MarkdownDescription: "Skills attached to the toolbox as MCP resources. At least one of `tools_json` or `skills` must be set. Changing this publishes a new toolbox version.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Required:            true,
							MarkdownDescription: "Name of an existing skill in the same Foundry project.",
						},
						"version": schema.StringAttribute{
							Optional:            true,
							MarkdownDescription: "Skill version to pin the reference to. Omit to follow the skill's default version.",
						},
					},
				},
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Identifier of the current toolbox version.",
			},
			"version": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Current toolbox version number.",
			},
			"default_version": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Version currently promoted as the toolbox's default version, served by the toolbox consumer MCP endpoint. Matches `version` after a successful apply.",
			},
		},
	}
}

func (r *toolboxResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

// ValidateConfig delegates to previewGate. Go's embedding does not promote
// previewGate.ValidateResourceConfig to satisfy resource.ResourceWithValidateConfig
// on *toolboxResource automatically, so this thin wrapper keeps the
// interface satisfied explicitly.
func (r *toolboxResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	r.ValidateResourceConfig(ctx, req, resp)
}

func (r *toolboxResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan toolboxModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	version, err := r.createVersion(ctx, plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to create toolbox", err.Error())
		return
	}

	plan.applyVersion(version)
	// The service auto-promotes the first version of a new toolbox.
	plan.DefaultVersion = types.StringValue(version.Version)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *toolboxResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state toolboxModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	previewCtx, err := r.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read toolbox", err.Error())
		return
	}

	var toolbox toolboxResponse
	err = r.client.JSON(previewCtx, http.MethodGet, parentPath("toolboxes", state.Name.ValueString()), nil, &toolbox)
	if clients.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read toolbox", err.Error())
		return
	}

	var version toolboxVersionResponse
	err = r.client.JSON(previewCtx, http.MethodGet, versionPath("toolboxes", state.Name.ValueString(), toolbox.DefaultVersion), nil, &version)
	if clients.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read toolbox version", err.Error())
		return
	}

	state.applyVersion(version)
	state.DefaultVersion = types.StringValue(toolbox.DefaultVersion)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *toolboxResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan toolboxModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	version, err := r.createVersion(ctx, plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to publish new toolbox version", err.Error())
		return
	}
	plan.applyVersion(version)
	plan.DefaultVersion = types.StringValue(version.Version)

	// Unlike the first version of a new toolbox, later versions don't
	// auto-promote, so explicitly point default_version at the new version.
	if err := promoteDefaultVersion(ctx, r.previewGate, "toolboxes", plan.Name.ValueString(), version.Version, http.MethodPatch); err != nil {
		resp.Diagnostics.AddError("Unable to promote new toolbox version to default", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *toolboxResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state toolboxModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := deleteParent(ctx, r.previewGate, "toolboxes", state.Name.ValueString()); err != nil {
		resp.Diagnostics.AddError("Unable to delete toolbox", err.Error())
	}
}

func (r *toolboxResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}

// createVersion publishes a new toolbox version from the plan's tools and
// skill references.
func (r *toolboxResource) createVersion(ctx context.Context, plan toolboxModel, diagnostics *diag.Diagnostics) (toolboxVersionResponse, error) {
	ctx, err := r.previewContext(ctx)
	if err != nil {
		return toolboxVersionResponse{}, err
	}

	tools := rawJSONOrEmpty(plan.Tools)
	if tools == nil {
		tools = []byte("[]")
	} else if !json.Valid(tools) {
		diagnostics.AddError("Invalid tools_json", "tools_json must be valid JSON encoding an array of tool objects.")
		return toolboxVersionResponse{}, nil
	}

	var version toolboxVersionResponse
	err = r.client.JSON(ctx, http.MethodPost, versionsPath("toolboxes", plan.Name.ValueString()), toolboxVersionRequest{
		Description: plan.Description.ValueString(),
		Tools:       tools,
		Skills:      plan.skillReferences(),
	}, &version)
	return version, err
}
