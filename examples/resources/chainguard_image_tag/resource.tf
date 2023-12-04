resource "chainguard_image_tag" "example" {
  name    = "v1.2.3"
  repo_id = "foo/bar"
  bundles = ["a", "b"]
}
