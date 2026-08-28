resource "foundry_dataset" "knowledge" {
  name            = "product-knowledge"
  version         = "1"
  type            = "uri_file"
  connection_name = "storage"
  data_uri        = "https://example.blob.core.windows.net/knowledge/product.txt"
  description     = "Product knowledge base"
  tags = {
    env = "test"
  }
}
