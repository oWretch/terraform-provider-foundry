package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

func NewVoiceAgentResource() resource.Resource { return &voiceAgentResource{} }

var (
	_ resource.ResourceWithConfigure   = &voiceAgentResource{}
	_ resource.ResourceWithImportState = &voiceAgentResource{}
)

type voiceAgentResource struct {
	client *clients.Client
}

type voiceAgentModel struct {
	agentCommon
	voiceAgentSettings
}

type voiceAgentSettings struct {
	Model        types.String `tfsdk:"model"`
	ModelType    types.String `tfsdk:"model_type"`
	Instructions types.String `tfsdk:"instructions"`
	Store        types.Bool   `tfsdk:"store"`
	Voice        types.String `tfsdk:"voice"`
	VoiceType    types.String `tfsdk:"voice_type"`
}

const (
	voiceModelDescription        = "Managed model name or existing deployment name, depending on `model_type`."
	voiceModelTypeDescription    = "How the model is served: `managed` or `self_deployed`."
	voiceInstructionsDescription = "Instructions for the voice agent."
	voiceStoreDescription        = "Whether Foundry stores conversation content and audio. Defaults to `false`."
	voiceNameDescription         = "Voice name, such as `en-US-AvaNeural`."
	voiceTypeDescription         = "Voice implementation, such as `azure-standard` or `openai`."
)

type voiceAgentDefinition struct {
	Kind             string           `json:"kind"`
	SubProtocol      string           `json:"sub_protocol"`
	Model            string           `json:"model,omitempty"`
	ModelType        string           `json:"model_type,omitempty"`
	Instructions     *string          `json:"instructions,omitempty"`
	Store            *bool            `json:"store,omitempty"`
	OutputModalities []string         `json:"output_modalities,omitempty"`
	Audio            *voiceAgentAudio `json:"audio,omitempty"`
}

type voiceAgentAudio struct {
	Output *voiceAgentOutput `json:"output,omitempty"`
}

type voiceAgentOutput struct {
	Voice     string `json:"voice,omitempty"`
	VoiceType string `json:"voice_type,omitempty"`
}

// The read-only data source can project the supported fields from a richer
// definition. The resource decodes the original JSON again in strict mode so
// even an unmodeled field inside audio.output cannot be lost on update.
type managedVoiceAgentDefinition struct {
	voiceAgentDefinition
}

func (d *managedVoiceAgentDefinition) UnmarshalJSON(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&d.voiceAgentDefinition); err != nil {
		return fmt.Errorf("cannot manage this voice definition without losing unmodeled fields: %w", err)
	}
	return nil
}

func (*voiceAgentDefinition) expectedKind() string { return "voice" }

func (d *voiceAgentDefinition) validateSupported() error {
	if d.SubProtocol != "realtime" {
		return fmt.Errorf("unsupported sub_protocol %q; only realtime is supported", d.SubProtocol)
	}
	if d.Model == "" || (d.ModelType != "managed" && d.ModelType != "self_deployed") {
		return fmt.Errorf("only model-backed definitions with a managed or self_deployed model are supported")
	}
	if d.Audio == nil || d.Audio.Output == nil || d.Audio.Output.Voice == "" || d.Audio.Output.VoiceType == "" {
		return fmt.Errorf("only definitions with an explicit audio.output voice and voice_type are supported")
	}
	if d.OutputModalities != nil && (len(d.OutputModalities) != 1 || d.OutputModalities[0] != "audio") {
		return fmt.Errorf("only the audio output modality is supported")
	}
	return nil
}

func (m voiceAgentSettings) definition() voiceAgentDefinition {
	return voiceAgentDefinition{
		Kind: "voice", SubProtocol: "realtime",
		Model: m.Model.ValueString(), ModelType: m.ModelType.ValueString(),
		Instructions:     m.Instructions.ValueStringPointer(),
		Store:            m.Store.ValueBoolPointer(),
		OutputModalities: []string{"audio"},
		Audio: &voiceAgentAudio{Output: &voiceAgentOutput{
			Voice: m.Voice.ValueString(), VoiceType: m.VoiceType.ValueString(),
		}},
	}
}

func (m *voiceAgentSettings) apply(definition voiceAgentDefinition) {
	m.Model = optionalString(definition.Model)
	m.ModelType = optionalString(definition.ModelType)
	m.Instructions = types.StringPointerValue(definition.Instructions)
	m.Store = types.BoolValue(definition.Store != nil && *definition.Store)
	m.Voice = types.StringNull()
	m.VoiceType = types.StringNull()
	if definition.Audio != nil && definition.Audio.Output != nil {
		m.Voice = optionalString(definition.Audio.Output.Voice)
		m.VoiceType = optionalString(definition.Audio.Output.VoiceType)
	}
}

func (r *voiceAgentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_voice_agent"
}

func (r *voiceAgentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attributes := agentCommonSchema()
	attributes["draft"] = schema.BoolAttribute{
		Computed:            true,
		MarkdownDescription: "Whether the version is a draft. This resource publishes numbered versions only.",
	}
	attributes["model"] = schema.StringAttribute{
		Required: true, MarkdownDescription: voiceModelDescription,
		Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
	}
	attributes["model_type"] = schema.StringAttribute{
		Required: true, MarkdownDescription: voiceModelTypeDescription,
		Validators: []validator.String{stringvalidator.OneOf("managed", "self_deployed")},
	}
	attributes["instructions"] = schema.StringAttribute{
		Optional: true, MarkdownDescription: voiceInstructionsDescription,
	}
	attributes["store"] = schema.BoolAttribute{
		Optional: true, Computed: true, Default: booldefault.StaticBool(false), MarkdownDescription: voiceStoreDescription,
	}
	attributes["voice"] = schema.StringAttribute{
		Required: true, MarkdownDescription: voiceNameDescription,
		Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
	}
	attributes["voice_type"] = schema.StringAttribute{
		Required: true, MarkdownDescription: voiceTypeDescription,
		Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
	}

	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a model-backed realtime voice agent with audio output. Changes publish a new immutable version. The resource refuses to manage definitions with fields it cannot write back, including fields nested under `audio`. It does not start sessions or bind phone numbers. Destroy deletes the agent and its versions.",
		Attributes:          attributes,
	}
}

func (r *voiceAgentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (r *voiceAgentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan voiceAgentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name := plan.Name.ValueString()
	// The version endpoint also publishes to an existing agent. Do not adopt
	// one implicitly, since destroy would then delete its version history.
	_, _, err := readAgent(ctx, r.client, name)
	if err == nil {
		resp.Diagnostics.AddError("Voice agent already exists", "Import the existing agent by name before managing it.")
		return
	}
	if !clients.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to check for an existing voice agent", err.Error())
		return
	}
	r.publish(ctx, &plan, &resp.State, &resp.Diagnostics)
}

func (r *voiceAgentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state voiceAgentModel
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
		resp.Diagnostics.AddError("Unable to read voice agent", err.Error())
		return
	}
	state.apply(ctx, version, endpoint, &resp.State, &resp.Diagnostics)
}

func (r *voiceAgentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan voiceAgentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || !validateAgentBeforeUpdate(ctx, r.client, plan.Name.ValueString(), &managedVoiceAgentDefinition{}, &resp.Diagnostics) {
		return
	}
	r.publish(ctx, &plan, &resp.State, &resp.Diagnostics)
}

func (r *voiceAgentResource) publish(ctx context.Context, plan *voiceAgentModel, state *tfsdk.State, diagnostics *diag.Diagnostics) {
	name := plan.Name.ValueString()
	// SDK create_version uses this same route for the first and later versions.
	version, err := updateAgent(ctx, r.client, name, plan.Description.ValueString(), plan.definition(), false)
	if err != nil {
		diagnostics.AddError("Unable to publish voice agent version", err.Error())
		return
	}
	_, endpoint, err := readAgent(ctx, r.client, name)
	if err != nil {
		diagnostics.AddError("Unable to read voice agent endpoint", err.Error())
		return
	}
	plan.apply(ctx, version, endpoint, state, diagnostics)
}

func (m *voiceAgentModel) apply(ctx context.Context, version agentVersion, endpoint *agentEndpoint, state *tfsdk.State, diagnostics *diag.Diagnostics) {
	var definition managedVoiceAgentDefinition
	if !decodeManagedDefinition(version, &definition, diagnostics) {
		return
	}
	m.applyVersion(version)
	m.applyEndpoint(ctx, endpoint, diagnostics)
	m.voiceAgentSettings.apply(definition.voiceAgentDefinition)
	diagnostics.Append(state.Set(ctx, m)...)
}

func (r *voiceAgentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state voiceAgentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name := state.Name.ValueString()
	version, _, err := readAgent(ctx, r.client, name)
	if clients.IsNotFound(err) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read voice agent before deletion", err.Error())
		return
	}
	if !decodeDefinition(version, &voiceAgentDefinition{}, &resp.Diagnostics) {
		return
	}
	if err := deleteAgent(ctx, r.client, name); err != nil {
		resp.Diagnostics.AddError("Unable to delete voice agent", err.Error())
	}
}

func (r *voiceAgentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}
