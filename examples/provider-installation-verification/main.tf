# This example assumes the user has a Chainguard token with
# permission to list IAM roles.

terraform {
  required_providers {
    chainguard = {
      source = "chainguard-dev/chainguard"
    }
  }
}

data "chainguard_role" "viewer" {
  name   = "viewer"
  parent = "/"
}

output "viewer_role" {
  value = data.chainguard_role.viewer
}
