# Skills are a preview feature; opt in on the provider block:
# provider "foundry" {
#   enable_preview = ["skills"]
# }

data "foundry_skill" "greeting" {
  name    = "greeting"
  version = foundry_skill.greeting.default_version
}

output "greeting_skill_id" {
  value = data.foundry_skill.greeting.id
}
