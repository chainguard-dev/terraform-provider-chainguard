resource "chainguard_sigstore" "example" {
  parent_id   = "foo/bar"
  name        = "my sigstore"
  description = "my personal sigstore"
  kms_ca {
    key_ref    = "awskms:///<aws-arn>"
    cert_chain = <<-EOF
       ------ CERTIFICATE ---------
       derpderp
       ------ END CERTIFICATE --------
    EOF
  }
}
