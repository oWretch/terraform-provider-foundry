# foundry_routine is a preview feature. It requires an explicit opt-in on
# the provider before it can be used, and preview features may change or be
# removed in any provider release without following semantic versioning.
provider "foundry" {
  account_name   = var.foundry_account_name
  project_name   = var.foundry_project_name
  enable_preview = ["routines"]
}

data "foundry_routine" "daily_summary" {
  name = "daily-summary"
}

output "routine_agent_name" {
  value = data.foundry_routine.daily_summary.agent_name
}
