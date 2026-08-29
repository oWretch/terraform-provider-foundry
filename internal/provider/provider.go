package provider

import (
	"context"
	"fmt"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

const typeName = "foundry"

var _ provider.Provider = &foundryProvider{}

type foundryProvider struct {
	version string
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &foundryProvider{version: version}
	}
}

func (p *foundryProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = typeName
	resp.Version = p.version
}

func (p *foundryProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The Foundry provider manages resources in the current Microsoft Foundry Agent Service.",
		Attributes: map[string]schema.Attribute{
			"account_name": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Foundry account name. Can also be set with `FOUNDRY_ACCOUNT_NAME`.",
			},
			"project_name": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Foundry project name. Can also be set with `FOUNDRY_PROJECT_NAME`.",
			},
			"environment": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Azure environment. Supported values are `public`, `usgovernment`, and `china`. Defaults to `public` or `FOUNDRY_ENVIRONMENT` when set.",
			},
			"api_key": schema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "Foundry API key. Can also be set with `FOUNDRY_API_KEY`. API key authentication cannot be combined with Microsoft Entra ID authentication.",
			},
			"tenant_id": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Microsoft Entra tenant ID. Can also be set with `ARM_TENANT_ID`.",
			},
			"client_id": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Application or user-assigned managed identity client ID. Can also be set with `ARM_CLIENT_ID`.",
			},
			"client_secret": schema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "Service principal client secret. Can also be set with `ARM_CLIENT_SECRET`.",
			},
			"client_certificate_path": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Path to a PEM or PKCS#12 service principal certificate. Can also be set with `ARM_CLIENT_CERTIFICATE_PATH`.",
			},
			"client_certificate_password": schema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "Password for the service principal certificate. Can also be set with `ARM_CLIENT_CERTIFICATE_PASSWORD`.",
			},
			"use_oidc": schema.BoolAttribute{
				Optional:            true,
				MarkdownDescription: "Use workload identity federation. Can also be set with `ARM_USE_OIDC`.",
			},
			"oidc_token_file_path": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Path to the workload identity token file. Can also be set with `ARM_OIDC_TOKEN_FILE_PATH` or `AZURE_FEDERATED_TOKEN_FILE`.",
			},
			"use_msi": schema.BoolAttribute{
				Optional:            true,
				MarkdownDescription: "Use a system-assigned or user-assigned managed identity. Can also be set with `ARM_USE_MSI`.",
			},
			"use_cli": schema.BoolAttribute{
				Optional:            true,
				MarkdownDescription: "Use the signed-in Azure CLI account. Defaults to `true` when no other authentication mode is configured. Can also be set with `ARM_USE_CLI`.",
			},
			"enable_preview": schema.SetAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Opts in to preview Foundry resources and data sources, individually by name (for example `[\"evaluations\", \"memory_stores\"]`). Preview features may change their inputs, outputs, or behavior in any provider release without following semantic versioning. See the provider documentation for the list of preview feature names and the resources each one gates.",
			},
		},
	}
}

func (p *foundryProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var model providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	config, err := resolveConfig(model, os.LookupEnv)
	if err != nil {
		resp.Diagnostics.AddError("Invalid provider configuration", err.Error())
		return
	}

	config.UserAgent = fmt.Sprintf("terraform-provider-foundry/%s", p.version)
	client, err := clients.New(config)
	if err != nil {
		resp.Diagnostics.AddError("Unable to configure Foundry client", err.Error())
		return
	}

	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *foundryProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewPromptAgentResource,
		NewHostedAgentResource,
		NewExternalAgentResource,
		NewFileResource,
		NewVectorStoreResource,
		NewVectorStoreFileResource,
		NewDatasetResource,
		NewIndexResource,
		NewMemoryStoreResource,
		NewRoutineResource,
		NewScheduleResource,
		NewSkillResource,
		NewToolboxResource,
		NewEvaluatorVersionResource,
		NewEvaluationTaxonomyResource,
		NewEvaluationResource,
		NewEvaluationRuleResource,
	}
}

func (p *foundryProvider) DataSources(context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewDeploymentsDataSource,
		NewConnectionsDataSource,
		NewRoutineDataSource,
		NewScheduleDataSource,
		NewConnectionDataSource,
		NewSkillDataSource,
		NewToolboxDataSource,
		NewEvaluatorVersionDataSource,
		NewEvaluationTaxonomyDataSource,
		NewEvaluationDataSource,
		NewEvaluationRuleDataSource,
	}
}
