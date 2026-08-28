# Changelog

This file records user-visible changes to the provider.

## Unreleased

### Added

- Initial protocol 6 provider wiring.
- API key, service principal, workload identity, managed identity, and Azure CLI authentication.
- Azure CLI authentication by default when no other mode is configured.
- Account and project configuration for public, US Government, and China environments.
- Local development, CI, documentation, and draft release workflows.
- `foundry_prompt_agent` resource for agents backed by a model deployment and instructions.
- `foundry_hosted_agent` resource for agents backed by a container image.
- `foundry_file` resource for uploading files used by other Foundry resources.
- `foundry_vector_store` resource for indexing attached files for retrieval.
- `foundry_vector_store_file` resource for attaching a file to a vector store.
- `foundry_dataset` resource for registering a blob file or folder as a dataset version.
- `foundry_index` resource for managing an Azure AI Search-backed index version.
- `foundry_deployments` data source listing the model deployments available to the project.

### Fixed

- Requests to OpenAI-compatible routes under `/openai/v1` no longer send an unsupported `api-version` parameter.
