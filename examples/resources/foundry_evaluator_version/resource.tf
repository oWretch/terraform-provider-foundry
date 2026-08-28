# Evaluations are a preview feature; opt in on the provider block:
# provider "foundry" {
#   enable_preview = ["evaluations"]
# }

resource "foundry_evaluator_version" "relevance" {
  name        = "response-relevance"
  description = "Rates how relevant the agent's response is to the user's request."
  type        = "prompt"
  prompt_text = <<-EOT
    Rate the relevance of the response to the request on a scale of 1-5.

    Request: {{item.request}}
    Response: {{item.response}}
  EOT

  metrics = {
    relevance = "relevance"
  }
}

resource "foundry_evaluator_version" "custom_scorer" {
  name        = "custom-scorer"
  description = "A custom Python evaluator."
  type        = "code"
  code_text   = <<-EOT
    def evaluate(item):
        return {"score": 1 if item["response"] else 0}
  EOT

  metrics = {
    score = "score"
  }
}
