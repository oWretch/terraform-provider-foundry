data "foundry_index" "knowledge" {
  name    = "knowledge-index"
  version = "1"
}

output "knowledge_index_id" {
  value = data.foundry_index.knowledge.id
}
