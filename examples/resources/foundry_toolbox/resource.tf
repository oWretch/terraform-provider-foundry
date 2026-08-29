# Toolboxes are a preview feature; opt in on the provider block:
# provider "foundry" {
#   enable_preview = ["toolboxes", "skills"]
# }

resource "foundry_skill" "greeting" {
  name         = "greeting"
  description  = "Generate a personalized greeting for the user."
  instructions = "You are a friendly greeting assistant. Keep greetings brief and warm."
}

resource "foundry_toolbox" "my_toolbox" {
  name        = "my-toolbox"
  description = "Toolbox with web search and an attached skill"

  tools {
    type        = "web_search"
    description = "Search the web for current information"
  }

  tools {
    type         = "mcp"
    name         = "docs"
    server_label = "docs"
    server_url   = "https://example.com/mcp"
  }

  skills {
    name = foundry_skill.greeting.name
    # Omit version to follow the skill's default version, or pin it:
    # version = foundry_skill.greeting.default_version
  }
}
