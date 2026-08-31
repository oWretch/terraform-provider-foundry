# Terraform Provider for Microsoft Foundry

This repository contains a Terraform provider for the current Microsoft Foundry Agent Service.

## Resources

- `foundry_prompt_agent` — an agent backed by a model deployment and instructions.
- `foundry_hosted_agent` — an agent backed by a container image you supply.
- `foundry_file` — an uploaded file used by other Foundry resources.
- `foundry_vector_store` — a vector store that indexes attached files for retrieval.
- `foundry_vector_store_file` — a file attached to a vector store.
- `foundry_dataset` — a dataset version registered from a blob file or folder.
- `foundry_index` — an Azure AI Search-backed index version.

### Preview resources

These require opt-in through the provider's `enable_preview` argument. See [Preview features](#preview-features).

- `foundry_external_agent` — an agent hosted outside Foundry, registered for observability (`external_agents`).
- `foundry_memory_store` — a memory store backing agent recall (`memory_stores`).
- `foundry_skill` — a versioned skill and its default version pointer (`skills`).
- `foundry_toolbox` — a versioned toolbox and its default version pointer (`toolboxes`).
- `foundry_routine` — a triggered routine that invokes an agent (`routines`).
- `foundry_schedule` — a schedule that runs an evaluation or insight task (`schedules`).
- `foundry_evaluator_version` — a code, prompt, rubric, or endpoint evaluator version (`evaluations`).
- `foundry_evaluation_taxonomy` — an evaluation taxonomy and its risk categories (`evaluations`).
- `foundry_evaluation` — an evaluation definition and its testing criteria (`evaluations`).
- `foundry_evaluation_rule` — a continuous evaluation rule (`evaluations`).

## Data Sources

- `foundry_deployments` — model deployments available to the configured project.
- `foundry_connections` — connections configured on the account and project, for referencing an existing connection by name.
- `foundry_prompt_agent`, `foundry_hosted_agent` — read the latest version of an agent by name.
- `foundry_dataset` — read a dataset by name and optional version; omitting the version resolves the service's current latest version.
- `foundry_index` — read an Azure AI Search-backed index by name and optional version; omitting the version resolves the service's current latest version.

### Preview data sources

- `foundry_skill`, `foundry_toolbox` — read a specific skill or toolbox version (`skills`, `toolboxes`).
- `foundry_memory_store` — read the current memory store object by name (`memory_stores`).
- `foundry_external_agent` — read the latest external agent version by name (`external_agents`).
- `foundry_routine`, `foundry_schedule` — read an existing routine or schedule (`routines`, `schedules`).
- `foundry_evaluator_version`, `foundry_evaluation_taxonomy`, `foundry_evaluation`, `foundry_evaluation_rule` — read existing evaluation objects (`evaluations`).

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

## Preview features

Some Foundry APIs are still in preview. The resources and data sources built on them are only usable after opting in by name through `enable_preview`:

```hcl
provider "foundry" {
  account_name = "example"
  project_name = "demo"

  enable_preview = ["evaluations", "memory_stores"]
}
```

Opt-in is per feature, so enabling one preview family does not enable the others. Using a preview resource or data source without listing its feature fails during `terraform validate` or `terraform plan`, before any API call is made.

| `enable_preview` value | Resources and data sources |
| --- | --- |
| `agent_endpoints` | The `agent_endpoint` attribute on agent resources |
| `draft_agents` | The `draft` attribute on agent resources |
| `evaluations` | `foundry_evaluation`, `foundry_evaluation_rule`, `foundry_evaluation_taxonomy`, `foundry_evaluator_version` |
| `external_agents` | `foundry_external_agent` |
| `memory_stores` | `foundry_memory_store` |
| `routines` | `foundry_routine` |
| `schedules` | `foundry_schedule` |
| `skills` | `foundry_skill` |
| `toolboxes` | `foundry_toolbox` |
| `toolbox_tools` | Preview tool types in the `tools` block of `foundry_toolbox` |

Preview tool types are named by the service with a `_preview` suffix, such as `fabric_iq_preview`. The suffix is part of the tool type, not a modifier the provider adds, and a preview type is not interchangeable with its non-preview counterpart: `a2a` and `a2a_preview` coexist and take different arguments. When a tool type reaches general availability the service publishes it under a new name, so configurations using the preview name must be updated at that point. Each preview tool in use is reported as a warning on every plan.

Preview features are exempt from the provider's compatibility guarantees. Their arguments, attributes, and behavior may change in any release, including a patch release, and the underlying preview API may be withdrawn by the service. Do not depend on them where a stable upgrade path matters.

## Scope

The provider targets the current Foundry project API. It does not support the deprecated Assistants API, classic Foundry agents, or API-version-specific Terraform resource names.

## Releases

Release automation is present but creates draft releases. Publishing requires repository signing secrets and Terraform Registry registration.

## License

Mozilla Public License 2.0. See [LICENSE](LICENSE).
