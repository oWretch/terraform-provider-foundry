# Vector store names are not unique, so name is a filter and the result is a
# list. Use one() when the configuration expects exactly one match and should
# fail loudly if that stops being true.
data "foundry_vector_stores" "support" {
  name = "support-docs"
}

output "support_vector_store_id" {
  value = one(data.foundry_vector_stores.support.vector_stores).id
}
