# Evaluations are a preview feature; opt in on the provider block:
# provider "foundry" {
#   enable_preview = ["evaluations"]
# }

data "foundry_evaluation_rule" "continuous_support_eval" {
  id = "support-response-completed"
}

output "eval_id" {
  value = data.foundry_evaluation_rule.continuous_support_eval.eval_id
}
