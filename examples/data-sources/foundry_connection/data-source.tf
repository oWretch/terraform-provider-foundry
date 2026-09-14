data "foundry_connection" "search" {
  name = "azure-ai-search"
}

output "search_connection_id" {
  value = data.foundry_connection.search.id
}
