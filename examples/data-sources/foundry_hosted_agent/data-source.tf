data "foundry_hosted_agent" "example" {
  name = "example-hosted-agent"
}

data "foundry_hosted_agent" "pinned" {
  name    = "example-hosted-agent"
  version = "1"
}

output "hosted_agent_image" {
  value = data.foundry_hosted_agent.example.image
}
