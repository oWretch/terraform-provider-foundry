data "foundry_voice_agent" "latest" {
  name = "support-voice"
}

data "foundry_voice_agent" "pinned" {
  name    = "support-voice"
  version = "1"
}
