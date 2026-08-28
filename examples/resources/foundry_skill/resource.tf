# Skills are a preview feature; opt in on the provider block:
# provider "foundry" {
#   enable_preview = ["skills"]
# }

resource "foundry_skill" "greeting" {
  name        = "greeting"
  description = "Generate a personalized greeting for the user."

  instructions = <<-EOT
    You are a friendly greeting assistant. Keep greetings brief and warm.
  EOT
}

# Alternatively, upload a SKILL.md file or a .zip archive containing one.
# Changing source_path, or the referenced file's contents when paired with
# source_hash, publishes a new skill version and promotes it to default.
resource "foundry_skill" "from_file" {
  name        = "code-review"
  source_path = "${path.module}/skills/code-review/SKILL.md"
  source_hash = filesha256("${path.module}/skills/code-review/SKILL.md")
}
