# Files are identified by a service-assigned ID rather than by name, so a list
# is the only way to reference a file this configuration did not upload.
data "foundry_files" "assistants" {
  purpose = "assistants"
}

# Filenames are not unique, so pick deliberately rather than assuming one match.
locals {
  handbook_ids = [
    for file in data.foundry_files.assistants.files :
    file.id if file.filename == "handbook.pdf"
  ]
}

output "handbook_file_id" {
  value = one(local.handbook_ids)
}
