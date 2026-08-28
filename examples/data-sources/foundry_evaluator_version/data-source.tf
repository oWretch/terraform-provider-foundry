# Evaluations are a preview feature; opt in on the provider block:
# provider "foundry" {
#   enable_preview = ["evaluations"]
# }

data "foundry_evaluator_version" "relevance" {
  name    = "response-relevance"
  version = "1"
}

output "evaluator_id" {
  value = data.foundry_evaluator_version.relevance.id
}
