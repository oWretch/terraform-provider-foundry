data "foundry_prompt_agent" "example" {
  name = "example-prompt-agent"
}

output "prompt_agent_model" {
  value = data.foundry_prompt_agent.example.model
}
