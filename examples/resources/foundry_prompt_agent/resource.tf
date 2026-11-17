resource "foundry_prompt_agent" "support" {
  name         = "support-agent"
  description  = "Answers product support questions."
  model        = "gpt-4.1"
  instructions = "You are a concise support agent. Answer in at most three sentences."
}

# Use a provider configuration with enable_preview = ["copilot_harness"].
resource "foundry_prompt_agent" "copilot" {
  name         = "copilot-support-agent"
  model        = "my-reasoning-model-deployment"
  instructions = "Answer product support questions in at most three sentences."
  harness_type = "microsoft_copilot_preview"
}
