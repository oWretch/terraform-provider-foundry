package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

var (
	_ resource.Resource                   = &externalAgentResource{}
	_ resource.ResourceWithConfigure      = &externalAgentResource{}
	_ resource.ResourceWithImportState    = &externalAgentResource{}
	_ resource.ResourceWithValidateConfig = &externalAgentResource{}
)

// NewExternalAgentResource registers an agent whose runtime lives outside
// Foundry. Foundry never hosts or invokes it, it only stores registration
// metadata and, when the caller emits OpenTelemetry traces tagged with
// otel_agent_id, correlates them for observability and evaluation.
func NewExternalAgentResource() resource.Resource {
	return &externalAgentResource{}
}

type externalAgentResource struct {
	previewGate
}

type externalAgentModel struct {
	agentCommon
	Endpoint    types.String `tfsdk:"endpoint"`
	OtelAgentID types.String `tfsdk:"otel_agent_id"`
}

// externalAgentDefinition is the service representation of an external agent.
// Verified live: creating an external agent without an explicit
// otel_agent_id defaults it to the agent name, so this provider always sends
// it explicitly to keep state deterministic.
type externalAgentDefinition struct {
	Kind        string `json:"kind"`
	Endpoint    string `json:"endpoint"`
	OtelAgentID string `json:"otel_agent_id,omitempty"`
}

func (m externalAgentModel) definition() externalAgentDefinition {
	definition := externalAgentDefinition{
		Kind:     "external",
		Endpoint: m.Endpoint.ValueString(),
	}
	if !m.OtelAgentID.IsNull() && !m.OtelAgentID.IsUnknown() {
		definition.OtelAgentID = m.OtelAgentID.ValueString()
	} else {
		definition.OtelAgentID = m.Name.ValueString()
	}
	return definition
}

func (m *externalAgentModel) apply(ctx context.Context, version agentVersion, endpoint *agentEndpoint, definition externalAgentDefinition, diagnostics *diag.Diagnostics) {
	m.applyVersion(version)
	m.applyEndpoint(ctx, endpoint, diagnostics)
	m.Endpoint = types.StringValue(definition.Endpoint)
	m.OtelAgentID = optionalString(definition.OtelAgentID)
}

func (r *externalAgentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_external_agent"
}

func (r *externalAgentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attributes := agentCommonSchema()
	attributes["endpoint"] = schema.StringAttribute{
		Required:            true,
		MarkdownDescription: "URL of the externally hosted agent. Foundry stores this for reference only, it never calls this endpoint.",
	}
	attributes["otel_agent_id"] = schema.StringAttribute{
		Optional:            true,
		Computed:            true,
		MarkdownDescription: "Identifier the external agent tags its OpenTelemetry traces with so Foundry can correlate them. Defaults to the agent name.",
	}

	resp.Schema = schema.Schema{
		MarkdownDescription: "~> **Preview:** requires `enable_preview` to include `\"external_agents\"`. Preview features may change in any provider release without following semantic versioning.\n\n" +
			"Registers an external agent, one whose runtime is hosted outside Foundry (any cloud, on-premises, or another provider). Foundry stores only registration metadata for observability, tracing, and evaluation, it never hosts, proxies, or invokes the agent. Changing the definition publishes a new agent version.",
		Attributes: attributes,
	}
}

func (r *externalAgentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
	r.feature = "ExternalAgents"
	r.name = "external_agents"
}

func (r *externalAgentResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	r.ValidateResourceConfig(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		return
	}
	var config externalAgentModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	validateDraftPreview(r.client, config.Draft, &resp.Diagnostics)
}

func (r *externalAgentResource) withPreviewContext(ctx context.Context, diagnostics *diag.Diagnostics) context.Context {
	previewCtx, err := r.previewContext(ctx)
	if err != nil {
		diagnostics.AddError("Preview feature not enabled", err.Error())
		return ctx
	}
	return previewCtx
}

func (r *externalAgentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan externalAgentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx = r.withPreviewContext(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	version, endpoint, err := createAgent(ctx, r.client, plan.Name.ValueString(), plan.Description.ValueString(), plan.definition(), plan.Draft.ValueBool())
	if err != nil {
		resp.Diagnostics.AddError("Unable to create external agent", err.Error())
		return
	}

	var definition externalAgentDefinition
	if !decodeDefinition(version, &definition, &resp.Diagnostics) {
		return
	}
	plan.apply(ctx, version, endpoint, definition, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *externalAgentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state externalAgentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx = r.withPreviewContext(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	version, endpoint, err := readAgent(ctx, r.client, state.Name.ValueString())
	if clients.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read external agent", err.Error())
		return
	}

	var definition externalAgentDefinition
	if !decodeDefinition(version, &definition, &resp.Diagnostics) {
		return
	}
	state.apply(ctx, version, endpoint, definition, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *externalAgentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan externalAgentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx = r.withPreviewContext(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	version, err := updateAgent(ctx, r.client, plan.Name.ValueString(), plan.Description.ValueString(), plan.definition(), plan.Draft.ValueBool())
	if err != nil {
		resp.Diagnostics.AddError("Unable to update external agent", err.Error())
		return
	}

	var definition externalAgentDefinition
	if !decodeDefinition(version, &definition, &resp.Diagnostics) {
		return
	}
	// updateAgent does not return endpoint metadata, so re-read it to keep the
	// computed agent_endpoint attribute accurate after publishing a new version.
	_, endpoint, err := readAgent(ctx, r.client, plan.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read external agent endpoint", err.Error())
		return
	}
	plan.apply(ctx, version, endpoint, definition, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *externalAgentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state externalAgentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ctx = r.withPreviewContext(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := deleteAgent(ctx, r.client, state.Name.ValueString()); err != nil {
		resp.Diagnostics.AddError("Unable to delete external agent", err.Error())
	}
}

func (r *externalAgentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}
