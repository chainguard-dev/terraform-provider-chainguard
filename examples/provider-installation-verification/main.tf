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
  #console_api = "https://console-api.enforce.dev"
  console_api = "https://console-api.cmdpdx.dev"
}
#data "chainguard_role" "viewer" {
#  name   = "viewer"
#  parent = "/"
#}
#
#output "viewer_role" {
#  value = data.chainguard_role.viewer
#}

# TODO(colin): remove temporary testing resources below
data "chainguard_group" "cmdpdx_root" {
  name = "cmdpdx.dev"
}

data "chainguard_group" "tf-test-data" {
  name      = "tf-test"
  parent_id = data.chainguard_group.cmdpdx_root.id
}

resource "chainguard_group" "tf-test" {
  name        = "tf-test"
  parent_id   = data.chainguard_group.cmdpdx_root.id
  description = "a test group of the new provider!"
}

resource "chainguard_group" "sub-tf-test" {
  name        = "tf-test"
  parent_id   = chainguard_group.tf-test.id
  description = "a test (sub) group of the new provider!"
}

output "tf-test" {
  value = chainguard_group.tf-test
}

output "sub-tf-test" {
  value = chainguard_group.sub-tf-test
}
