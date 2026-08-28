# foundry_external_agent is a preview feature. It requires an explicit
# opt-in on the provider before it can be used, and preview features may
# change or be removed in any provider release without following semantic
# versioning.
provider "foundry" {
  account_name   = var.foundry_account_name
  project_name   = var.foundry_project_name
  enable_preview = ["external_agents"]
}

resource "foundry_external_agent" "legacy_support_bot" {
  name        = "legacy-support-bot"
  description = "Support bot hosted outside Foundry, registered for observability only."
  endpoint    = "https://support-bot.contoso.com/agent"

  # Foundry never calls this endpoint. It only correlates OpenTelemetry
  # traces the external agent emits, tagged with this identifier.
  otel_agent_id = "legacy-support-bot"
}
