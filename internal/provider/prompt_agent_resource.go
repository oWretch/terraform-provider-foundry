package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

var (
	_ resource.Resource                = &promptAgentResource{}
	_ resource.ResourceWithConfigure   = &promptAgentResource{}
	_ resource.ResourceWithImportState = &promptAgentResource{}
)

func NewPromptAgentResource() resource.Resource {
	return &promptAgentResource{}
}

type promptAgentResource struct {
	client *clients.Client
}

type promptAgentModel struct {
	agentCommon
	Model        types.String `tfsdk:"model"`
	Instructions types.String `tfsdk:"instructions"`
}

type promptAgentDefinition struct {
	Kind         string `json:"kind"`
	Model        string `json:"model"`
	Instructions string `json:"instructions,omitempty"`
}

func (m promptAgentModel) definition() promptAgentDefinition {
	return promptAgentDefinition{
		Kind:         "prompt",
		Model:        m.Model.ValueString(),
		Instructions: m.Instructions.ValueString(),
	}
}

func (m *promptAgentModel) apply(version agentVersion, definition promptAgentDefinition) {
	m.applyVersion(version)
	m.Model = types.StringValue(definition.Model)
	m.Instructions = optionalString(definition.Instructions)
}

func (r *promptAgentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_prompt_agent"
}

func (r *promptAgentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attributes := agentCommonSchema()
	attributes["model"] = schema.StringAttribute{
		Required:            true,
		MarkdownDescription: "Name of the model deployment that backs the agent.",
	}
	attributes["instructions"] = schema.StringAttribute{
		Optional:            true,
		MarkdownDescription: "System instructions for the agent.",
	}

	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a prompt agent, which runs a model deployment against a set of instructions. Changing the definition publishes a new agent version.",
		Attributes:          attributes,
	}
}

func (r *promptAgentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (r *promptAgentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan promptAgentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	version, err := createAgent(ctx, r.client, plan.Name.ValueString(), plan.Description.ValueString(), plan.definition())
	if err != nil {
		resp.Diagnostics.AddError("Unable to create prompt agent", err.Error())
		return
	}

	var definition promptAgentDefinition
	if !decodeDefinition(version, &definition, &resp.Diagnostics) {
		return
	}
	plan.apply(version, definition)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *promptAgentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state promptAgentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	version, err := readAgent(ctx, r.client, state.Name.ValueString())
	if clients.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read prompt agent", err.Error())
		return
	}

	var definition promptAgentDefinition
	if !decodeDefinition(version, &definition, &resp.Diagnostics) {
		return
	}
	state.apply(version, definition)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *promptAgentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan promptAgentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	version, err := updateAgent(ctx, r.client, plan.Name.ValueString(), plan.Description.ValueString(), plan.definition())
	if err != nil {
		resp.Diagnostics.AddError("Unable to update prompt agent", err.Error())
		return
	}

	var definition promptAgentDefinition
	if !decodeDefinition(version, &definition, &resp.Diagnostics) {
		return
	}
	plan.apply(version, definition)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *promptAgentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state promptAgentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := deleteAgent(ctx, r.client, state.Name.ValueString()); err != nil {
		resp.Diagnostics.AddError("Unable to delete prompt agent", err.Error())
	}
}

func (r *promptAgentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}
