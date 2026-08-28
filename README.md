# Terraform Provider for Microsoft Foundry

This repository contains an early Terraform provider for the current Microsoft Foundry Agent Service. It currently implements provider configuration and authentication only. Resources and data sources will be added once their API contracts are chosen.

The provider uses Terraform Plugin Protocol 6 and requires Terraform or OpenTofu 1.11 or later. The Go module requires Go 1.24 or later.

## Development

Download dependencies and run the offline checks:

```shell
go mod download
make check
```

`make check` formats the Go code, runs vet and lint, executes unit tests, builds the provider, regenerates documentation, and validates the provider example. OpenTofu validation runs when `tofu` is installed.

To regenerate only the provider documentation:

```shell
make docs
```

## Local provider override

Build the provider in the repository root:

```shell
go build -o terraform-provider-foundry
```

Create a Terraform CLI configuration outside the repository and replace `/absolute/path/to/terraform-provider-foundry` with this repository's absolute path:

```hcl
provider_installation {
  dev_overrides {
    "oWretch/foundry" = "/absolute/path/to/terraform-provider-foundry"
  }

  direct {}
}
```

Point Terraform or OpenTofu at that file:

```shell
TF_CLI_CONFIG_FILE=/absolute/path/to/dev.tfrc terraform -chdir=examples/provider plan
TF_CLI_CONFIG_FILE=/absolute/path/to/dev.tfrc tofu -chdir=examples/provider plan
```

Development overrides are for local testing. Do not run `terraform init` for the overridden provider.

## Provider configuration

Set `account_name` and `project_name` to select the Foundry project. The provider builds the project endpoint for the selected Azure environment. Provider arguments take precedence over environment variables.

The provider uses Azure CLI authentication when no authentication mode is configured. Otherwise, configure exactly one of these modes:

| Mode | Provider arguments | Environment variables |
| --- | --- | --- |
| API key | `api_key` | `FOUNDRY_API_KEY` |
| Client secret | `tenant_id`, `client_id`, `client_secret` | `ARM_TENANT_ID`, `ARM_CLIENT_ID`, `ARM_CLIENT_SECRET` |
| Client certificate | `tenant_id`, `client_id`, `client_certificate_path`, optional password | Matching `ARM_*` variables |
| Workload identity | `use_oidc`, `tenant_id`, `client_id`, `oidc_token_file_path` | `ARM_USE_OIDC`, `ARM_TENANT_ID`, `ARM_CLIENT_ID`, `ARM_OIDC_TOKEN_FILE_PATH` |
| Managed identity | `use_msi`, optional `client_id` | `ARM_USE_MSI`, optional `ARM_CLIENT_ID` |
| Azure CLI | `use_cli`, optional `tenant_id` | `ARM_USE_CLI`, optional `ARM_TENANT_ID` |

The account and project names can also be set with `FOUNDRY_ACCOUNT_NAME` and `FOUNDRY_PROJECT_NAME`. The `environment` argument accepts `public`, `usgovernment`, or `china` and defaults to `public`. It can also be set with `FOUNDRY_ENVIRONMENT` or `ARM_ENVIRONMENT`. The client uses the current `v1` API.

| Environment | Project endpoint suffix | Token scope |
| --- | --- | --- |
| `public` | `services.ai.azure.com` | `https://ai.azure.com/.default` |
| `usgovernment` | `services.ai.azure.us` | `https://ai.azure.us/.default` |
| `china` | `services.ai.azure.cn` | `https://ai.azure.cn/.default` |

Set `use_cli = false` or `ARM_USE_CLI=false` to disable the default. API key authentication cannot be combined with an explicitly enabled Microsoft Entra ID mode. The provider does not use `DefaultAzureCredential`; managed identity and workload identity require explicit opt-in.

## Scope

The provider targets the current Foundry project API. It does not support the deprecated Assistants API, classic Foundry agents, or API-version-specific Terraform resource names.

## Releases

Release automation is present but creates draft releases. Publishing requires repository signing secrets and Terraform Registry registration.

## License

Mozilla Public License 2.0. See [LICENSE](LICENSE).
