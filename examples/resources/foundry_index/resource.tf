resource "foundry_index" "search" {
  name            = "product-index"
  version         = "1"
  connection_name = "azure-ai-search"
  index_name      = "product-search-index"
}
