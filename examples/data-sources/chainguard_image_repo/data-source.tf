# Fetch a repo by name
data "chainguard_image_repo" "nginx" {
  parent_id = "foo/bar"
  name      = "nginx"
}
