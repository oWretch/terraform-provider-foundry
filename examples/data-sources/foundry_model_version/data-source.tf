# Model version APIs require `enable_preview = ["models"]` on the provider.
#
data "foundry_model_version" "classifier" {
  name    = "support-classifier"
  version = "1"
}

output "classifier_id" {
  value = data.foundry_model_version.classifier.id
}
