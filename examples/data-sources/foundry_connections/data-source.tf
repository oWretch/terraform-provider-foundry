data "foundry_connections" "available" {}

# Use a connection name to back a dataset or index.
output "storage_connection" {
  value = one([
    for connection in data.foundry_connections.available.connections :
    connection.name if connection.type == "AzureStorageAccount"
  ])
}
