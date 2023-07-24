# This example assumes the user has a Chainguard token with
# permission to list IAM roles.
# To obtain a Chainguard token, login with chainctl:
#   chainctl auth login

terraform {
  required_providers {
    chainguard = {
      source = "chainguard-dev/chainguard"
    }
  }
}

provider "chainguard" {
  console_api = "https://console-api.enforce.dev"
}
data "chainguard_role" "viewer" {
  name   = "viewer"
  parent = "/"
}

output "viewer_role" {
  value = data.chainguard_role.viewer
}
