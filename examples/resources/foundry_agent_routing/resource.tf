provider "foundry" {
  account_name   = var.foundry_account_name
  project_name   = var.foundry_project_name
  enable_preview = ["agent_endpoints"]
}

resource "foundry_prompt_agent" "example" {
  name         = "example-prompt-agent"
  model        = "gpt-4.1-mini"
  instructions = "Answer briefly."
}

resource "foundry_agent_routing" "example" {
  agent_name = foundry_prompt_agent.example.name

  version_selection_rules {
    agent_version = foundry_prompt_agent.example.version
  }
}

output "agent_active_version" {
  value = foundry_agent_routing.example.active_version
}
