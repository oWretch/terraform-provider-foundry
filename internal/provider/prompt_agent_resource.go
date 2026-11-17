package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

var (
	_ resource.Resource                   = &promptAgentResource{}
	_ resource.ResourceWithConfigure      = &promptAgentResource{}
	_ resource.ResourceWithImportState    = &promptAgentResource{}
	_ resource.ResourceWithValidateConfig = &promptAgentResource{}
	_ resource.ResourceWithModifyPlan     = &promptAgentResource{}
)

const copilotHarnessType = "microsoft_copilot_preview"

func copilotHarnessGate(client *clients.Client) previewGate {
	return previewGate{client: client, name: "copilot_harness", feature: "MicrosoftCopilot"}
}

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
	HarnessType  types.String `tfsdk:"harness_type"`
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
	Harness          json.RawMessage            `json:"harness,omitempty"`
	Skills           []json.RawMessage          `json:"skills,omitempty"`
	unknownFields    []string
}

func (m promptAgentModel) definition() promptAgentDefinition {
	definition := promptAgentDefinition{
		Kind:         "prompt",
		Model:        m.Model.ValueString(),
		Instructions: m.Instructions.ValueString(),
	}
	if m.HarnessType.ValueString() == copilotHarnessType {
		definition.Harness = json.RawMessage(`{"type":"` + copilotHarnessType + `"}`)
	}
	return definition
}

func (d *promptAgentDefinition) UnmarshalJSON(data []byte) error {
	type definition promptAgentDefinition
	*d = promptAgentDefinition{}
	if err := json.Unmarshal(data, (*definition)(d)); err != nil {
		return err
	}
	if !hasJSONValue(d.Harness) {
		return nil
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	// Do not publish a preview definition if the service returned a field
	// this provider cannot round-trip. Leave default agents on their existing checks.
	for name, value := range fields {
		switch name {
		case "kind", "model", "instructions", "rai_config", "temperature", "top_p",
			"reasoning", "tools", "tool_choice", "text", "structured_inputs", "harness", "skills":
		default:
			if hasJSONValue(value) {
				d.unknownFields = append(d.unknownFields, name)
			}
		}
	}
	sort.Strings(d.unknownFields)
	return nil
}

func (d *promptAgentDefinition) harnessType() (types.String, error) {
	if !hasJSONValue(d.Harness) {
		return types.StringNull(), nil
	}
	var fields map[string]json.RawMessage
	var kind string
	if err := json.Unmarshal(d.Harness, &fields); err != nil ||
		len(fields) != 1 || json.Unmarshal(fields["type"], &kind) != nil || kind != copilotHarnessType {
		return types.StringNull(), fmt.Errorf("harness must contain only the supported type %q", copilotHarnessType)
	}
	return types.StringValue(kind), nil
}

func readPromptHarness(client *clients.Client, definition *promptAgentDefinition, diagnostics *diag.Diagnostics) types.String {
	kind, err := definition.harnessType()
	if err != nil {
		diagnostics.AddError("Unsupported prompt agent harness", err.Error())
	} else if !kind.IsNull() {
		// Agent GET operations do not take the SDK's Foundry-Features option.
		// Enforce the Terraform opt-in after discovering the harness.
		copilotHarnessGate(client).validate(diagnostics)
	}
	return kind
}

func promptPublishContext(ctx context.Context, client *clients.Client, kind types.String) (context.Context, error) {
	switch {
	case kind.IsNull():
		return ctx, nil
	case !kind.IsUnknown() && kind.ValueString() == copilotHarnessType:
		return copilotHarnessGate(client).previewContext(ctx)
	default:
		return ctx, fmt.Errorf("harness_type must be omitted or set to %q", copilotHarnessType)
	}
}

func (*promptAgentDefinition) expectedKind() string {
	return "prompt"
}

func (d *promptAgentDefinition) validateSupported() error {
	fields := append([]string(nil), d.unknownFields...)
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
	if _, err := d.harnessType(); err != nil {
		fields = append(fields, "harness")
	}
	if len(d.Skills) > 0 {
		fields = append(fields, "skills")
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
	attributes["harness_type"] = schema.StringAttribute{
		Optional:   true,
		Validators: []validator.String{stringvalidator.OneOf(copilotHarnessType)},
		MarkdownDescription: "Managed harness type. Omit this attribute to use the default runtime. " +
			"`microsoft_copilot_preview` requires `enable_preview = [\"copilot_harness\"]` on the provider. " +
			"Foundry fixes the harness when the agent is created. Use a new agent name to change it. " +
			"When importing a Copilot agent, set this attribute in your configuration. Terraform does not invoke the agent or manage its tools or skill attachments.",
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
	if config.HarnessType.ValueString() == copilotHarnessType {
		copilotHarnessGate(r.client).validate(&resp.Diagnostics)
	}
}

func (r *promptAgentResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var state, plan promptAgentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if !resp.Diagnostics.HasError() && !plan.Name.IsUnknown() && state.Name.Equal(plan.Name) &&
		!plan.HarnessType.IsUnknown() && !state.HarnessType.Equal(plan.HarnessType) {
		rejectPromptHarnessChange(&resp.Diagnostics)
	}
}

func rejectPromptHarnessChange(diagnostics *diag.Diagnostics) {
	diagnostics.AddAttributeError(path.Root("harness_type"), "Cannot change the prompt agent harness",
		"Foundry fixes the harness when the agent is created. Use a new agent name to change it.")
}

func checkPublishedPromptHarness(definition *promptAgentDefinition, expected types.String, diagnostics *diag.Diagnostics) bool {
	actual, err := definition.harnessType()
	if err != nil || !actual.Equal(expected) {
		diagnostics.AddError("Unexpected prompt agent harness", "Foundry returned a different harness than requested.")
		return false
	}
	return true
}

func (r *promptAgentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan promptAgentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	publishCtx, err := promptPublishContext(ctx, r.client, plan.HarnessType)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create prompt agent", err.Error())
		return
	}
	version, endpoint, err := createAgent(publishCtx, r.client, plan.Name.ValueString(), plan.Description.ValueString(), plan.definition(), plan.Draft.ValueBool())
	if err != nil {
		resp.Diagnostics.AddError("Unable to create prompt agent", err.Error())
		return
	}

	var definition promptAgentDefinition
	if !decodeManagedDefinition(version, &definition, &resp.Diagnostics) ||
		!checkPublishedPromptHarness(&definition, plan.HarnessType, &resp.Diagnostics) {
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
	state.HarnessType = readPromptHarness(r.client, &definition, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
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

	publishCtx, err := promptPublishContext(ctx, r.client, plan.HarnessType)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update prompt agent", err.Error())
		return
	}
	var current promptAgentDefinition
	if !validateAgentBeforeUpdate(ctx, r.client, plan.Name.ValueString(), &current, &resp.Diagnostics) {
		return
	}
	currentHarness, _ := current.harnessType() // Already validated by validateAgentBeforeUpdate.
	if !currentHarness.Equal(plan.HarnessType) {
		rejectPromptHarnessChange(&resp.Diagnostics)
		return
	}

	version, err := updateAgent(publishCtx, r.client, plan.Name.ValueString(), plan.Description.ValueString(), plan.definition(), plan.Draft.ValueBool())
	if err != nil {
		resp.Diagnostics.AddError("Unable to update prompt agent", err.Error())
		return
	}

	var definition promptAgentDefinition
	if !decodeManagedDefinition(version, &definition, &resp.Diagnostics) ||
		!checkPublishedPromptHarness(&definition, plan.HarnessType, &resp.Diagnostics) {
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
