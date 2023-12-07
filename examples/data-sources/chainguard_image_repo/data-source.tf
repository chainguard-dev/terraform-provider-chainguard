# List all repos for a given organization or group
data "chainguard_image_repo" "repo" {
  parent_id = "foo/bar"
}

# Lookup a specific image repo by name
data "chainguard_image_repo" "repo" {
  parent_id = "foo/bar"
  name      = "nginx"
}
