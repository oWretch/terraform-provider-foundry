#!/bin/sh
set -eu

: "${FOUNDRY_ACCOUNT_NAME:?FOUNDRY_ACCOUNT_NAME is required}"
: "${FOUNDRY_PROJECT_NAME:?FOUNDRY_PROJECT_NAME is required}"
: "${FOUNDRY_TEST_CHAT_MODEL_NAME:?FOUNDRY_TEST_CHAT_MODEL_NAME is required}"
: "${FOUNDRY_TEST_EMBEDDING_MODEL_NAME:?FOUNDRY_TEST_EMBEDDING_MODEL_NAME is required}"
: "${FOUNDRY_TEST_RUN_ID:?FOUNDRY_TEST_RUN_ID is required}"

case "$FOUNDRY_TEST_RUN_ID" in
*[!A-Za-z0-9-]*) echo "FOUNDRY_TEST_RUN_ID must contain only letters, numbers, and hyphens" >&2; exit 2 ;;
esac

search_connection=${FOUNDRY_TEST_SEARCH_CONNECTION_NAME:-}
search_index=${FOUNDRY_TEST_SEARCH_INDEX_NAME:-}
if { [ -n "$search_connection" ] && [ -z "$search_index" ]; } ||
	{ [ -z "$search_connection" ] && [ -n "$search_index" ]; }; then
	echo "FOUNDRY_TEST_SEARCH_CONNECTION_NAME and FOUNDRY_TEST_SEARCH_INDEX_NAME must be set together" >&2
	exit 2
fi

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
example="$root/examples/acceptance"
work="$root/.tmp/acceptance-$FOUNDRY_TEST_RUN_ID"
platform="$(go env GOOS)_$(go env GOARCH)"
mirror="$work/provider-mirror"
config="$work/terraform.rc"
install_dir="$mirror/registry.terraform.io/owretch/foundry/0.1.0/$platform"

export TF_CLI_CONFIG_FILE="$config"
export TF_VAR_run_id="$FOUNDRY_TEST_RUN_ID"
export TF_VAR_chat_model_name="$FOUNDRY_TEST_CHAT_MODEL_NAME"
export TF_VAR_embedding_model_name="$FOUNDRY_TEST_EMBEDDING_MODEL_NAME"
export TF_VAR_data_uri="${FOUNDRY_TEST_DATA_URI:-}"
export TF_VAR_storage_connection_name="${FOUNDRY_TEST_STORAGE_CONNECTION_NAME:-}"
export TF_VAR_search_connection_name="$search_connection"
export TF_VAR_search_index_name="$search_index"

delete_fixture() {
	output=$(az rest "$@" 2>&1) && return 0
	case "$output" in
	*"Not Found"* | *"not_found"* | *"ResourceNotFound"*) return 0 ;;
	esac
	printf '%s\n' "$output" >&2
	return 1
}

cleanup() {
	status=$?
	trap - EXIT INT TERM
	TF_VAR_revision=cleanup terraform -chdir="$example" destroy -auto-approve -input=false >/dev/null 2>&1 || true
	if command -v az >/dev/null 2>&1 && [ "${FOUNDRY_ENVIRONMENT:-public}" = public ]; then
		endpoint="https://$FOUNDRY_ACCOUNT_NAME.services.ai.azure.com/api/projects/$FOUNDRY_PROJECT_NAME"
		name="tf-acc-$FOUNDRY_TEST_RUN_ID"
		cleanup_failed=0
		delete_fixture --method delete --resource https://ai.azure.com --url "$endpoint/agents/$name-prompt?api-version=v1" --output none || cleanup_failed=1
		delete_fixture --method delete --resource https://ai.azure.com --headers Foundry-Features=MemoryStores=V1Preview --url "$endpoint/memory_stores/$name-memory?api-version=v1" --output none || cleanup_failed=1
		delete_fixture --method delete --resource https://ai.azure.com --url "$endpoint/datasets/$name-dataset/versions/$FOUNDRY_TEST_RUN_ID?api-version=v1" --output none || cleanup_failed=1
		delete_fixture --method delete --resource https://ai.azure.com --url "$endpoint/indexes/$name-index/versions/$FOUNDRY_TEST_RUN_ID?api-version=v1" --output none || cleanup_failed=1
		if [ "$status" -eq 0 ] && [ "$cleanup_failed" -ne 0 ]; then
			status=1
		fi
	fi
	rm -rf "$example/.terraform" "$work"
	rm -f "$example/.terraform.lock.hcl" "$example/terraform.tfstate" "$example/terraform.tfstate.backup"
	exit "$status"
}
trap cleanup EXIT INT TERM

mkdir -p "$install_dir"
go build -o "$install_dir/terraform-provider-foundry_v0.1.0" "$root"

cat >"$config" <<EOF
provider_installation {
  filesystem_mirror {
    path    = "$mirror"
    include = ["registry.terraform.io/oWretch/foundry"]
  }
  direct {
    exclude = ["registry.terraform.io/oWretch/foundry"]
  }
}
EOF

terraform fmt -check -recursive "$example"
terraform -chdir="$example" init -backend=false -input=false
TF_VAR_revision=one terraform -chdir="$example" apply -auto-approve -input=false
TF_VAR_revision=two terraform -chdir="$example" apply -auto-approve -input=false
TF_VAR_revision=two terraform -chdir="$example" plan -detailed-exitcode -input=false
TF_VAR_revision=two terraform -chdir="$example" destroy -auto-approve -input=false
