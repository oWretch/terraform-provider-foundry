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
- `foundry_dataset` data source for looking up a dataset by name and optional version. Omitting `version` resolves the service's current latest version.
- `foundry_index` data source for looking up an Azure AI Search-backed index by name and optional version. Omitting `version` resolves the service's current latest version.
- `foundry_deployments` data source listing the model deployments available to the project.
- `foundry_connections` data source listing the connections configured on the account and project, for referencing an existing connection by name.
- `foundry_memory_store` resource (preview) for managing a memory store that extracts and stores user profile, chat summary, and procedural memories from agent conversations. Requires `enable_preview = ["memory_stores"]` on the provider.
- `foundry_memory_store` data source (preview) for looking up the current memory store object by name. Requires `enable_preview = ["memory_stores"]` on the provider.
- `foundry_routine` resource (preview) for managing a named automation rule that triggers an agent on a schedule or at a specific time. Requires `enable_preview = ["routines"]` on the provider.
- `foundry_routine` data source (preview) for looking up an existing routine by name. Requires `enable_preview = ["routines"]` on the provider.
- `foundry_schedule` resource (preview) for managing a schedule that runs an evaluation or insight task on a cron, recurrence, or one-time trigger. Requires `enable_preview = ["schedules"]` on the provider.
- `foundry_schedule` data source (preview) for looking up an existing schedule by ID. Requires `enable_preview = ["schedules"]` on the provider.
- `foundry_external_agent` resource (preview) for registering an agent hosted outside Foundry for observability, tracing, and evaluation only. Requires `enable_preview = ["external_agents"]` on the provider.
- `foundry_prompt_agent`, `foundry_hosted_agent`, and `foundry_external_agent` data sources for looking up the latest agent version by name. The external agent data source requires `enable_preview = ["external_agents"]`.
- `draft` attribute (preview) on `foundry_prompt_agent` and `foundry_hosted_agent`, publishing a mutable draft version instead of an immutable numbered version. Requires `enable_preview = ["draft_agents"]` on the provider.
- `agent_endpoint` computed attribute (preview) on `foundry_prompt_agent` and `foundry_hosted_agent`, surfacing the agent's built-in endpoint protocols and version traffic routing rules. Documented as requiring `enable_preview = ["agent_endpoints"]`; verified live that the service currently populates this field without any `Foundry-Features` header, but the attribute is treated as preview and may change without following semver.
- `foundry_skill` resource (preview) for managing a named skill, the single owner of its immutable version chain and `default_version` pointer. Create publishes the first version from inline instructions or an uploaded `SKILL.md`/`.zip` file; any change publishes a new version and promotes it to default. Requires `enable_preview = ["skills"]` on the provider.
- `foundry_skill` data source (preview) for looking up a specific skill version's metadata by name and version. Requires `enable_preview = ["skills"]` on the provider.
- `foundry_toolbox` resource (preview) for managing a named toolbox, the single owner of its immutable version chain and `default_version` pointer, bundling tools and skill references behind one MCP endpoint. Requires `enable_preview = ["toolboxes"]` on the provider.
- `foundry_toolbox` data source (preview) for looking up a specific toolbox version's tool and skill configuration by name and version. Requires `enable_preview = ["toolboxes"]` on the provider.
- `foundry_evaluator_version` resource (preview) for managing an immutable evaluator version (prompt, code, rubric, or endpoint definition) used by evaluations. Requires `enable_preview = ["evaluations"]` on the provider.
- `foundry_evaluator_version` data source (preview) for looking up a specific evaluator version by name and version. Requires `enable_preview = ["evaluations"]` on the provider.
- `foundry_evaluation_taxonomy` resource (preview) for managing an agent risk taxonomy; categories and subcategories are auto-derived by the service from the selected risk categories and modeled as a computed, order-preserving list. Requires `enable_preview = ["evaluations"]` on the provider.
- `foundry_evaluation_taxonomy` data source (preview) for looking up an existing evaluation taxonomy by name. Requires `enable_preview = ["evaluations"]` on the provider.
- `foundry_evaluation` resource (preview) for managing an evaluation definition (data source item schema and testing criteria) on the OpenAI-compatible `/openai/v1/evals` route; evaluation runs, output items, and cancellation are intentionally left outside Terraform. Requires `enable_preview = ["evaluations"]` on the provider.
- `foundry_evaluation` data source (preview) for looking up an existing evaluation definition by ID. Requires `enable_preview = ["evaluations"]` on the provider.
- `foundry_evaluation_rule` resource (preview) for managing a rule that starts a continuous evaluation run against a `foundry_evaluation` when an agent event occurs; evaluation execution itself is left outside Terraform. Requires `enable_preview = ["evaluations"]` on the provider.
- `foundry_evaluation_rule` data source (preview) for looking up an existing evaluation rule by ID. Requires `enable_preview = ["evaluations"]` on the provider.
- `foundry_connection` data source for looking up a single connection on the account or project by name, for referencing an existing connection from a tool or index. Connections are created through Azure Resource Manager; the Foundry data plane exposes them read-only.
- Typed `tools` blocks on the `foundry_toolbox` resource, covering every tool type the service accepts, with per-type argument validation during `terraform validate` and `terraform plan`.
- `toolbox_tools` preview feature gating the preview tool types available in a toolbox's `tools` block.
- A warning on every plan naming each preview feature and preview tool type in use, so the exemption from compatibility guarantees stays visible for as long as a preview feature is configured.
- `foundry_model_version` resource for uploading a custom model artifact and registering it as a model version, including `LoRA` adapters. The artifact is uploaded to service-managed storage through the pending-upload handshake, because the model registry only issues a container SAS for storage it manages and rejects a practitioner's own blob URI.
- `foundry_model_version` data source for looking up an existing model version by name and version.

- `foundry_files` data source for listing uploaded files, optionally filtered by purpose. Files are identified by a service-assigned ID rather than by name, so a list is the only way to reference a file the configuration did not upload.
- `foundry_vector_stores` data source for listing vector stores, optionally filtered by name. Vector store names are not unique, so the result is a list rather than a single store.

### Changed

- Agent resources now use the current v1 API definitions. Hosted agents use `container_configuration` and `protocol_versions`, external agents use `otel_agent_id`, and resources reject imported definitions or version metadata that an update would discard.
- Dataset, index, and memory-store resources now use the current v1 wire fields. Create-only dataset and Azure Search fields force replacement, index field mappings are preserved, unsupported index and memory-store kinds are rejected, and mutable metadata can be cleared.
- The `foundry_toolbox` resource replaces the `tools_json` argument with repeatable typed `tools` blocks. A configuration using `tools_json` must be rewritten; each element of the previous JSON array becomes one `tools` block with the same keys as arguments. The `foundry_toolbox` data source keeps `tools_json` as a computed attribute.

### Fixed

- The `foundry_file` resource no longer plans a replacement after an import. `source_path` is now optional, because the service cannot report the local path an imported file came from, and `purpose` is now read back from the service instead of relying on the configuration. Uploading still requires both, and a create without them fails with an explanatory error.

- Skill `instructions` are now recovered when reading a skill, so importing a skill no longer produces a plan that publishes an identical version.
- Skill `description` is derived from an uploaded skill file when it is not configured, so `source_path` no longer fails with an inconsistent result after apply.
- The `foundry_skill` data source now returns `instructions`, which it previously omitted even though the skill body is the point of looking one up.
- The `server_label` that the service derives from a `fabric_iq_preview` tool's `project_connection_id` is no longer written to state, so a toolbox using that tool applies and imports without a spurious difference.
- Requests to OpenAI-compatible routes under `/openai/v1` no longer send an unsupported `api-version` parameter.
