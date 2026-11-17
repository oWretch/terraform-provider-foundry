resource "foundry_voice_agent" "support" {
  name         = "support-voice"
  model        = "gpt-realtime"
  model_type   = "managed"
  instructions = "Help callers find the right support team. Keep replies short."
  voice        = "en-US-AvaNeural"
  voice_type   = "azure-standard"
}
