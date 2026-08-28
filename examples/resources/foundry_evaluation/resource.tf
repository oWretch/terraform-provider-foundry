# Evaluations are a preview feature; opt in on the provider block:
# provider "foundry" {
#   enable_preview = ["evaluations"]
# }

resource "foundry_evaluation" "support_quality" {
  name = "support-response-quality"

  data_source_item_schema = jsonencode({
    type = "object"
    properties = {
      request  = { type = "string" }
      response = { type = "string" }
    }
    required = ["request", "response"]
  })

  testing_criteria = [
    {
      name           = "relevance"
      evaluator_name = foundry_evaluator_version.relevance.name
      data_mapping = {
        request  = "{{item.request}}"
        response = "{{item.response}}"
      }
    },
  ]

  metadata = {
    team = "support"
  }
}
