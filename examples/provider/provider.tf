terraform {
  required_version = ">= 1.11.0"

  required_providers {
    foundry = {
      source = "registry.terraform.io/oWretch/foundry"
    }
  }
}

provider "foundry" {
  account_name = "example"
  project_name = "example"
}
