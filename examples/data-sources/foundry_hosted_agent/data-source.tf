data "foundry_hosted_agent" "example" {
  name = "example-hosted-agent"
}

output "hosted_agent_image" {
  value = data.foundry_hosted_agent.example.image
}
