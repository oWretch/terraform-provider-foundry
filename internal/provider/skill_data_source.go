package provider

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

var (
	_ datasource.DataSource                   = &skillDataSource{}
	_ datasource.DataSourceWithConfigure      = &skillDataSource{}
	_ datasource.DataSourceWithValidateConfig = &skillDataSource{}
)

func NewSkillDataSource() datasource.DataSource {
	return &skillDataSource{previewGate: previewGate{feature: skillsFeature, name: skillsFeatureName}}
}

// skillDataSource is a read-only lookup of a single skill version's
// metadata. foundry_skill owns creation and promotion; this data source only
// reads, so it cannot race with the resource for ownership of the version
// chain.
type skillDataSource struct {
	previewGate
}

type skillDataSourceModel struct {
	Name        types.String `tfsdk:"name"`
	Version     types.String `tfsdk:"version"`
	ID          types.String `tfsdk:"id"`
	SkillID     types.String `tfsdk:"skill_id"`
	Description types.String `tfsdk:"description"`
}

func (d *skillDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_skill"
}

func (d *skillDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "~> **Preview:** This data source requires `\"skills\"` in the provider's `enable_preview` attribute. Preview features may change their inputs, outputs, or behavior in any provider release without following semantic versioning.\n\n" +
			"Looks up a single version of a Foundry skill. Use this to reference an existing skill version's metadata, such as when pinning a toolbox's `skills` reference to an immutable version.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the skill to look up.",
			},
			"version": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Version of the skill to look up. Use the `foundry_skill` resource's `default_version` attribute to look up the currently active version.",
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Identifier of the skill version.",
			},
			"skill_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Service-assigned identifier of the parent skill.",
			},
			"description": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Description recorded for this skill version.",
			},
		},
	}
}

func (d *skillDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

// ValidateConfig delegates to previewGate. Go's embedding does not promote
// previewGate.ValidateDataSourceConfig to satisfy
// datasource.DataSourceWithValidateConfig on *skillDataSource automatically,
// so this thin wrapper keeps the interface satisfied explicitly.
func (d *skillDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	d.ValidateDataSourceConfig(ctx, req, resp)
}

func (d *skillDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config skillDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	previewCtx, err := d.previewContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read skill version", err.Error())
		return
	}

	var version skillVersionResponse
	err = d.client.JSON(previewCtx, http.MethodGet, versionPath("skills", config.Name.ValueString(), config.Version.ValueString()), nil, &version)
	if clients.IsNotFound(err) {
		resp.Diagnostics.AddError("Skill version not found", "No skill version matched the given name and version.")
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read skill version", err.Error())
		return
	}

	config.ID = types.StringValue(version.ID)
	config.SkillID = types.StringValue(version.SkillID)
	config.Description = optionalString(version.Description)
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
