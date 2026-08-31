# A full-weight model uploaded from a directory. The whole directory is sent,
# preserving its layout, which is what a model split across weight, tokenizer,
# and configuration files needs.
resource "foundry_model_version" "classifier" {
  name        = "support-classifier"
  version     = "1"
  source_path = "${path.module}/artifacts/classifier"
  weight_type = "FullWeight"
  description = "Ticket classifier fine-tuned on support transcripts."

  tags = {
    team = "support"
  }
}

# A LoRA adapter, which the serving engine needs adapter settings for.
resource "foundry_model_version" "adapter" {
  name        = "support-classifier-adapter"
  version     = "1"
  source_path = "${path.module}/artifacts/adapter.safetensors"
  weight_type = "LoRA"
  base_model  = foundry_model_version.classifier.id

  lora_config {
    rank           = 16
    alpha          = 32
    dropout        = 0.05
    target_modules = ["q_proj", "v_proj"]
  }
}
