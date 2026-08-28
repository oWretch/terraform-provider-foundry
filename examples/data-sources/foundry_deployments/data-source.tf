data "foundry_deployments" "available" {}

# Use a deployment name to back an agent.
output "chat_deployments" {
  value = [
    for deployment in data.foundry_deployments.available.deployments :
    deployment.name if deployment.model_publisher == "OpenAI"
  ]
}
