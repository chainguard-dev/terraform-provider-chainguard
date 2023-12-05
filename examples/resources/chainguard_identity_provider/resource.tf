resource "chainguard_identity_provider" "example" {
  parent_id   = "foo/bar"
  name        = "my customer idp"
  description = "for my team"
  # The default role must be a built in role,
  # or belong to the parent group or one of its ancestors.
  default_role = "foo/bar/role-id"
  oidc {
    issuer            = "https://issuer.example.com"
    cliend_id         = "client id"
    client_secret     = "client secret"
    additional_scopes = ["email", "profile", "oidc"]
  }
}
