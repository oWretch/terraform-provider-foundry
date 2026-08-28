resource "foundry_file" "manual" {
  source_path = "${path.module}/manual.pdf"
  purpose     = "assistants"
  source_hash = filesha256("${path.module}/manual.pdf")
}
