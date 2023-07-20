# Configure the Chainguard provider and use the default console API URL
#  (https://console-api.enforce.dev)
# provider "chainguard" {}

# Configure the Chainguard provider and pass a specific console API URL.
provider "chainguard" {
  console_api = "https://console-api.example.com"
}
