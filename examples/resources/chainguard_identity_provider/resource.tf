resource "chainguard_identity_provider" "example" {
  parent_id   = "foo/bar"
  name        = "my customer idp"
  description = "for my team"
  oidc {
    issuer            = "https://issuer.example.com"
    cliend_id         = "client id"
    client_secret     = "client secret"
    additional_scopes = ["email", "profile", "oidc"]
  }
}
