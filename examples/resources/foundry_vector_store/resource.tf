resource "foundry_vector_store" "support" {
  name = "support-knowledge-base"
  metadata = {
    team = "support"
  }
  expires_after_days = 30
}
