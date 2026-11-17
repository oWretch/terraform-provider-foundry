data "foundry_prompt_agent" "example" {
  name = "example-prompt-agent"
}

data "foundry_prompt_agent" "pinned" {
  name    = "example-prompt-agent"
  version = "1"
}

output "prompt_agent_model" {
  value = data.foundry_prompt_agent.example.model
}

output "prompt_agent_active_version" {
  value = data.foundry_prompt_agent.example.active_version
}

# Use a provider configuration with enable_preview = ["copilot_harness"].
data "foundry_prompt_agent" "copilot" {
  name = "copilot-support-agent"
}

output "copilot_harness_type" {
  value = data.foundry_prompt_agent.copilot.harness_type
}
