package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

func TestProviderSchema(t *testing.T) {
	t.Parallel()

	var response provider.SchemaResponse
	New("test")().Schema(context.Background(), provider.SchemaRequest{}, &response)
	if response.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %v", response.Diagnostics)
	}

	for _, name := range []string{"account_name", "project_name", "environment", "api_key", "tenant_id", "client_id", "client_secret", "client_certificate_path", "client_certificate_password", "use_oidc", "oidc_token_file_path", "use_msi", "use_cli"} {
		if _, ok := response.Schema.Attributes[name]; !ok {
			t.Errorf("schema is missing %q", name)
		}
	}
	if !response.Schema.Attributes["api_key"].IsSensitive() {
		t.Error("api_key must be sensitive")
	}
	if !response.Schema.Attributes["client_secret"].IsSensitive() {
		t.Error("client_secret must be sensitive")
	}
	if !response.Schema.Attributes["client_certificate_password"].IsSensitive() {
		t.Error("client_certificate_password must be sensitive")
	}
}

func TestResolveConfig(t *testing.T) {
	t.Parallel()

	nullModel := func() providerModel {
		return providerModel{
			AccountName:               types.StringNull(),
			ProjectName:               types.StringNull(),
			Environment:               types.StringNull(),
			APIKey:                    types.StringNull(),
			TenantID:                  types.StringNull(),
			ClientID:                  types.StringNull(),
			ClientSecret:              types.StringNull(),
			ClientCertificatePath:     types.StringNull(),
			ClientCertificatePassword: types.StringNull(),
			UseOIDC:                   types.BoolNull(),
			OIDCTokenFilePath:         types.StringNull(),
			UseMSI:                    types.BoolNull(),
			UseCLI:                    types.BoolNull(),
		}
	}

	tests := map[string]struct {
		model       providerModel
		environment map[string]string
		wantMethod  clients.AuthenticationMethod
		wantAccount string
		wantProject string
		wantCloud   clients.Environment
		wantError   string
	}{
		"default environment": {
			model: func() providerModel {
				model := nullModel()
				model.AccountName = types.StringValue("example")
				model.ProjectName = types.StringValue("demo")
				model.APIKey = types.StringValue("key")
				return model
			}(),
			wantMethod:  clients.AuthenticationAPIKey,
			wantAccount: "example",
			wantProject: "demo",
			wantCloud:   clients.EnvironmentPublic,
		},
		"default Azure CLI": {
			model: func() providerModel {
				model := nullModel()
				model.AccountName = types.StringValue("example")
				model.ProjectName = types.StringValue("demo")
				return model
			}(),
			wantMethod:  clients.AuthenticationAzureCLI,
			wantAccount: "example",
			wantProject: "demo",
			wantCloud:   clients.EnvironmentPublic,
		},
		"service principal suppresses default Azure CLI": {
			model: func() providerModel {
				model := nullModel()
				model.AccountName = types.StringValue("example")
				model.ProjectName = types.StringValue("demo")
				model.TenantID = types.StringValue("tenant")
				model.ClientID = types.StringValue("client")
				model.ClientSecret = types.StringValue("secret")
				return model
			}(),
			wantMethod:  clients.AuthenticationClientSecret,
			wantAccount: "example",
			wantProject: "demo",
			wantCloud:   clients.EnvironmentPublic,
		},
		"explicitly disable default Azure CLI": {
			model: func() providerModel {
				model := nullModel()
				model.AccountName = types.StringValue("example")
				model.ProjectName = types.StringValue("demo")
				model.UseCLI = types.BoolValue(false)
				return model
			}(),
			wantError: "configure exactly one authentication mode",
		},
		"environment fallback": {
			model: nullModel(),
			environment: map[string]string{
				"FOUNDRY_ACCOUNT_NAME": "example",
				"FOUNDRY_PROJECT_NAME": "demo",
				"ARM_ENVIRONMENT":      "china",
				"ARM_USE_CLI":          "true",
			},
			wantMethod:  clients.AuthenticationAzureCLI,
			wantAccount: "example",
			wantProject: "demo",
			wantCloud:   clients.EnvironmentChina,
		},
		"provider values take precedence": {
			model: func() providerModel {
				model := nullModel()
				model.AccountName = types.StringValue("configured")
				model.ProjectName = types.StringValue("project")
				model.Environment = types.StringValue("usgovernment")
				model.APIKey = types.StringValue("configured-key")
				return model
			}(),
			environment: map[string]string{
				"FOUNDRY_ACCOUNT_NAME": "environment",
				"FOUNDRY_PROJECT_NAME": "environment-project",
				"FOUNDRY_ENVIRONMENT":  "public",
				"FOUNDRY_API_KEY":      "environment-key",
			},
			wantMethod:  clients.AuthenticationAPIKey,
			wantAccount: "configured",
			wantProject: "project",
			wantCloud:   clients.EnvironmentUSGovernment,
		},
		"authentication conflict": {
			model: func() providerModel {
				model := nullModel()
				model.AccountName = types.StringValue("example")
				model.ProjectName = types.StringValue("demo")
				model.APIKey = types.StringValue("key")
				model.UseCLI = types.BoolValue(true)
				return model
			}(),
			wantError: "cannot be combined",
		},
		"incomplete client secret": {
			model: func() providerModel {
				model := nullModel()
				model.AccountName = types.StringValue("example")
				model.ProjectName = types.StringValue("demo")
				model.ClientSecret = types.StringValue("secret")
				return model
			}(),
			wantError: "requires tenant_id and client_id",
		},
		"unknown value": {
			model: func() providerModel {
				model := nullModel()
				model.AccountName = types.StringUnknown()
				model.ProjectName = types.StringValue("demo")
				model.APIKey = types.StringValue("key")
				return model
			}(),
			wantError: "must be known",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			lookup := func(name string) (string, bool) {
				value, ok := test.environment[name]
				return value, ok
			}
			config, err := resolveConfig(test.model, lookup)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("resolveConfig() error = %v, want substring %q", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveConfig() error = %v", err)
			}
			if config.Auth.Method != test.wantMethod {
				t.Errorf("authentication method = %q, want %q", config.Auth.Method, test.wantMethod)
			}
			if config.AccountName != test.wantAccount {
				t.Errorf("account name = %q, want %q", config.AccountName, test.wantAccount)
			}
			if config.ProjectName != test.wantProject {
				t.Errorf("project name = %q, want %q", config.ProjectName, test.wantProject)
			}
			if config.Environment != test.wantCloud {
				t.Errorf("environment = %q, want %q", config.Environment, test.wantCloud)
			}
		})
	}
}
