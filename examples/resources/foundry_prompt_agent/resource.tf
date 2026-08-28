resource "foundry_prompt_agent" "support" {
  name         = "support-agent"
  description  = "Answers product support questions."
  model        = "gpt-4.1"
  instructions = "You are a concise support agent. Answer in at most three sentences."
}
