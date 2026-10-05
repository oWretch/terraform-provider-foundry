provider "foundry" {
  account_name = var.foundry_account_name
  project_name = var.foundry_project_name
}

data "foundry_routine" "daily_summary" {
  name = "daily-summary"
}

output "routine_agent_name" {
  value = data.foundry_routine.daily_summary.agent_name
}
