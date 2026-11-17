package provider

import (
	"context"
	"net/http"

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
	_ resource.Resource                   = &evaluatorVersionResource{}
	_ resource.ResourceWithConfigure      = &evaluatorVersionResource{}
	_ resource.ResourceWithImportState    = &evaluatorVersionResource{}
	_ resource.ResourceWithValidateConfig = &evaluatorVersionResource{}
)

func NewEvaluatorVersionResource() resource.Resource {
	return &evaluatorVersionResource{}
}

type evaluatorVersionResource struct {
	previewGate
}

// evaluatorVersionModel models a single immutable evaluator version. Only one
// of the *_type-specific attribute groups is populated at a time, matching
// evaluator_type in the schema.
type evaluatorVersionModel struct {
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Type        types.String `tfsdk:"type"`
	Categories  types.List   `tfsdk:"categories"`

	// prompt
	PromptText types.String `tfsdk:"prompt_text"`
	// code
	CodeText types.String `tfsdk:"code_text"`
	BlobURI  types.String `tfsdk:"blob_uri"`
	// rubric
	ScoringMode types.String              `tfsdk:"scoring_mode"`
	Dimensions  []evaluatorDimensionModel `tfsdk:"dimensions"`
	// endpoint
	ConnectionName types.String `tfsdk:"connection_name"`

	Metrics types.Map `tfsdk:"metrics"`

	ID          types.String `tfsdk:"id"`
	Version     types.String `tfsdk:"version"`
	DisplayName types.String `tfsdk:"display_name"`
	CreatedAt   types.String `tfsdk:"created_at"`
	ModifiedAt  types.String `tfsdk:"modified_at"`
}

type evaluatorDimensionModel struct {
	ID               types.String  `tfsdk:"id"`
	Description      types.String  `tfsdk:"description"`
	Weight           types.Float64 `tfsdk:"weight"`
	Allowed          types.Bool    `tfsdk:"allowed"`
	AlwaysApplicable types.Bool    `tfsdk:"always_applicable"`
}

type evaluatorDefinitionRequest struct {
	Type           string                       `json:"type"`
	PromptText     string                       `json:"prompt_text,omitempty"`
	CodeText       string                       `json:"code_text,omitempty"`
	BlobURI        string                       `json:"blob_uri,omitempty"`
	Dimensions     []evaluatorDimensionWire     `json:"dimensions,omitempty"`
	Scoring        *evaluatorRubricScoring      `json:"scoring,omitempty"`
	ConnectionName string                       `json:"connection_name,omitempty"`
	Metrics        map[string]evaluatorMetricIn `json:"metrics,omitempty"`
}

type evaluatorDimensionWire struct {
	ID               string   `json:"id"`
	Description      string   `json:"description,omitempty"`
	Weight           *float64 `json:"weight,omitempty"`
	Allowed          *bool    `json:"allowed,omitempty"`
	AlwaysApplicable *bool    `json:"always_applicable,omitempty"`
}

type evaluatorRubricScoring struct {
	Mode string `json:"mode"`
}

// evaluatorMetricIn is always sent as an empty object; the service derives
// the metric shape (type, bounds, direction) itself and returns it on read.
type evaluatorMetricIn struct{}

type evaluatorVersionRequest struct {
	Description   string                     `json:"description,omitempty"`
	EvaluatorType string                     `json:"evaluator_type,omitempty"`
	Categories    []string                   `json:"categories,omitempty"`
	Definition    evaluatorDefinitionRequest `json:"definition"`
}

type evaluatorDefinitionResponse struct {
	Type           string                     `json:"type"`
	PromptText     string                     `json:"prompt_text"`
	CodeText       string                     `json:"code_text"`
	BlobURI        string                     `json:"blob_uri"`
	Dimensions     []evaluatorDimensionWire   `json:"dimensions"`
	Scoring        *evaluatorRubricScoring    `json:"scoring"`
	ConnectionName string                     `json:"connection_name"`
	Metrics        map[string]evaluatorMetric `json:"metrics"`
}

type evaluatorVersionResponse struct {
	ID          string                      `json:"id"`
	Name        string                      `json:"name"`
	Version     string                      `json:"version"`
	DisplayName string                      `json:"display_name"`
	Description string                      `json:"description"`
	CreatedAt   string                      `json:"created_at"`
	ModifiedAt  string                      `json:"modified_at"`
	Categories  []string                    `json:"categories"`
	Definition  evaluatorDefinitionResponse `json:"definition"`
}

func (m evaluatorVersionModel) request(ctx context.Context, diagnostics *diag.Diagnostics) evaluatorVersionRequest {
	definition := evaluatorDefinitionRequest{
		Type:    m.Type.ValueString(),
		Metrics: map[string]evaluatorMetricIn{},
	}
	var categories []string
	if !m.Categories.IsNull() && !m.Categories.IsUnknown() {
		diagnostics.Append(m.Categories.ElementsAs(ctx, &categories, false)...)
	}

	switch definition.Type {
	case "prompt":
		definition.PromptText = m.PromptText.ValueString()
	case "code":
		definition.CodeText = m.CodeText.ValueString()
		definition.BlobURI = m.BlobURI.ValueString()
	case "rubric":
		if mode := m.ScoringMode.ValueString(); mode != "" {
			definition.Scoring = &evaluatorRubricScoring{Mode: mode}
		}
		definition.Dimensions = make([]evaluatorDimensionWire, 0, len(m.Dimensions))
		for _, dimension := range m.Dimensions {
			var alwaysApplicable *bool
			if !dimension.AlwaysApplicable.IsUnknown() {
				alwaysApplicable = dimension.AlwaysApplicable.ValueBoolPointer()
			}
			definition.Dimensions = append(definition.Dimensions, evaluatorDimensionWire{
				ID:               dimension.ID.ValueString(),
				Description:      dimension.Description.ValueString(),
				Weight:           dimension.Weight.ValueFloat64Pointer(),
				Allowed:          dimension.Allowed.ValueBoolPointer(),
				AlwaysApplicable: alwaysApplicable,
			})
		}
	case "endpoint":
		definition.ConnectionName = m.ConnectionName.ValueString()
	}

	if !m.Metrics.IsNull() && !m.Metrics.IsUnknown() {
		var names map[string]string
		diagnostics.Append(m.Metrics.ElementsAs(ctx, &names, false)...)
		for name := range names {
			definition.Metrics[name] = evaluatorMetricIn{}
		}
	}

	request := evaluatorVersionRequest{
		Description: m.Description.ValueString(),
		Categories:  categories,
		Definition:  definition,
	}
	if len(categories) > 0 {
		request.EvaluatorType = "custom"
	}
	return request
}

func (m *evaluatorVersionModel) apply(ctx context.Context, response evaluatorVersionResponse, diagnostics *diag.Diagnostics) {
	m.ID = types.StringValue(response.ID)
	m.Version = types.StringValue(response.Version)
	m.DisplayName = optionalString(response.DisplayName)
	m.Description = optionalString(response.Description)
	m.CreatedAt = optionalString(response.CreatedAt)
	m.ModifiedAt = optionalString(response.ModifiedAt)
	m.Type = types.StringValue(response.Definition.Type)

	m.PromptText = optionalString(response.Definition.PromptText)
	m.CodeText = optionalString(response.Definition.CodeText)
	m.BlobURI = optionalString(response.Definition.BlobURI)
	m.ConnectionName = optionalString(response.Definition.ConnectionName)
	categories, diags := types.ListValueFrom(ctx, types.StringType, response.Categories)
	diagnostics.Append(diags...)
	m.Categories = categories
	m.ScoringMode = types.StringNull()
	if response.Definition.Scoring != nil {
		m.ScoringMode = optionalString(response.Definition.Scoring.Mode)
	}

	if len(response.Definition.Dimensions) == 0 {
		m.Dimensions = nil
	} else {
		m.Dimensions = make([]evaluatorDimensionModel, 0, len(response.Definition.Dimensions))
		for _, dimension := range response.Definition.Dimensions {
			m.Dimensions = append(m.Dimensions, evaluatorDimensionModel{
				ID:               types.StringValue(dimension.ID),
				Description:      optionalString(dimension.Description),
				Weight:           types.Float64PointerValue(dimension.Weight),
				Allowed:          types.BoolPointerValue(dimension.Allowed),
				AlwaysApplicable: types.BoolPointerValue(dimension.AlwaysApplicable),
			})
		}
	}

	metricNames := make(map[string]string, len(response.Definition.Metrics))
	for name := range response.Definition.Metrics {
		metricNames[name] = name
	}
	if len(metricNames) == 0 {
		m.Metrics = types.MapNull(types.StringType)
	} else {
		value, diags := types.MapValueFrom(ctx, types.StringType, metricNames)
		diagnostics.Append(diags...)
		m.Metrics = value
	}
}

func (m evaluatorVersionModel) versionsPath() string {
	return "evaluators/" + m.Name.ValueString() + "/versions"
}

func (m evaluatorVersionModel) versionPath() string {
	return "evaluators/" + m.Name.ValueString() + "/versions/" + m.Version.ValueString()
}

func (r *evaluatorVersionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_evaluator_version"
}

func (r *evaluatorVersionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	requiresReplace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: evaluationsPreviewNote + "Manages a single immutable version of a Foundry evaluator: a code, prompt, rubric, or endpoint based scoring definition used by evaluations and evaluation rules. Publishing a new version never mutates an existing one; `terraform apply` on a changed configuration creates a new version under the same evaluator name.\n\n" +
			"For a [safety rubric](https://learn.microsoft.com/azure/foundry/concepts/evaluation-evaluators/safety-rubric-evaluators), set `type = \"rubric\"`, `categories = [\"safety\"]`, and `scoring_mode = \"boolean\"`. Provide a description and an explicit `allowed` value for each of up to 20 dimensions. Leave out weights. Microsoft hosts the judge, so evaluation runs do not need a judge deployment for this rubric.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the evaluator. Changing this forces a new evaluator to be created.",
				PlanModifiers:       requiresReplace,
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Description of the evaluator version.",
			},
			"type": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Evaluator kind. One of `prompt`, `code`, `rubric`, or `endpoint`. Changing this forces a new evaluator version to be created.",
				PlanModifiers:       requiresReplace,
				Validators:          []validator.String{stringvalidator.OneOf("prompt", "code", "rubric", "endpoint")},
			},
			"categories": schema.ListAttribute{
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Evaluator categories. Set to `[\"safety\"]` for a safety rubric.",
			},
			"scoring_mode": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Rubric scoring mode: `boolean` or `likert`. Set to `boolean` for a safety rubric. Omit to keep legacy rubric scoring.",
				Validators:          []validator.String{stringvalidator.OneOf("boolean", "likert")},
			},
			"prompt_text": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "LLM judge prompt template. Required when `type` is `prompt`.",
				PlanModifiers:       requiresReplace,
			},
			"code_text": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Inline Python evaluator source. Required when `type` is `code`, unless `blob_uri` is set.",
				PlanModifiers:       requiresReplace,
			},
			"blob_uri": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Blob URI of an existing Python evaluator source file. Required when `type` is `code`, unless `code_text` is set.",
				PlanModifiers:       requiresReplace,
			},
			"dimensions": schema.ListNestedAttribute{
				Optional:            true,
				MarkdownDescription: "Scoring dimensions. Required when `type` is `rubric`.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Required:            true,
							MarkdownDescription: "Stable identifier of the dimension, used to correlate rubric scores across versions.",
						},
						"description": schema.StringAttribute{
							Optional:            true,
							MarkdownDescription: "Description of the dimension shown to the LLM judge.",
						},
						"weight": schema.Float64Attribute{
							Optional:            true,
							MarkdownDescription: "Relative weight of the dimension in the overall rubric score. Omit for Boolean scoring.",
						},
						"allowed": schema.BoolAttribute{
							Optional:            true,
							MarkdownDescription: "Set on every safety dimension: `true` for behavior the agent should follow and `false` for behavior it must avoid. Omit for quality rubrics.",
						},
						"always_applicable": schema.BoolAttribute{
							Optional:            true,
							Computed:            true,
							MarkdownDescription: "Whether the judge always checks this dimension. The service defaults to `false`.",
						},
					},
				},
			},
			"connection_name": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Name of the connection the endpoint evaluator calls. Required when `type` is `endpoint`.",
				PlanModifiers:       requiresReplace,
			},
			"metrics": schema.MapAttribute{
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Metric names produced by this evaluator, mapped to themselves, for example `{ score = \"score\" }`. Omit for a safety rubric; the service reports allowed and disallowed behavior violations separately.",
				PlanModifiers:       []planmodifier.Map{},
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Service-assigned identifier of this evaluator version, in `azureai://.../evaluators/{name}/versions/{version}` form.",
			},
			"version": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Service-assigned version number of this evaluator version.",
			},
			"display_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Display name assigned to the evaluator version.",
			},
			"created_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Timestamp when the evaluator version was created.",
			},
			"modified_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Timestamp when the evaluator version was last modified.",
			},
		},
	}
}

func (r *evaluatorVersionResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	r.ValidateResourceConfig(ctx, req, resp)

	var config evaluatorVersionModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if config.Type.IsUnknown() || config.Type.IsNull() {
		return
	}

	switch config.Type.ValueString() {
	case "prompt":
		if config.PromptText.IsNull() || config.PromptText.ValueString() == "" {
			resp.Diagnostics.AddAttributeError(path.Root("prompt_text"), "Missing required attribute", `"prompt_text" is required when "type" is "prompt".`)
		}
	case "code":
		codeEmpty := config.CodeText.IsNull() || config.CodeText.ValueString() == ""
		blobEmpty := config.BlobURI.IsNull() || config.BlobURI.ValueString() == ""
		if codeEmpty && blobEmpty {
			resp.Diagnostics.AddAttributeError(path.Root("code_text"), "Missing required attribute", `Either "code_text" or "blob_uri" is required when "type" is "code".`)
		}
	case "rubric":
		if len(config.Dimensions) == 0 {
			resp.Diagnostics.AddAttributeError(path.Root("dimensions"), "Missing required attribute", `"dimensions" must contain at least one entry when "type" is "rubric".`)
		}
	case "endpoint":
		if config.ConnectionName.IsNull() || config.ConnectionName.ValueString() == "" {
			resp.Diagnostics.AddAttributeError(path.Root("connection_name"), "Missing required attribute", `"connection_name" is required when "type" is "endpoint".`)
		}
	}
}

func (r *evaluatorVersionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client := clientFromProviderData(req.ProviderData, &resp.Diagnostics)
	r.previewGate = previewGate{client: client, feature: evaluationsPreviewFeature, name: evaluationsPreviewFeatureName}
}

func (r *evaluatorVersionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan evaluatorVersionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, err := r.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Preview feature not enabled", err.Error())
		return
	}

	var response evaluatorVersionResponse
	if err := r.client.JSON(ctx, http.MethodPost, plan.versionsPath(), plan.request(ctx, &resp.Diagnostics), &response); err != nil {
		resp.Diagnostics.AddError("Unable to create evaluator version", err.Error())
		return
	}

	plan.apply(ctx, response, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *evaluatorVersionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state evaluatorVersionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, err := r.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Preview feature not enabled", err.Error())
		return
	}

	var response evaluatorVersionResponse
	err = r.client.JSON(ctx, http.MethodGet, state.versionPath(), nil, &response)
	if clients.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read evaluator version", err.Error())
		return
	}

	state.Name = types.StringValue(response.Name)
	state.apply(ctx, response, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update publishes a new evaluator version, because the service treats each
// version as immutable.
func (r *evaluatorVersionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan evaluatorVersionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, err := r.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Preview feature not enabled", err.Error())
		return
	}

	var response evaluatorVersionResponse
	if err := r.client.JSON(ctx, http.MethodPost, plan.versionsPath(), plan.request(ctx, &resp.Diagnostics), &response); err != nil {
		resp.Diagnostics.AddError("Unable to update evaluator version", err.Error())
		return
	}

	plan.apply(ctx, response, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *evaluatorVersionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state evaluatorVersionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, err := r.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Preview feature not enabled", err.Error())
		return
	}

	// Delete the whole evaluator, since Terraform models a single managed
	// version and there is no sibling version left for another resource to
	// reference once this one is destroyed.
	err = r.client.JSON(ctx, http.MethodDelete, "evaluators/"+state.Name.ValueString(), nil, nil)
	if err != nil && !clients.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete evaluator", err.Error())
	}
}

func (r *evaluatorVersionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	name, version, err := splitNameVersion(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Unable to import evaluator version", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("version"), version)...)
}
