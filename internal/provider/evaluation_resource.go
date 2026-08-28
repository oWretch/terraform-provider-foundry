package provider

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
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
	_ resource.Resource                   = &evaluationResource{}
	_ resource.ResourceWithConfigure      = &evaluationResource{}
	_ resource.ResourceWithImportState    = &evaluationResource{}
	_ resource.ResourceWithValidateConfig = &evaluationResource{}
)

func NewEvaluationResource() resource.Resource {
	return &evaluationResource{}
}

type evaluationResource struct {
	previewGate
}

// evaluationModel models an evaluation definition on the OpenAI-compatible
// /openai/v1/evals route. Terraform manages only the evaluation definition;
// runs, output items, and run cancellation/deletion are intentionally left
// outside this resource.
type evaluationModel struct {
	Name                 types.String                      `tfsdk:"name"`
	DataSourceItemSchema types.String                      `tfsdk:"data_source_item_schema"`
	TestingCriteria      []evaluationTestingCriterionModel `tfsdk:"testing_criteria"`
	Metadata             types.Map                         `tfsdk:"metadata"`

	ID types.String `tfsdk:"id"`
}

// evaluationTestingCriterionModel currently supports only the non-deprecated
// azure_ai_evaluator criterion type, which references a foundry_evaluator_version
// by name (optionally pinned to a version).
type evaluationTestingCriterionModel struct {
	Name             types.String `tfsdk:"name"`
	EvaluatorName    types.String `tfsdk:"evaluator_name"`
	EvaluatorVersion types.String `tfsdk:"evaluator_version"`
	DataMapping      types.Map    `tfsdk:"data_mapping"`
}

// evaluationDataSourceConfigRequest is the request-side shape: the item
// schema is sent as a top-level item_schema field.
type evaluationDataSourceConfigRequest struct {
	Type       string          `json:"type"`
	ItemSchema json.RawMessage `json:"item_schema,omitempty"`
}

// evaluationDataSourceConfigResponse is the response-side shape. The service
// always echoes an empty item_schema field, but the schema actually posted
// is echoed back nested at schema.item instead.
type evaluationDataSourceConfigResponse struct {
	Type   string                         `json:"type"`
	Schema evaluationDataSourceSchemaWire `json:"schema"`
}

type evaluationDataSourceSchemaWire struct {
	Item json.RawMessage `json:"item,omitempty"`
}

type evaluationTestingCriterionWire struct {
	Type             string            `json:"type"`
	Name             string            `json:"name,omitempty"`
	EvaluatorName    string            `json:"evaluator_name,omitempty"`
	EvaluatorVersion string            `json:"evaluator_version,omitempty"`
	DataMapping      map[string]string `json:"data_mapping,omitempty"`
}

type evaluationRequest struct {
	Name             string                            `json:"name"`
	Metadata         map[string]string                 `json:"metadata,omitempty"`
	DataSourceConfig evaluationDataSourceConfigRequest `json:"data_source_config"`
	TestingCriteria  []evaluationTestingCriterionWire  `json:"testing_criteria"`
}

type evaluationResponse struct {
	ID               string                             `json:"id"`
	Name             string                             `json:"name"`
	Metadata         map[string]string                  `json:"metadata"`
	DataSourceConfig evaluationDataSourceConfigResponse `json:"data_source_config"`
	TestingCriteria  []evaluationTestingCriterionWire   `json:"testing_criteria"`
}

func (m evaluationModel) request(ctx context.Context, diagnostics *diag.Diagnostics) evaluationRequest {
	request := evaluationRequest{
		Name: m.Name.ValueString(),
		DataSourceConfig: evaluationDataSourceConfigRequest{
			Type:       "custom",
			ItemSchema: json.RawMessage(m.DataSourceItemSchema.ValueString()),
		},
	}

	if !m.Metadata.IsNull() {
		diagnostics.Append(m.Metadata.ElementsAs(ctx, &request.Metadata, false)...)
	}

	request.TestingCriteria = make([]evaluationTestingCriterionWire, 0, len(m.TestingCriteria))
	for _, criterion := range m.TestingCriteria {
		wire := evaluationTestingCriterionWire{
			Type:             "azure_ai_evaluator",
			Name:             criterion.Name.ValueString(),
			EvaluatorName:    criterion.EvaluatorName.ValueString(),
			EvaluatorVersion: criterion.EvaluatorVersion.ValueString(),
		}
		if !criterion.DataMapping.IsNull() {
			diagnostics.Append(criterion.DataMapping.ElementsAs(ctx, &wire.DataMapping, false)...)
		}
		request.TestingCriteria = append(request.TestingCriteria, wire)
	}

	return request
}

func (m *evaluationModel) apply(ctx context.Context, response evaluationResponse, diagnostics *diag.Diagnostics) {
	m.ID = types.StringValue(response.ID)
	m.Name = types.StringValue(response.Name)

	if len(response.DataSourceConfig.Schema.Item) > 0 {
		m.DataSourceItemSchema = types.StringValue(string(response.DataSourceConfig.Schema.Item))
	}

	if len(response.Metadata) == 0 {
		m.Metadata = types.MapNull(types.StringType)
	} else {
		value, diags := types.MapValueFrom(ctx, types.StringType, response.Metadata)
		diagnostics.Append(diags...)
		m.Metadata = value
	}

	m.TestingCriteria = make([]evaluationTestingCriterionModel, 0, len(response.TestingCriteria))
	for _, criterion := range response.TestingCriteria {
		model := evaluationTestingCriterionModel{
			Name:             types.StringValue(criterion.Name),
			EvaluatorName:    types.StringValue(criterion.EvaluatorName),
			EvaluatorVersion: optionalString(criterion.EvaluatorVersion),
		}
		if len(criterion.DataMapping) == 0 {
			model.DataMapping = types.MapNull(types.StringType)
		} else {
			value, diags := types.MapValueFrom(ctx, types.StringType, criterion.DataMapping)
			diagnostics.Append(diags...)
			model.DataMapping = value
		}
		m.TestingCriteria = append(m.TestingCriteria, model)
	}
}

func (m evaluationModel) path() string {
	return "openai/v1/evals/" + m.ID.ValueString()
}

func (r *evaluationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_evaluation"
}

func (r *evaluationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: evaluationsPreviewNote + "Manages a Foundry evaluation definition on the OpenAI-compatible `/openai/v1/evals` route: a named data source configuration and set of testing criteria against which evaluation runs can later be started. This resource manages only the evaluation definition; runs, output items, and run cancellation or deletion are not modeled in Terraform.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the evaluation.",
			},
			"data_source_item_schema": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "JSON schema (as a JSON-encoded string) describing a single evaluation data item. Use `jsonencode(...)` to build this from an HCL object.",
			},
			"metadata": schema.MapAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Key-value metadata attached to the evaluation.",
			},
			"testing_criteria": schema.ListNestedAttribute{
				Required:            true,
				MarkdownDescription: "Testing criteria evaluated against each data source item. Currently only Azure evaluator references (the non-deprecated criterion type) are supported.",
				Validators: []validator.List{
					listvalidator.SizeAtLeast(1),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Required:            true,
							MarkdownDescription: "Name of this testing criterion within the evaluation.",
						},
						"evaluator_name": schema.StringAttribute{
							Required:            true,
							MarkdownDescription: "Name of the `foundry_evaluator_version` evaluator to reference (its `name` attribute, not the versioned `id`).",
							Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
						},
						"evaluator_version": schema.StringAttribute{
							Optional:            true,
							MarkdownDescription: "Specific evaluator version to pin to. Defaults to the evaluator's latest version when unset.",
						},
						"data_mapping": schema.MapAttribute{
							Optional:            true,
							ElementType:         types.StringType,
							MarkdownDescription: "Maps evaluator input field names to data item field references, such as `{ response = \"{{item.response}}\" }`.",
						},
					},
				},
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Service-assigned evaluation identifier.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *evaluationResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	r.ValidateResourceConfig(ctx, req, resp)
}

func (r *evaluationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client := clientFromProviderData(req.ProviderData, &resp.Diagnostics)
	r.previewGate = previewGate{client: client, feature: evaluationsPreviewFeature, name: evaluationsPreviewFeatureName}
}

func (r *evaluationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan evaluationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, err := r.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Preview feature not enabled", err.Error())
		return
	}

	var response evaluationResponse
	if err := r.client.JSON(ctx, http.MethodPost, "openai/v1/evals", plan.request(ctx, &resp.Diagnostics), &response); err != nil {
		resp.Diagnostics.AddError("Unable to create evaluation", err.Error())
		return
	}

	plan.apply(ctx, response, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *evaluationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state evaluationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, err := r.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Preview feature not enabled", err.Error())
		return
	}

	var response evaluationResponse
	err = r.client.JSON(ctx, http.MethodGet, state.path(), nil, &response)
	if clients.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read evaluation", err.Error())
		return
	}

	state.apply(ctx, response, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *evaluationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan evaluationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var state evaluationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = state.ID

	ctx, err := r.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Preview feature not enabled", err.Error())
		return
	}

	var response evaluationResponse
	if err := r.client.JSON(ctx, http.MethodPost, plan.path(), plan.request(ctx, &resp.Diagnostics), &response); err != nil {
		resp.Diagnostics.AddError("Unable to update evaluation", err.Error())
		return
	}

	plan.apply(ctx, response, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *evaluationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state evaluationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, err := r.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Preview feature not enabled", err.Error())
		return
	}

	err = r.client.JSON(ctx, http.MethodDelete, state.path(), nil, nil)
	if err != nil && !clients.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete evaluation", err.Error())
	}
}

func (r *evaluationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
