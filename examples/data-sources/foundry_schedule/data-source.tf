# foundry_schedule is a preview feature. It requires an explicit opt-in on
# the provider before it can be used, and preview features may change or be
# removed in any provider release without following semantic versioning.
provider "foundry" {
  account_name   = var.foundry_account_name
  project_name   = var.foundry_project_name
  enable_preview = ["schedules"]
}

data "foundry_schedule" "nightly_evaluation" {
  id = "nightly-evaluation"
}

output "schedule_provisioning_status" {
  value = data.foundry_schedule.nightly_evaluation.provisioning_status
}
