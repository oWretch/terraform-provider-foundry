package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

var (
	_ resource.Resource                   = &hostedAgentResource{}
	_ resource.ResourceWithConfigure      = &hostedAgentResource{}
	_ resource.ResourceWithImportState    = &hostedAgentResource{}
	_ resource.ResourceWithValidateConfig = &hostedAgentResource{}
)

func NewHostedAgentResource() resource.Resource {
	return &hostedAgentResource{}
}

type hostedAgentResource struct {
	client *clients.Client
}

type hostedAgentModel struct {
	agentCommon
	Image                types.String           `tfsdk:"image"`
	RegistryConnectionID types.String           `tfsdk:"registry_connection_id"`
	CPU                  types.String           `tfsdk:"cpu"`
	Memory               types.String           `tfsdk:"memory"`
	EnvironmentVariables types.Map              `tfsdk:"environment_variables"`
	ProtocolVersions     []protocolVersionModel `tfsdk:"protocol_versions"`
	RAIConfig            types.Object           `tfsdk:"rai_config"`
}

type protocolVersionModel struct {
	Protocol types.String `tfsdk:"protocol"`
	Version  types.String `tfsdk:"version"`
}

type hostedAgentDefinition struct {
	Kind                   string                  `json:"kind"`
	CPU                    string                  `json:"cpu"`
	Memory                 string                  `json:"memory"`
	EnvironmentVariables   map[string]string       `json:"environment_variables,omitempty"`
	ContainerConfiguration *containerConfiguration `json:"container_configuration,omitempty"`
	ProtocolVersions       []protocolVersion       `json:"protocol_versions,omitempty"`
	CodeConfiguration      json.RawMessage         `json:"code_configuration,omitempty"`
	TelemetryConfig        json.RawMessage         `json:"telemetry_config,omitempty"`
	RAIConfig              *raiConfig              `json:"rai_config,omitempty"`
}

type containerConfiguration struct {
	Image                string `json:"image"`
	RegistryConnectionID string `json:"registry_connection_id,omitempty"`
}

type protocolVersion struct {
	Protocol string `json:"protocol"`
	Version  string `json:"version"`
}

func (r *hostedAgentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_hosted_agent"
}

func (r *hostedAgentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attributes := agentCommonSchema()
	attributes["image"] = schema.StringAttribute{
		Required:            true,
		MarkdownDescription: "Container image in `<registry>/<repository>[:<tag>|@<digest>]` form.",
	}
	attributes["registry_connection_id"] = schema.StringAttribute{
		Optional:            true,
		MarkdownDescription: "Foundry project connection used to authenticate to the container registry.",
	}
	attributes["cpu"] = schema.StringAttribute{
		Required:            true,
		MarkdownDescription: "CPU cores allocated to the container, such as `1`.",
	}
	attributes["memory"] = schema.StringAttribute{
		Required:            true,
		MarkdownDescription: "Memory allocated to the container, such as `2Gi`.",
	}
	attributes["environment_variables"] = schema.MapAttribute{
		Optional:            true,
		ElementType:         types.StringType,
		MarkdownDescription: "Environment variables passed to the container. Do not place secrets here, because Terraform stores them in state.",
	}
	attributes["protocol_versions"] = schema.ListNestedAttribute{
		Optional:            true,
		MarkdownDescription: "Protocols the container implements.",
		Validators:          []validator.List{listvalidator.SizeAtLeast(1)},
		NestedObject: schema.NestedAttributeObject{
			Attributes: map[string]schema.Attribute{
				"protocol": schema.StringAttribute{
					Required:            true,
					MarkdownDescription: "Protocol name, such as `responses`.",
				},
				"version": schema.StringAttribute{
					Required:            true,
					MarkdownDescription: "Protocol version implemented by the container.",
				},
			},
		},
	}
	attributes["rai_config"] = schema.SingleNestedAttribute{
		Optional:            true,
		MarkdownDescription: "Responsible AI policy applied to the agent.",
		Attributes: map[string]schema.Attribute{
			"rai_policy_name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the Responsible AI policy.",
			},
		},
	}

	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a hosted agent, which runs a container image supplied by you. Changing the definition publishes a new agent version.",
		Attributes:          attributes,
	}
}

func (r *hostedAgentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (r *hostedAgentResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config hostedAgentModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	validateDraftPreview(r.client, config.Draft, &resp.Diagnostics)
}

func (m hostedAgentModel) definition(ctx context.Context, diagnostics *diag.Diagnostics) hostedAgentDefinition {
	definition := hostedAgentDefinition{
		Kind:   "hosted",
		CPU:    m.CPU.ValueString(),
		Memory: m.Memory.ValueString(),
		ContainerConfiguration: &containerConfiguration{
			Image:                m.Image.ValueString(),
			RegistryConnectionID: m.RegistryConnectionID.ValueString(),
		},
		RAIConfig: raiConfigDefinition(m.RAIConfig),
	}
	if !m.EnvironmentVariables.IsNull() {
		diagnostics.Append(m.EnvironmentVariables.ElementsAs(ctx, &definition.EnvironmentVariables, false)...)
	}
	for _, item := range m.ProtocolVersions {
		definition.ProtocolVersions = append(definition.ProtocolVersions, protocolVersion{
			Protocol: item.Protocol.ValueString(),
			Version:  item.Version.ValueString(),
		})
	}
	return definition
}

func (*hostedAgentDefinition) expectedKind() string {
	return "hosted"
}

func (d *hostedAgentDefinition) validateSupported() error {
	var fields []string
	if hasJSONValue(d.CodeConfiguration) {
		fields = append(fields, "code_configuration")
	}
	if hasJSONValue(d.TelemetryConfig) {
		fields = append(fields, "telemetry_config")
	}
	if len(fields) > 0 {
		return fmt.Errorf(
			"the service returned unsupported fields (%s); only image-based container hosted agents are supported",
			strings.Join(fields, ", "),
		)
	}
	if d.ContainerConfiguration == nil || d.ContainerConfiguration.Image == "" {
		return fmt.Errorf("only image-based hosted agents with container_configuration.image are supported")
	}
	return nil
}

func (m *hostedAgentModel) apply(ctx context.Context, version agentVersion, endpoint *agentEndpoint, definition hostedAgentDefinition, diagnostics *diag.Diagnostics) {
	m.applyVersion(version)
	m.applyEndpoint(ctx, endpoint, diagnostics)
	m.Image = types.StringValue(definition.ContainerConfiguration.Image)
	m.RegistryConnectionID = optionalString(definition.ContainerConfiguration.RegistryConnectionID)
	m.CPU = types.StringValue(definition.CPU)
	m.Memory = types.StringValue(definition.Memory)
	m.RAIConfig = raiConfigValue(definition.RAIConfig, diagnostics)

	if len(definition.EnvironmentVariables) == 0 {
		m.EnvironmentVariables = types.MapNull(types.StringType)
	} else {
		value, diags := types.MapValueFrom(ctx, types.StringType, definition.EnvironmentVariables)
		diagnostics.Append(diags...)
		m.EnvironmentVariables = value
	}

	if definition.ProtocolVersions == nil {
		m.ProtocolVersions = nil
		return
	}
	m.ProtocolVersions = make([]protocolVersionModel, 0, len(definition.ProtocolVersions))
	for _, item := range definition.ProtocolVersions {
		m.ProtocolVersions = append(m.ProtocolVersions, protocolVersionModel{
			Protocol: types.StringValue(item.Protocol),
			Version:  types.StringValue(item.Version),
		})
	}
}

func (r *hostedAgentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan hostedAgentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	version, endpoint, err := createAgent(ctx, r.client, plan.Name.ValueString(), plan.Description.ValueString(), plan.definition(ctx, &resp.Diagnostics), plan.Draft.ValueBool())
	if err != nil {
		resp.Diagnostics.AddError("Unable to create hosted agent", err.Error())
		return
	}

	var definition hostedAgentDefinition
	if !decodeManagedDefinition(version, &definition, &resp.Diagnostics) {
		return
	}
	plan.apply(ctx, version, endpoint, definition, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *hostedAgentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state hostedAgentModel
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
		resp.Diagnostics.AddError("Unable to read hosted agent", err.Error())
		return
	}

	var definition hostedAgentDefinition
	if !decodeManagedDefinition(version, &definition, &resp.Diagnostics) {
		return
	}
	state.apply(ctx, version, endpoint, definition, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *hostedAgentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan hostedAgentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var current hostedAgentDefinition
	if !validateAgentBeforeUpdate(ctx, r.client, plan.Name.ValueString(), &current, &resp.Diagnostics) {
		return
	}

	version, err := updateAgent(ctx, r.client, plan.Name.ValueString(), plan.Description.ValueString(), plan.definition(ctx, &resp.Diagnostics), plan.Draft.ValueBool())
	if err != nil {
		resp.Diagnostics.AddError("Unable to update hosted agent", err.Error())
		return
	}

	var definition hostedAgentDefinition
	if !decodeManagedDefinition(version, &definition, &resp.Diagnostics) {
		return
	}
	// updateAgent does not return endpoint metadata, so re-read it to keep the
	// computed agent_endpoint attribute accurate after publishing a new version.
	_, endpoint, err := readAgent(ctx, r.client, plan.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read hosted agent endpoint", err.Error())
		return
	}
	plan.apply(ctx, version, endpoint, definition, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *hostedAgentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state hostedAgentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := deleteAgent(ctx, r.client, state.Name.ValueString()); err != nil {
		resp.Diagnostics.AddError("Unable to delete hosted agent", err.Error())
	}
}

func (r *hostedAgentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}
