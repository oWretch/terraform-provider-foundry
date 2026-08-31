data "foundry_dataset" "knowledge" {
  name = "knowledge"
}

output "knowledge_dataset_version" {
  value = data.foundry_dataset.knowledge.version
}
