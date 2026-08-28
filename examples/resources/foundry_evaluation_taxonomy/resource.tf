# Evaluations are a preview feature; opt in on the provider block:
# provider "foundry" {
#   enable_preview = ["evaluations"]
# }

resource "foundry_evaluation_taxonomy" "safety" {
  name            = "customer-support-safety"
  description     = "Risk taxonomy for the customer support agent."
  agent_name      = foundry_hosted_agent.support.name
  agent_version   = foundry_hosted_agent.support.version
  risk_categories = ["ProhibitedActions"]
}
