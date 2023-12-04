# This example demonstrates how to configure some of the login
# options to have this provider automatically launch a browser
# to authenticate with the Chainguard platform if a token is expired
# or missing.

terraform {
  required_providers {
    chainguard = {
      source = "chainguard-dev/chainguard"
    }
  }
}

provider "chainguard" {
  login_options {
    # Auth0 social connection names must be one of:
    # google-oauth2, github, gitlab
    auth0_connection = "google-oauth2"

    # Exact id of an identity to assume when authenticating.
    # Get this ID with chainctl iam identities list
    identity_id = "foo/bar"

    # Other supported options:
    #
    # Disable browser flow for authentication for workflows.
    # Authenticate with chainctl auth login
    # or https://github.com/chainguard-dev/actions/tree/main/setup-chainctl
    # disabled = true
    #
    # Exact id of an identity provider to user for authenticating
    # when using a custom configured identity provider
    # identity_provider_id = "foo/bar"
    #
    # Verified organization name to determine the configured
    # identity provider to use when authenticating, if different
    # from the default Auth0 IdPs.
    # organization_name = "my-company.org"
  }
}

data "chainguard_role" "viewer" {
  name   = "viewer"
  parent = "/"
}

output "viewer_role" {
  value = data.chainguard_role.viewer
}
