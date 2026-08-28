#!/bin/sh
set -eu

cli=${1:?usage: validate-example.sh terraform|tofu}
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
platform="$(go env GOOS)_$(go env GOARCH)"
mirror="$root/.tmp/provider-mirror"
install_dir="$mirror/registry.terraform.io/owretch/foundry/0.1.0/$platform"
config="$root/.tmp/$cli.rc"
example="$root/examples/provider"

cleanup() {
	rm -rf "$example/.terraform"
	rm -f "$example/.terraform.lock.hcl"
}
trap cleanup EXIT

rm -rf "$mirror"
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

"$cli" fmt -check -recursive "$root/examples"
TF_CLI_CONFIG_FILE="$config" "$cli" -chdir="$example" init -backend=false
TF_CLI_CONFIG_FILE="$config" "$cli" -chdir="$example" validate
