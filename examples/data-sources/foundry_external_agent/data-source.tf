provider "foundry" {
  account_name   = var.foundry_account_name
  project_name   = var.foundry_project_name
  enable_preview = ["external_agents"]
}

data "foundry_external_agent" "example" {
  name = "example-external-agent"
}

output "external_agent_otel_id" {
  value = data.foundry_external_agent.example.otel_agent_id
}
