resource "foundry_vector_store_file" "manual" {
  vector_store_id = foundry_vector_store.support.id
  file_id         = foundry_file.manual.id
}
