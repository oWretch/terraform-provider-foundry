# foundry_schedule is a preview feature. It requires an explicit opt-in on
# the provider before it can be used, and preview features may change or be
# removed in any provider release without following semantic versioning.
provider "foundry" {
  account_name   = var.foundry_account_name
  project_name   = var.foundry_project_name
  enable_preview = ["schedules"]
}

resource "foundry_schedule" "nightly_evaluation" {
  id           = "nightly-evaluation"
  display_name = "Nightly evaluation"
  description  = "Runs an evaluation every night at midnight UTC."
  enabled      = true

  trigger = {
    type            = "Cron"
    cron_expression = "0 0 * * *"
    time_zone       = "UTC"
  }

  task = {
    type           = "Evaluation"
    evaluation_id  = "eval-123"
    evaluation_run = jsonencode({})
  }

  tags = {
    team = "quality"
  }
}
