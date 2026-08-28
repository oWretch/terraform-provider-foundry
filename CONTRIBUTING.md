# Contributing

## Development setup

Install Go 1.24 or later, Terraform 1.11 or later, and optionally OpenTofu 1.11 or later.

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
