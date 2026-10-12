package provider

import (
	"context"
	"net/http"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
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
	Name          types.String `tfsdk:"name"`
	ID            types.String `tfsdk:"id"`
	Version       types.String `tfsdk:"version"`
	ActiveVersion types.String `tfsdk:"active_version"`
	AgentEndpoint types.Object `tfsdk:"agent_endpoint"`
	Description   types.String `tfsdk:"description"`
	Metadata      types.Map    `tfsdk:"metadata"`
	Status        types.String `tfsdk:"status"`
	CreatedAt     types.Int64  `tfsdk:"created_at"`
	AgentGUID     types.String `tfsdk:"agent_guid"`
	PrincipalID   types.String `tfsdk:"principal_id"`
	ClientID      types.String `tfsdk:"client_id"`
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
			MarkdownDescription: "Identifier of the selected agent version.",
		},
		"version": schema.StringAttribute{
			Optional:            true,
			Computed:            true,
			MarkdownDescription: "Agent version to look up. Omit to read the latest version, regardless of endpoint routing.",
			Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
		},
		"active_version": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Version receiving all stable-endpoint traffic, independent of the selected definition version. Resolves `@latest` to the latest version number. Null when routing is absent, split across rules, or not a single 100% `FixedRatio` rule.",
		},
		"agent_endpoint": schema.SingleNestedAttribute{
			Computed:            true,
			MarkdownDescription: "The agent's stable-endpoint protocols and traffic routing, independent of the selected definition version.",
			Attributes: map[string]schema.Attribute{
				"protocols": schema.ListAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "Protocols the endpoint serves."},
				"version_selection_rules": schema.ListNestedAttribute{
					Computed:            true,
					MarkdownDescription: "All routing rules returned by the service, including their traffic percentages.",
					NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
						"type":               schema.StringAttribute{Computed: true, MarkdownDescription: "Routing rule type, such as `FixedRatio`."},
						"agent_version":      schema.StringAttribute{Computed: true, MarkdownDescription: "Target version or `@latest`."},
						"traffic_percentage": schema.Int64Attribute{Computed: true, MarkdownDescription: "Percentage of traffic routed to this version."},
					}},
				},
			},
		},
		"description": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Description of the selected agent version.",
		},
		"metadata": schema.MapAttribute{
			Computed:            true,
			ElementType:         types.StringType,
			MarkdownDescription: "Metadata attached to the selected agent version.",
		},
		"status": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Provisioning status of the selected agent version.",
		},
		"created_at": schema.Int64Attribute{
			Computed:            true,
			MarkdownDescription: "Unix timestamp when the selected agent version was created.",
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

	description := "Looks up a " + d.kind + " agent by name and optional version. Omitting `version` reads the latest definition, not necessarily the version receiving traffic. Endpoint routing and `active_version` always describe the agent's stable endpoint."
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
		version, agent, ok := d.read(ctx, config.Name.ValueString(), config.Version, &definition, &resp.Diagnostics)
		if !ok {
			return
		}
		config.apply(ctx, version, definition, &resp.Diagnostics)
		config.applyRouting(ctx, agent, &resp.Diagnostics)
		resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
	case "hosted":
		var config hostedAgentDataSourceModel
		resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
		if resp.Diagnostics.HasError() {
			return
		}
		var definition hostedAgentDefinition
		version, agent, ok := d.read(ctx, config.Name.ValueString(), config.Version, &definition, &resp.Diagnostics)
		if !ok {
			return
		}
		config.apply(ctx, version, definition, &resp.Diagnostics)
		config.applyRouting(ctx, agent, &resp.Diagnostics)
		resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
	case "external":
		var config externalAgentDataSourceModel
		resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
		if resp.Diagnostics.HasError() {
			return
		}
		var definition externalAgentDefinition
		version, agent, ok := d.read(ctx, config.Name.ValueString(), config.Version, &definition, &resp.Diagnostics)
		if !ok {
			return
		}
		config.apply(ctx, version, definition, &resp.Diagnostics)
		config.applyRouting(ctx, agent, &resp.Diagnostics)
		resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
	}
}

func (d *agentDataSource) read(ctx context.Context, name string, selectedVersion types.String, definition supportedAgentDefinition, diagnostics *diag.Diagnostics) (agentVersion, agentResponse, bool) {
	if selectedVersion.IsUnknown() || (!selectedVersion.IsNull() && selectedVersion.ValueString() == "") {
		diagnostics.AddError("Invalid agent version", "The version must be known and nonempty, or omitted to select the latest version.")
		return agentVersion{}, agentResponse{}, false
	}
	if d.kind == "external" {
		var err error
		ctx, err = d.previewContext(ctx)
		if err != nil {
			diagnostics.AddError("Unable to read external agent", err.Error())
			return agentVersion{}, agentResponse{}, false
		}
	}
	var agent agentResponse
	err := d.client.JSON(ctx, http.MethodGet, "agents/"+url.PathEscape(name), nil, &agent)
	if err != nil {
		diagnostics.AddError("Unable to read "+d.kind+" agent", err.Error())
		return agentVersion{}, agentResponse{}, false
	}
	version := agent.latestVersion()
	if !selectedVersion.IsNull() {
		version = agentVersion{}
		err = d.client.JSON(ctx, http.MethodGet, "agents/"+url.PathEscape(name)+"/versions/"+url.PathEscape(selectedVersion.ValueString()), nil, &version)
		if err != nil {
			diagnostics.AddError("Unable to read "+d.kind+" agent version", err.Error())
			return agentVersion{}, agentResponse{}, false
		}
		if version.Version != selectedVersion.ValueString() {
			diagnostics.AddError("Unexpected agent version", "The service returned a different version from the one requested.")
			return agentVersion{}, agentResponse{}, false
		}
		if version.Name == "" {
			version.Name = name
		}
	}
	return version, agent, decodeDefinition(version, definition, diagnostics)
}

func (m *agentDataSourceCommon) applyRouting(ctx context.Context, agent agentResponse, diagnostics *diag.Diagnostics) {
	m.AgentEndpoint = agentEndpointValue(ctx, agent.AgentEndpoint, diagnostics)
	m.ActiveVersion = activeAgentVersion(agent.Versions.Latest.Version, agent.AgentEndpoint)
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
