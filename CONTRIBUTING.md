# Contributing

## Development setup

Install Go 1.25.13 or later, Terraform 1.11 or later, and optionally OpenTofu 1.11 or later.

```shell
go mod download
make check
```

Keep changes focused and add the smallest test that would fail without the change. Update provider documentation and `CHANGELOG.md` when behavior visible to users changes.

Generated provider documentation lives in `docs`. Edit the provider schema, examples, or `templates`, then run `make docs`.

## Pull requests

Pull requests should explain the problem, the chosen fix, and any user-facing effect. Complete the pull request checklist and keep credentialed acceptance tests out of untrusted workflows.

All commits must include a Developer Certificate of Origin sign-off:

```text
Signed-off-by: Name <email@example.com>
```

Use `git commit -s` to add the sign-off automatically. By signing off, you certify that you have the right to submit the contribution under this repository's license.

## Protected automation

Azure acceptance tests run with `TF_ACC=1` in the `foundry-ci` environment for same-repository pull requests, `main`, scheduled runs, and manual runs. Fork pull requests never receive Azure credentials or run Azure login. Configure these environment variables:

- Required OIDC values: `ARM_CLIENT_ID`, `ARM_TENANT_ID`, `ARM_SUBSCRIPTION_ID`.
- Required target values: `FOUNDRY_ACCOUNT_NAME`, `FOUNDRY_PROJECT_NAME`, `FOUNDRY_TEST_CHAT_MODEL_NAME`, and `FOUNDRY_TEST_EMBEDDING_MODEL_NAME`.
- Optional dataset values: `FOUNDRY_TEST_DATA_URI` and, when the URI needs it, `FOUNDRY_TEST_STORAGE_CONNECTION_NAME`.
- Optional Azure AI Search values: `FOUNDRY_TEST_SEARCH_CONNECTION_NAME` and `FOUNDRY_TEST_SEARCH_INDEX_NAME`. Set both or neither.

The federated identity needs the Foundry User role on the target account. No client secret is used.
Configure its GitHub OIDC credential with audience `api://AzureADTokenExchange` and subject `repo:oWretch/terraform-provider-foundry:environment:foundry-ci`.

Tag pushes matching `v*` publish a signed, non-draft GitHub release through the protected `release` environment. Configure `GPG_PRIVATE_KEY` and `GPG_FINGERPRINT` as environment secrets and require approval for that environment.
