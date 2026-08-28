# Toolboxes are a preview feature; opt in on the provider block:
# provider "foundry" {
#   enable_preview = ["toolboxes"]
# }

data "foundry_toolbox" "my_toolbox" {
  name    = "my-toolbox"
  version = foundry_toolbox.my_toolbox.default_version
}

output "toolbox_tools" {
  value = data.foundry_toolbox.my_toolbox.tools_json
}
