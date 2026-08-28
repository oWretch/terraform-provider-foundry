package provider

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

type providerModel struct {
	AccountName               types.String `tfsdk:"account_name"`
	ProjectName               types.String `tfsdk:"project_name"`
	Environment               types.String `tfsdk:"environment"`
	APIKey                    types.String `tfsdk:"api_key"`
	TenantID                  types.String `tfsdk:"tenant_id"`
	ClientID                  types.String `tfsdk:"client_id"`
	ClientSecret              types.String `tfsdk:"client_secret"`
	ClientCertificatePath     types.String `tfsdk:"client_certificate_path"`
	ClientCertificatePassword types.String `tfsdk:"client_certificate_password"`
	UseOIDC                   types.Bool   `tfsdk:"use_oidc"`
	OIDCTokenFilePath         types.String `tfsdk:"oidc_token_file_path"`
	UseMSI                    types.Bool   `tfsdk:"use_msi"`
	UseCLI                    types.Bool   `tfsdk:"use_cli"`
}

type environmentLookup func(string) (string, bool)

func resolveConfig(model providerModel, lookup environmentLookup) (clients.Config, error) {
	stringsToCheck := map[string]types.String{
		"account_name":                model.AccountName,
		"project_name":                model.ProjectName,
		"environment":                 model.Environment,
		"api_key":                     model.APIKey,
		"tenant_id":                   model.TenantID,
		"client_id":                   model.ClientID,
		"client_secret":               model.ClientSecret,
		"client_certificate_path":     model.ClientCertificatePath,
		"client_certificate_password": model.ClientCertificatePassword,
		"oidc_token_file_path":        model.OIDCTokenFilePath,
	}
	for name, value := range stringsToCheck {
		if value.IsUnknown() {
			return clients.Config{}, fmt.Errorf("%q must be known before the provider can be configured", name)
		}
	}

	boolsToCheck := map[string]types.Bool{
		"use_oidc": model.UseOIDC,
		"use_msi":  model.UseMSI,
		"use_cli":  model.UseCLI,
	}
	for name, value := range boolsToCheck {
		if value.IsUnknown() {
			return clients.Config{}, fmt.Errorf("%q must be known before the provider can be configured", name)
		}
	}

	accountName := strings.TrimSpace(stringValue(model.AccountName, "FOUNDRY_ACCOUNT_NAME", lookup))
	if accountName == "" {
		return clients.Config{}, fmt.Errorf("set \"account_name\" or FOUNDRY_ACCOUNT_NAME")
	}
	projectName := strings.TrimSpace(stringValue(model.ProjectName, "FOUNDRY_PROJECT_NAME", lookup))
	if projectName == "" {
		return clients.Config{}, fmt.Errorf("set \"project_name\" or FOUNDRY_PROJECT_NAME")
	}
	environment := strings.ToLower(strings.TrimSpace(stringValueFallback(model.Environment, []string{"FOUNDRY_ENVIRONMENT", "ARM_ENVIRONMENT"}, lookup)))
	if environment == "" {
		environment = string(clients.EnvironmentPublic)
	}

	apiKey := stringValue(model.APIKey, "FOUNDRY_API_KEY", lookup)
	tenantID := strings.TrimSpace(stringValue(model.TenantID, "ARM_TENANT_ID", lookup))
	clientID := strings.TrimSpace(stringValue(model.ClientID, "ARM_CLIENT_ID", lookup))
	clientSecret := stringValue(model.ClientSecret, "ARM_CLIENT_SECRET", lookup)
	certificatePath := strings.TrimSpace(stringValue(model.ClientCertificatePath, "ARM_CLIENT_CERTIFICATE_PATH", lookup))
	certificatePassword := stringValue(model.ClientCertificatePassword, "ARM_CLIENT_CERTIFICATE_PASSWORD", lookup)
	oidcTokenFilePath := strings.TrimSpace(stringValueFallback(model.OIDCTokenFilePath, []string{"ARM_OIDC_TOKEN_FILE_PATH", "AZURE_FEDERATED_TOKEN_FILE"}, lookup))

	useOIDC, err := boolValue(model.UseOIDC, "ARM_USE_OIDC", lookup)
	if err != nil {
		return clients.Config{}, err
	}
	useMSI, err := boolValue(model.UseMSI, "ARM_USE_MSI", lookup)
	if err != nil {
		return clients.Config{}, err
	}
	useCLI, useCLIConfigured, err := boolValueConfigured(model.UseCLI, "ARM_USE_CLI", lookup)
	if err != nil {
		return clients.Config{}, err
	}

	hasEntraConfig := tenantID != "" || clientID != "" || clientSecret != "" || certificatePath != "" ||
		certificatePassword != "" || oidcTokenFilePath != "" || useOIDC || useMSI || useCLIConfigured && useCLI
	if apiKey != "" && hasEntraConfig {
		return clients.Config{}, fmt.Errorf("API key authentication cannot be combined with Microsoft Entra ID authentication")
	}
	if apiKey != "" {
		return clients.Config{
			AccountName: accountName,
			ProjectName: projectName,
			Environment: clients.Environment(environment),
			Auth: clients.Authentication{
				Method: clients.AuthenticationAPIKey,
				APIKey: apiKey,
			},
		}, nil
	}

	if clientSecret != "" && (tenantID == "" || clientID == "") {
		return clients.Config{}, fmt.Errorf("client secret authentication requires tenant_id and client_id")
	}
	if certificatePath != "" && (tenantID == "" || clientID == "") {
		return clients.Config{}, fmt.Errorf("client certificate authentication requires tenant_id and client_id")
	}
	if certificatePassword != "" && certificatePath == "" {
		return clients.Config{}, fmt.Errorf("client_certificate_password requires client_certificate_path")
	}
	if useOIDC && (tenantID == "" || clientID == "" || oidcTokenFilePath == "") {
		return clients.Config{}, fmt.Errorf("OIDC authentication requires tenant_id, client_id, and oidc_token_file_path")
	}
	if oidcTokenFilePath != "" && !useOIDC {
		return clients.Config{}, fmt.Errorf("oidc_token_file_path requires use_oidc")
	}

	if !useCLIConfigured && clientSecret == "" && certificatePath == "" && !useOIDC && !useMSI {
		useCLI = true
	}

	modes := 0
	if clientSecret != "" {
		modes++
	}
	if certificatePath != "" {
		modes++
	}
	if useOIDC {
		modes++
	}
	if useMSI {
		modes++
	}
	if useCLI {
		modes++
	}
	if modes == 0 {
		return clients.Config{}, fmt.Errorf("configure exactly one authentication mode: api_key, client_secret, client_certificate_path, use_oidc, use_msi, or use_cli")
	}
	if modes > 1 {
		return clients.Config{}, fmt.Errorf("configure only one Microsoft Entra ID authentication mode")
	}

	auth := clients.Authentication{
		TenantID:            tenantID,
		ClientID:            clientID,
		ClientSecret:        clientSecret,
		CertificatePath:     certificatePath,
		CertificatePassword: certificatePassword,
		OIDCTokenFilePath:   oidcTokenFilePath,
	}
	switch {
	case clientSecret != "":
		auth.Method = clients.AuthenticationClientSecret
	case certificatePath != "":
		auth.Method = clients.AuthenticationClientCertificate
	case useOIDC:
		auth.Method = clients.AuthenticationOIDC
	case useMSI:
		if tenantID != "" {
			return clients.Config{}, fmt.Errorf("tenant_id is not used with managed identity authentication")
		}
		auth.Method = clients.AuthenticationManagedIdentity
	case useCLI:
		if clientID != "" {
			return clients.Config{}, fmt.Errorf("client_id is not used with Azure CLI authentication")
		}
		auth.Method = clients.AuthenticationAzureCLI
	}

	return clients.Config{
		AccountName: accountName,
		ProjectName: projectName,
		Environment: clients.Environment(environment),
		Auth:        auth,
	}, nil
}

func stringValue(value types.String, environment string, lookup environmentLookup) string {
	return stringValueFallback(value, []string{environment}, lookup)
}

func stringValueFallback(value types.String, environments []string, lookup environmentLookup) string {
	if !value.IsNull() {
		return value.ValueString()
	}
	for _, environment := range environments {
		if result, ok := lookup(environment); ok {
			return result
		}
	}
	return ""
}

func boolValue(value types.Bool, environment string, lookup environmentLookup) (bool, error) {
	result, _, err := boolValueConfigured(value, environment, lookup)
	return result, err
}

func boolValueConfigured(value types.Bool, environment string, lookup environmentLookup) (bool, bool, error) {
	if !value.IsNull() {
		return value.ValueBool(), true, nil
	}
	raw, ok := lookup(environment)
	if !ok || raw == "" {
		return false, false, nil
	}
	result, err := strconv.ParseBool(raw)
	if err != nil {
		return false, true, fmt.Errorf("%s must be a boolean: %w", environment, err)
	}
	return result, true, nil
}
