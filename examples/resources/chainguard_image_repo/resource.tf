resource "chainguard_image_repo" "example" {
  parent_id = "foo/bar"
  name      = "nginx"
  bundles   = ["application", "fips"]
}
