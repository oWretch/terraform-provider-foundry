data "foundry_memory_store" "agent_memory" {
  name = "agent-memory"
}

output "agent_memory_updated_at" {
  value = data.foundry_memory_store.agent_memory.updated_at
}
