package provider

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
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
	_ resource.Resource                   = &evaluationTaxonomyResource{}
	_ resource.ResourceWithConfigure      = &evaluationTaxonomyResource{}
	_ resource.ResourceWithImportState    = &evaluationTaxonomyResource{}
	_ resource.ResourceWithValidateConfig = &evaluationTaxonomyResource{}
)

func NewEvaluationTaxonomyResource() resource.Resource {
	return &evaluationTaxonomyResource{}
}

type evaluationTaxonomyResource struct {
	previewGate
}

// evaluationTaxonomyModel models an evaluation taxonomy. Categories are
// authored as an ordered list: the service returns categories and their
// subcategories in a stable, service-defined order for a given risk
// category selection, so a list (not a set) keeps plans deterministic
// without requiring an explicit order field.
type evaluationTaxonomyModel struct {
	Name           types.String `tfsdk:"name"`
	Description    types.String `tfsdk:"description"`
	AgentName      types.String `tfsdk:"agent_name"`
	AgentVersion   types.String `tfsdk:"agent_version"`
	RiskCategories types.List   `tfsdk:"risk_categories"`

	ID         types.String                      `tfsdk:"id"`
	Version    types.String                      `tfsdk:"version"`
	Categories []evaluationTaxonomyCategoryModel `tfsdk:"categories"`
}

type evaluationTaxonomyCategoryModel struct {
	ID            types.String                         `tfsdk:"id"`
	Name          types.String                         `tfsdk:"name"`
	Description   types.String                         `tfsdk:"description"`
	RiskCategory  types.String                         `tfsdk:"risk_category"`
	SubCategories []evaluationTaxonomySubCategoryModel `tfsdk:"sub_categories"`
}

type evaluationTaxonomySubCategoryModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Enabled     types.Bool   `tfsdk:"enabled"`
}

type evaluationTaxonomyAgentTarget struct {
	Type    string `json:"type"`
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
}

type evaluationTaxonomyInput struct {
	Type           string                        `json:"type"`
	Target         evaluationTaxonomyAgentTarget `json:"target"`
	RiskCategories []string                      `json:"riskCategories,omitempty"`
}

type evaluationTaxonomyRequest struct {
	Description   string                  `json:"description,omitempty"`
	TaxonomyInput evaluationTaxonomyInput `json:"taxonomyInput"`
}

type evaluationTaxonomySubCategoryResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}

type evaluationTaxonomyCategoryResponse struct {
	ID            string                                  `json:"id"`
	Name          string                                  `json:"name"`
	Description   string                                  `json:"description"`
	RiskCategory  string                                  `json:"riskCategory"`
	SubCategories []evaluationTaxonomySubCategoryResponse `json:"subCategories"`
}

type evaluationTaxonomyResponse struct {
	ID                 string                               `json:"id"`
	Name               string                               `json:"name"`
	Version            string                               `json:"version"`
	Description        string                               `json:"description"`
	TaxonomyInput      evaluationTaxonomyInput              `json:"taxonomyInput"`
	TaxonomyCategories []evaluationTaxonomyCategoryResponse `json:"taxonomyCategories"`
}

func (m evaluationTaxonomyModel) request(ctx context.Context, diagnostics *diag.Diagnostics) evaluationTaxonomyRequest {
	var riskCategories []string
	if !m.RiskCategories.IsNull() {
		diagnostics.Append(m.RiskCategories.ElementsAs(ctx, &riskCategories, false)...)
	}

	return evaluationTaxonomyRequest{
		Description: m.Description.ValueString(),
		TaxonomyInput: evaluationTaxonomyInput{
			Type: "Agent",
			Target: evaluationTaxonomyAgentTarget{
				Type:    "azure_ai_agent",
				Name:    m.AgentName.ValueString(),
				Version: m.AgentVersion.ValueString(),
			},
			RiskCategories: riskCategories,
		},
	}
}

func (m *evaluationTaxonomyModel) apply(ctx context.Context, response evaluationTaxonomyResponse, diagnostics *diag.Diagnostics) {
	m.ID = types.StringValue(response.ID)
	m.Name = types.StringValue(response.Name)
	m.Version = types.StringValue(response.Version)
	m.Description = optionalString(response.Description)
	m.AgentName = optionalString(response.TaxonomyInput.Target.Name)
	m.AgentVersion = optionalString(response.TaxonomyInput.Target.Version)

	if len(response.TaxonomyInput.RiskCategories) == 0 {
		m.RiskCategories = types.ListNull(types.StringType)
	} else {
		value, diags := types.ListValueFrom(ctx, types.StringType, response.TaxonomyInput.RiskCategories)
		diagnostics.Append(diags...)
		m.RiskCategories = value
	}

	m.Categories = make([]evaluationTaxonomyCategoryModel, 0, len(response.TaxonomyCategories))
	for _, category := range response.TaxonomyCategories {
		subCategories := make([]evaluationTaxonomySubCategoryModel, 0, len(category.SubCategories))
		for _, sub := range category.SubCategories {
			subCategories = append(subCategories, evaluationTaxonomySubCategoryModel{
				ID:          types.StringValue(sub.ID),
				Name:        types.StringValue(sub.Name),
				Description: optionalString(sub.Description),
				Enabled:     types.BoolValue(sub.Enabled),
			})
		}
		m.Categories = append(m.Categories, evaluationTaxonomyCategoryModel{
			ID:            types.StringValue(category.ID),
			Name:          types.StringValue(category.Name),
			Description:   optionalString(category.Description),
			RiskCategory:  types.StringValue(category.RiskCategory),
			SubCategories: subCategories,
		})
	}
}

func (m evaluationTaxonomyModel) path() string {
	return "evaluationtaxonomies/" + m.Name.ValueString()
}

func (r *evaluationTaxonomyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_evaluation_taxonomy"
}

func (r *evaluationTaxonomyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: evaluationsPreviewNote + "Manages a Foundry evaluation taxonomy: a risk-category-scoped set of behavioral categories and subcategories generated for an agent, used to structure evaluation rules and reports.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the taxonomy. Changing this forces a new taxonomy to be created.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Description of the taxonomy.",
			},
			"agent_name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the Foundry agent the taxonomy is generated for.",
			},
			"agent_version": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Version of the Foundry agent the taxonomy is generated for. Defaults to the agent's latest version when unset.",
			},
			"risk_categories": schema.ListAttribute{
				Required:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Risk categories to generate the taxonomy for, in the order the service should apply them. Currently only `ProhibitedActions` is supported by the service.",
				Validators: []validator.List{
					listvalidator.SizeAtLeast(1),
				},
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Service-assigned taxonomy identifier.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"version": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Service-assigned taxonomy version.",
			},
			"categories": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Categories generated by the service for the taxonomy, in the order returned by the service.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":            schema.StringAttribute{Computed: true, MarkdownDescription: "Service-assigned category identifier."},
						"name":          schema.StringAttribute{Computed: true, MarkdownDescription: "Category name."},
						"description":   schema.StringAttribute{Computed: true, MarkdownDescription: "Category description."},
						"risk_category": schema.StringAttribute{Computed: true, MarkdownDescription: "Risk category this category belongs to."},
						"sub_categories": schema.ListNestedAttribute{
							Computed:            true,
							MarkdownDescription: "Subcategories within this category, in the order returned by the service.",
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"id":          schema.StringAttribute{Computed: true, MarkdownDescription: "Service-assigned subcategory identifier."},
									"name":        schema.StringAttribute{Computed: true, MarkdownDescription: "Subcategory name."},
									"description": schema.StringAttribute{Computed: true, MarkdownDescription: "Subcategory description."},
									"enabled":     schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the subcategory is enabled for evaluation."},
								},
							},
						},
					},
				},
			},
		},
	}
}

func (r *evaluationTaxonomyResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	r.ValidateResourceConfig(ctx, req, resp)
}

func (r *evaluationTaxonomyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client := clientFromProviderData(req.ProviderData, &resp.Diagnostics)
	r.previewGate = previewGate{client: client, feature: evaluationsPreviewFeature, name: evaluationsPreviewFeatureName}
}

func (r *evaluationTaxonomyResource) put(ctx context.Context, plan *evaluationTaxonomyModel, diagnostics *diag.Diagnostics) (evaluationTaxonomyResponse, error) {
	var response evaluationTaxonomyResponse
	err := r.client.JSON(ctx, http.MethodPut, plan.path(), plan.request(ctx, diagnostics), &response)
	return response, err
}

func (r *evaluationTaxonomyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan evaluationTaxonomyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, err := r.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Preview feature not enabled", err.Error())
		return
	}

	response, err := r.put(ctx, &plan, &resp.Diagnostics)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create evaluation taxonomy", err.Error())
		return
	}

	plan.apply(ctx, response, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *evaluationTaxonomyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state evaluationTaxonomyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, err := r.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Preview feature not enabled", err.Error())
		return
	}

	var response evaluationTaxonomyResponse
	err = r.client.JSON(ctx, http.MethodGet, state.path(), nil, &response)
	if clients.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read evaluation taxonomy", err.Error())
		return
	}

	state.apply(ctx, response, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *evaluationTaxonomyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan evaluationTaxonomyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx, err := r.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Preview feature not enabled", err.Error())
		return
	}

	response, err := r.put(ctx, &plan, &resp.Diagnostics)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update evaluation taxonomy", err.Error())
		return
	}

	plan.apply(ctx, response, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *evaluationTaxonomyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state evaluationTaxonomyModel
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
		resp.Diagnostics.AddError("Unable to delete evaluation taxonomy", err.Error())
	}
}

func (r *evaluationTaxonomyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}
