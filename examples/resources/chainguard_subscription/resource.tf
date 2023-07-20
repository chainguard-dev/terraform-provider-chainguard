resource "chainguard_subscription" "example" {
  parent_id = "foo/bar"
  sink      = "https://example.com/callback"
}
