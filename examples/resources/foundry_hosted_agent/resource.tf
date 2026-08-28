resource "foundry_hosted_agent" "checkout" {
  name        = "checkout-agent"
  description = "Runs the checkout agent container."
  image       = "contoso.azurecr.io/agents/checkout:1.4.0"
  cpu         = "1"
  memory      = "2Gi"

  environment_variables = {
    LOG_LEVEL = "info"
  }

  container_protocol_versions = [{
    protocol = "responses"
    version  = "1"
  }]
}
