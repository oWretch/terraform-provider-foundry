package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

var (
	_ resource.Resource                   = &promptAgentResource{}
	_ resource.ResourceWithConfigure      = &promptAgentResource{}
	_ resource.ResourceWithImportState    = &promptAgentResource{}
	_ resource.ResourceWithValidateConfig = &promptAgentResource{}
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
	Kind             string                     `json:"kind"`
	Model            string                     `json:"model"`
	Instructions     string                     `json:"instructions,omitempty"`
	RAIConfig        json.RawMessage            `json:"rai_config,omitempty"`
	Temperature      *float64                   `json:"temperature,omitempty"`
	TopP             *float64                   `json:"top_p,omitempty"`
	Reasoning        json.RawMessage            `json:"reasoning,omitempty"`
	Tools            []json.RawMessage          `json:"tools,omitempty"`
	ToolChoice       json.RawMessage            `json:"tool_choice,omitempty"`
	Text             json.RawMessage            `json:"text,omitempty"`
	StructuredInputs map[string]json.RawMessage `json:"structured_inputs,omitempty"`
}

func (m promptAgentModel) definition() promptAgentDefinition {
	return promptAgentDefinition{
		Kind:         "prompt",
		Model:        m.Model.ValueString(),
		Instructions: m.Instructions.ValueString(),
	}
}

func (*promptAgentDefinition) expectedKind() string {
	return "prompt"
}

func (d *promptAgentDefinition) validateSupported() error {
	var fields []string
	if hasJSONValue(d.RAIConfig) {
		fields = append(fields, "rai_config")
	}
	if d.Temperature != nil && *d.Temperature != 1 {
		fields = append(fields, "temperature")
	}
	if d.TopP != nil && *d.TopP != 1 {
		fields = append(fields, "top_p")
	}
	if hasJSONValue(d.Reasoning) {
		fields = append(fields, "reasoning")
	}
	if len(d.Tools) > 0 {
		fields = append(fields, "tools")
	}
	if hasJSONValue(d.ToolChoice) {
		fields = append(fields, "tool_choice")
	}
	if hasMaterialJSON(d.Text) {
		fields = append(fields, "text")
	}
	if len(d.StructuredInputs) > 0 {
		fields = append(fields, "structured_inputs")
	}
	if len(fields) > 0 {
		return fmt.Errorf(
			"the service returned unsupported fields (%s); remove them before managing this agent because an update would otherwise discard them",
			strings.Join(fields, ", "),
		)
	}
	return nil
}

func (m *promptAgentModel) apply(ctx context.Context, version agentVersion, endpoint *agentEndpoint, definition promptAgentDefinition, diagnostics *diag.Diagnostics) {
	m.applyVersion(version)
	m.applyEndpoint(ctx, endpoint, diagnostics)
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

func (r *promptAgentResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config promptAgentModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	validateDraftPreview(r.client, config.Draft, &resp.Diagnostics)
}

func (r *promptAgentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan promptAgentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	version, endpoint, err := createAgent(ctx, r.client, plan.Name.ValueString(), plan.Description.ValueString(), plan.definition(), plan.Draft.ValueBool())
	if err != nil {
		resp.Diagnostics.AddError("Unable to create prompt agent", err.Error())
		return
	}

	var definition promptAgentDefinition
	if !decodeManagedDefinition(version, &definition, &resp.Diagnostics) {
		return
	}
	plan.apply(ctx, version, endpoint, definition, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *promptAgentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state promptAgentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	version, endpoint, err := readAgent(ctx, r.client, state.Name.ValueString())
	if clients.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read prompt agent", err.Error())
		return
	}

	var definition promptAgentDefinition
	if !decodeManagedDefinition(version, &definition, &resp.Diagnostics) {
		return
	}
	state.apply(ctx, version, endpoint, definition, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *promptAgentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan promptAgentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var current promptAgentDefinition
	if !validateAgentBeforeUpdate(ctx, r.client, plan.Name.ValueString(), &current, &resp.Diagnostics) {
		return
	}

	version, err := updateAgent(ctx, r.client, plan.Name.ValueString(), plan.Description.ValueString(), plan.definition(), plan.Draft.ValueBool())
	if err != nil {
		resp.Diagnostics.AddError("Unable to update prompt agent", err.Error())
		return
	}

	var definition promptAgentDefinition
	if !decodeManagedDefinition(version, &definition, &resp.Diagnostics) {
		return
	}
	// updateAgent does not return endpoint metadata, so re-read it to keep the
	// computed agent_endpoint attribute accurate after publishing a new version.
	_, endpoint, err := readAgent(ctx, r.client, plan.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read prompt agent endpoint", err.Error())
		return
	}
	plan.apply(ctx, version, endpoint, definition, &resp.Diagnostics)
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
