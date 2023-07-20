data "chainguard_identity" "chainguardidentity" {
  issuer  = "https://auth.chainguard.dev/"
  subject = "idp-name|idp.user-id"
}
