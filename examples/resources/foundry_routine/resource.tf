# foundry_routine is a preview feature. It requires an explicit opt-in on
# the provider before it can be used, and preview features may change or be
# removed in any provider release without following semantic versioning.
provider "foundry" {
  account_name   = var.foundry_account_name
  project_name   = var.foundry_project_name
  enable_preview = ["routines"]
}

resource "foundry_routine" "daily_summary" {
  name        = "daily-summary"
  description = "Runs a daily summary agent on weekday mornings."
  enabled     = true

  trigger = {
    type            = "schedule"
    cron_expression = "0 7 * * 1-5"
    time_zone       = "UTC"
  }

  action_type = "invoke_agent_responses_api"
  agent_name  = foundry_prompt_agent.summary.name
  input       = "Summarize activity from the last 24 hours."
}
