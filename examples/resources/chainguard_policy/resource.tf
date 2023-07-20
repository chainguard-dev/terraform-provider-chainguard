resource "chainguard_policy" "fulcio" {
  parent_id   = "foo/bar"
  description = "images example.com/foo/* must be signed by user via public Fulcio"
  document = jsonencode({
    apiVersion = "policy.sigstore.dev/v1beta1"
    kind       = "ClusterImagePolicy"
    metadata = {
      name = "foo"
    }
    spec = {
      images = [{
        glob = "example.com/foo/*"
      }]
      authorities = [{
        keyless = {
          url = "https://fulcio.sigstore.dev"
          identities = [{
            issuer  = "https://accounts.google.com"
            subject = "user@chainguard.dev"
          }]
        }
      }]
    }
  })
}
