package provider

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

var (
	_ datasource.DataSource                   = &toolboxDataSource{}
	_ datasource.DataSourceWithConfigure      = &toolboxDataSource{}
	_ datasource.DataSourceWithValidateConfig = &toolboxDataSource{}
)

func NewToolboxDataSource() datasource.DataSource {
	return &toolboxDataSource{previewGate: previewGate{feature: toolboxesFeature, name: toolboxesFeatureName}}
}

// toolboxDataSource is a read-only lookup of a single toolbox version's
// metadata. foundry_toolbox owns creation and promotion; this data source
// only reads, so it cannot race with the resource for ownership of the
// version chain.
type toolboxDataSource struct {
	previewGate
}

type toolboxDataSourceModel struct {
	Name        types.String                 `tfsdk:"name"`
	Version     types.String                 `tfsdk:"version"`
	ID          types.String                 `tfsdk:"id"`
	Description types.String                 `tfsdk:"description"`
	Tools       types.String                 `tfsdk:"tools_json"`
	Skills      []toolboxSkillReferenceModel `tfsdk:"skills"`
}

func (d *toolboxDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_toolbox"
}

func (d *toolboxDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "~> **Preview:** This data source requires `\"toolboxes\"` in the provider's `enable_preview` attribute. Preview features may change their inputs, outputs, or behavior in any provider release without following semantic versioning.\n\n" +
			"Looks up a single version of a Foundry toolbox. Use this to reference an existing toolbox version's tool and skill configuration.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the toolbox to look up.",
			},
			"version": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Version of the toolbox to look up. Use the `foundry_toolbox` resource's `default_version` attribute to look up the currently active version.",
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Identifier of the toolbox version.",
			},
			"description": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Description recorded for this toolbox version.",
			},
			"tools_json": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "Tools in this toolbox version, as a JSON-encoded array. Tools are exposed as JSON rather than as typed attributes " +
					"because a data source only reports what the service returns; use `jsondecode` to inspect individual tools.",
			},
			"skills": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Skills attached to this toolbox version.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Name of the referenced skill.",
						},
						"version": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Pinned skill version, or null when the reference follows the skill's default version.",
						},
					},
				},
			},
		},
	}
}

func (d *toolboxDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

// ValidateConfig delegates to previewGate. Go's embedding does not promote
// previewGate.ValidateDataSourceConfig to satisfy
// datasource.DataSourceWithValidateConfig on *toolboxDataSource
// automatically, so this thin wrapper keeps the interface satisfied
// explicitly.
func (d *toolboxDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	d.ValidateDataSourceConfig(ctx, req, resp)
}

func (d *toolboxDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config toolboxDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	previewCtx, err := d.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read toolbox version", err.Error())
		return
	}

	var version toolboxVersionResponse
	err = d.client.JSON(previewCtx, http.MethodGet, versionPath("toolboxes", config.Name.ValueString(), config.Version.ValueString()), nil, &version)
	if clients.IsNotFound(err) {
		resp.Diagnostics.AddError("Toolbox version not found", "No toolbox version matched the given name and version.")
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read toolbox version", err.Error())
		return
	}

	config.ID = types.StringValue(version.ID)
	config.Description = optionalString(version.Description)
	encoded, marshalErr := json.Marshal(version.Tools)
	if marshalErr != nil {
		resp.Diagnostics.AddError("Unable to read toolbox tools", marshalErr.Error())
		return
	}
	config.Tools = types.StringValue(string(encoded))
	if len(version.Skills) == 0 {
		config.Skills = nil
	} else {
		skills := make([]toolboxSkillReferenceModel, 0, len(version.Skills))
		for _, skill := range version.Skills {
			skills = append(skills, toolboxSkillReferenceModel{
				Name:    types.StringValue(skill.Name),
				Version: optionalString(skill.Version),
			})
		}
		config.Skills = skills
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
