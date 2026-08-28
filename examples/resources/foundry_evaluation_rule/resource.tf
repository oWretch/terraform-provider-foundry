# Evaluations are a preview feature; opt in on the provider block:
# provider "foundry" {
#   enable_preview = ["evaluations"]
# }

resource "foundry_evaluation_rule" "continuous_support_eval" {
  id           = "support-response-completed"
  display_name = "Continuously evaluate support responses"
  event_type   = "responseCompleted"
  agent_name   = foundry_hosted_agent.support.name
  eval_id      = foundry_evaluation.support_quality.id
}
