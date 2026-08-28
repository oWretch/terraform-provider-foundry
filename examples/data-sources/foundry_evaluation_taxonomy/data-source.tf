# Evaluations are a preview feature; opt in on the provider block:
# provider "foundry" {
#   enable_preview = ["evaluations"]
# }

data "foundry_evaluation_taxonomy" "safety" {
  name = "customer-support-safety"
}

output "risk_categories" {
  value = data.foundry_evaluation_taxonomy.safety.categories
}
