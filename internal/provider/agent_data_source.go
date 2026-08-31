package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource                   = &agentDataSource{}
	_ datasource.DataSourceWithConfigure      = &agentDataSource{}
	_ datasource.DataSourceWithValidateConfig = &agentDataSource{}
)

func NewPromptAgentDataSource() datasource.DataSource {
	return &agentDataSource{kind: "prompt"}
}

func NewHostedAgentDataSource() datasource.DataSource {
	return &agentDataSource{kind: "hosted"}
}

func NewExternalAgentDataSource() datasource.DataSource {
	return &agentDataSource{kind: "external"}
}

type agentDataSource struct {
	previewGate
	kind string
}

type agentDataSourceCommon struct {
	Name        types.String `tfsdk:"name"`
	ID          types.String `tfsdk:"id"`
	Version     types.String `tfsdk:"version"`
	Description types.String `tfsdk:"description"`
	Metadata    types.Map    `tfsdk:"metadata"`
	Status      types.String `tfsdk:"status"`
	CreatedAt   types.Int64  `tfsdk:"created_at"`
	AgentGUID   types.String `tfsdk:"agent_guid"`
	PrincipalID types.String `tfsdk:"principal_id"`
	ClientID    types.String `tfsdk:"client_id"`
}

type promptAgentDataSourceModel struct {
	agentDataSourceCommon
	Model        types.String `tfsdk:"model"`
	Instructions types.String `tfsdk:"instructions"`
}

type hostedAgentDataSourceModel struct {
	agentDataSourceCommon
	Image                types.String           `tfsdk:"image"`
	RegistryConnectionID types.String           `tfsdk:"registry_connection_id"`
	CPU                  types.String           `tfsdk:"cpu"`
	Memory               types.String           `tfsdk:"memory"`
	EnvironmentVariables types.Map              `tfsdk:"environment_variables"`
	ProtocolVersions     []protocolVersionModel `tfsdk:"protocol_versions"`
	RAIConfig            types.Object           `tfsdk:"rai_config"`
}

type externalAgentDataSourceModel struct {
	agentDataSourceCommon
	OtelAgentID types.String `tfsdk:"otel_agent_id"`
	RAIConfig   types.Object `tfsdk:"rai_config"`
}

func (m *agentDataSourceCommon) apply(ctx context.Context, version agentVersion, diagnostics *diag.Diagnostics) {
	if version.Name != "" {
		m.Name = types.StringValue(version.Name)
	}
	m.ID = types.StringValue(version.ID)
	m.Version = types.StringValue(version.Version)
	m.Description = optionalString(version.Description)
	m.Status = optionalString(version.Status)
	m.CreatedAt = types.Int64Value(version.CreatedAt)
	m.AgentGUID = optionalString(version.AgentGUID)
	if version.Metadata == nil {
		m.Metadata = types.MapNull(types.StringType)
	} else {
		metadata, diags := types.MapValueFrom(ctx, types.StringType, version.Metadata)
		diagnostics.Append(diags...)
		m.Metadata = metadata
	}
	if version.Identity == nil {
		m.PrincipalID = types.StringNull()
		m.ClientID = types.StringNull()
	} else {
		m.PrincipalID = optionalString(version.Identity.PrincipalID)
		m.ClientID = optionalString(version.Identity.ClientID)
	}
}

func agentDataSourceCommonSchema() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"name": schema.StringAttribute{
			Required:            true,
			MarkdownDescription: "Name of the agent to look up.",
			Validators:          agentNameValidators,
		},
		"id": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Identifier of the latest agent version.",
		},
		"version": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Latest agent version.",
		},
		"description": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Description of the latest agent version.",
		},
		"metadata": schema.MapAttribute{
			Computed:            true,
			ElementType:         types.StringType,
			MarkdownDescription: "Metadata attached to the latest agent version.",
		},
		"status": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Provisioning status of the latest agent version.",
		},
		"created_at": schema.Int64Attribute{
			Computed:            true,
			MarkdownDescription: "Unix timestamp when the latest agent version was created.",
		},
		"agent_guid": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Service-assigned unique identifier for the agent.",
		},
		"principal_id": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Principal ID of the agent instance identity.",
		},
		"client_id": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Client ID of the agent instance identity.",
		},
	}
}

func (d *agentDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + d.kind + "_agent"
}

func (d *agentDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attributes := agentDataSourceCommonSchema()
	switch d.kind {
	case "prompt":
		attributes["model"] = schema.StringAttribute{Computed: true, MarkdownDescription: "Model deployment used by the agent."}
		attributes["instructions"] = schema.StringAttribute{Computed: true, MarkdownDescription: "System instructions for the agent."}
	case "hosted":
		attributes["image"] = schema.StringAttribute{Computed: true, MarkdownDescription: "Container image used by the agent."}
		attributes["registry_connection_id"] = schema.StringAttribute{Computed: true, MarkdownDescription: "Foundry connection used for container registry authentication."}
		attributes["cpu"] = schema.StringAttribute{Computed: true, MarkdownDescription: "CPU allocated to the container."}
		attributes["memory"] = schema.StringAttribute{Computed: true, MarkdownDescription: "Memory allocated to the container."}
		attributes["environment_variables"] = schema.MapAttribute{
			Computed:            true,
			ElementType:         types.StringType,
			MarkdownDescription: "Environment variables passed to the container.",
		}
		attributes["protocol_versions"] = schema.ListNestedAttribute{
			Computed:            true,
			MarkdownDescription: "Protocols implemented by the container.",
			NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
				"protocol": schema.StringAttribute{Computed: true, MarkdownDescription: "Protocol name."},
				"version":  schema.StringAttribute{Computed: true, MarkdownDescription: "Protocol version."},
			}},
		}
		attributes["rai_config"] = agentDataSourceRAIConfigSchema()
	case "external":
		attributes["otel_agent_id"] = schema.StringAttribute{Computed: true, MarkdownDescription: "OpenTelemetry agent identifier."}
		attributes["rai_config"] = agentDataSourceRAIConfigSchema()
	}

	description := "Looks up the latest version of a " + d.kind + " agent by name."
	if d.kind == "external" {
		description = "~> **Preview:** requires `enable_preview` to include `\"external_agents\"`.\n\n" + description
	}
	resp.Schema = schema.Schema{MarkdownDescription: description, Attributes: attributes}
}

func (d *agentDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
	if d.kind == "external" {
		d.feature = "ExternalAgents"
		d.name = "external_agents"
	}
}

func (d *agentDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	switch d.kind {
	case "prompt":
		var config promptAgentDataSourceModel
		resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
		if resp.Diagnostics.HasError() {
			return
		}
		var definition promptAgentDefinition
		version, ok := d.read(ctx, config.Name.ValueString(), &definition, &resp.Diagnostics)
		if !ok {
			return
		}
		config.apply(ctx, version, definition, &resp.Diagnostics)
		resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
	case "hosted":
		var config hostedAgentDataSourceModel
		resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
		if resp.Diagnostics.HasError() {
			return
		}
		var definition hostedAgentDefinition
		version, ok := d.read(ctx, config.Name.ValueString(), &definition, &resp.Diagnostics)
		if !ok {
			return
		}
		config.apply(ctx, version, definition, &resp.Diagnostics)
		resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
	case "external":
		var config externalAgentDataSourceModel
		resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
		if resp.Diagnostics.HasError() {
			return
		}
		var definition externalAgentDefinition
		version, ok := d.read(ctx, config.Name.ValueString(), &definition, &resp.Diagnostics)
		if !ok {
			return
		}
		config.apply(ctx, version, definition, &resp.Diagnostics)
		resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
	}
}

func (d *agentDataSource) read(ctx context.Context, name string, definition supportedAgentDefinition, diagnostics *diag.Diagnostics) (agentVersion, bool) {
	if d.kind == "external" {
		var err error
		ctx, err = d.previewContext(ctx)
		if err != nil {
			diagnostics.AddError("Unable to read external agent", err.Error())
			return agentVersion{}, false
		}
	}
	version, _, err := readAgent(ctx, d.client, name)
	if err != nil {
		diagnostics.AddError("Unable to read "+d.kind+" agent", err.Error())
		return agentVersion{}, false
	}
	return version, decodeDefinition(version, definition, diagnostics)
}

func (d *agentDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	if d.kind == "external" {
		d.ValidateDataSourceConfig(ctx, req, resp)
	}
}

func (m *promptAgentDataSourceModel) apply(ctx context.Context, version agentVersion, definition promptAgentDefinition, diagnostics *diag.Diagnostics) {
	m.agentDataSourceCommon.apply(ctx, version, diagnostics)
	m.Model = types.StringValue(definition.Model)
	m.Instructions = optionalString(definition.Instructions)
}

func (m *hostedAgentDataSourceModel) apply(ctx context.Context, version agentVersion, definition hostedAgentDefinition, diagnostics *diag.Diagnostics) {
	m.agentDataSourceCommon.apply(ctx, version, diagnostics)
	if definition.ContainerConfiguration == nil {
		m.Image = types.StringNull()
		m.RegistryConnectionID = types.StringNull()
	} else {
		m.Image = types.StringValue(definition.ContainerConfiguration.Image)
		m.RegistryConnectionID = optionalString(definition.ContainerConfiguration.RegistryConnectionID)
	}
	m.CPU = types.StringValue(definition.CPU)
	m.Memory = types.StringValue(definition.Memory)
	m.RAIConfig = raiConfigValue(definition.RAIConfig, diagnostics)
	if len(definition.EnvironmentVariables) == 0 {
		m.EnvironmentVariables = types.MapNull(types.StringType)
	} else {
		variables, diags := types.MapValueFrom(ctx, types.StringType, definition.EnvironmentVariables)
		diagnostics.Append(diags...)
		m.EnvironmentVariables = variables
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

func (m *externalAgentDataSourceModel) apply(ctx context.Context, version agentVersion, definition externalAgentDefinition, diagnostics *diag.Diagnostics) {
	m.agentDataSourceCommon.apply(ctx, version, diagnostics)
	m.OtelAgentID = optionalString(definition.OtelAgentID)
	m.RAIConfig = raiConfigValue(definition.RAIConfig, diagnostics)
}

func agentDataSourceRAIConfigSchema() schema.Attribute {
	return schema.SingleNestedAttribute{
		Computed:            true,
		MarkdownDescription: "Responsible AI policy applied to the agent.",
		Attributes: map[string]schema.Attribute{
			"rai_policy_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Name of the Responsible AI policy.",
			},
		},
	}
}
