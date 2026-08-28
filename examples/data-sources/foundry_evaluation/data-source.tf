# Evaluations are a preview feature; opt in on the provider block:
# provider "foundry" {
#   enable_preview = ["evaluations"]
# }

data "foundry_evaluation" "support_quality" {
  id = "eval_abc123"
}

output "testing_criteria" {
  value = data.foundry_evaluation.support_quality.testing_criteria
}
