terraform {
  required_version = ">= 1.11.0"

  required_providers {
    foundry = {
      source  = "registry.terraform.io/oWretch/foundry"
      version = "0.1.0"
    }
  }
}

variable "run_id" {
  type = string
}

variable "revision" {
  type = string
}

variable "chat_model_name" {
  type = string
}

variable "embedding_model_name" {
  type = string
}

variable "data_uri" {
  type      = string
  default   = ""
  sensitive = true
}

variable "storage_connection_name" {
  type    = string
  default = ""
}

variable "search_connection_name" {
  type    = string
  default = ""
}

variable "search_index_name" {
  type    = string
  default = ""
}

locals {
  test_dataset = var.data_uri != ""
  test_index   = var.search_connection_name != "" && var.search_index_name != ""
}

provider "foundry" {
  enable_preview = ["memory_stores"]
}

resource "foundry_prompt_agent" "acceptance" {
  name         = "tf-acc-${var.run_id}-prompt"
  model        = var.chat_model_name
  instructions = "Acceptance revision ${var.revision}."
}

data "foundry_prompt_agent" "acceptance" {
  name = foundry_prompt_agent.acceptance.name

  lifecycle {
    postcondition {
      condition     = self.model == var.chat_model_name
      error_message = "Prompt agent lookup returned the wrong model."
    }
  }
}

resource "foundry_memory_store" "acceptance" {
  name            = "tf-acc-${var.run_id}-memory"
  description     = "Acceptance revision ${var.revision}."
  chat_model      = var.chat_model_name
  embedding_model = var.embedding_model_name
}

data "foundry_memory_store" "acceptance" {
  name = foundry_memory_store.acceptance.name

  lifecycle {
    postcondition {
      condition     = self.embedding_model == var.embedding_model_name
      error_message = "Memory store lookup returned the wrong embedding model."
    }
  }
}

resource "foundry_dataset" "acceptance" {
  count = local.test_dataset ? 1 : 0

  name            = "tf-acc-${var.run_id}-dataset"
  version         = var.run_id
  type            = "uri_file"
  connection_name = var.storage_connection_name == "" ? null : var.storage_connection_name
  data_uri        = var.data_uri
  description     = "Acceptance revision ${var.revision}."
}

data "foundry_dataset" "acceptance" {
  count   = local.test_dataset ? 1 : 0
  name    = foundry_dataset.acceptance[0].name
  version = foundry_dataset.acceptance[0].version

  lifecycle {
    postcondition {
      condition     = self.version == var.run_id
      error_message = "Dataset lookup did not resolve the created latest version."
    }
  }
}

resource "foundry_index" "acceptance" {
  count = local.test_index ? 1 : 0

  name            = "tf-acc-${var.run_id}-index"
  version         = var.run_id
  connection_name = var.search_connection_name
  index_name      = var.search_index_name
  description     = "Acceptance revision ${var.revision}."
}

data "foundry_index" "acceptance" {
  count   = local.test_index ? 1 : 0
  name    = foundry_index.acceptance[0].name
  version = foundry_index.acceptance[0].version

  lifecycle {
    postcondition {
      condition     = self.version == var.run_id
      error_message = "Index lookup did not resolve the created latest version."
    }
  }
}
