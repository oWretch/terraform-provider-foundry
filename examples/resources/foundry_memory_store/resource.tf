# foundry_memory_store is a preview feature. It requires an explicit opt-in
# on the provider before it can be used, and preview features may change or
# be removed in any provider release without following semantic versioning.
provider "foundry" {
  account_name   = var.foundry_account_name
  project_name   = var.foundry_project_name
  enable_preview = ["memory_stores"]
}

resource "foundry_memory_store" "agent_memory" {
  name            = "agent-memory"
  description     = "Long-term memory for the support agent"
  chat_model      = "gpt-4.1-mini"
  embedding_model = "text-embedding-3-small"

  options = {
    user_profile_enabled      = true
    chat_summary_enabled      = true
    procedural_memory_enabled = false
    default_ttl_seconds       = 2592000 # 30 days
  }

  metadata = {
    team = "support"
  }
}
