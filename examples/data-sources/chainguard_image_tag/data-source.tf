# List all tags for a given repo
data "chainguard_image_tag" "tags" {
  repo_id = "foo/bar"
}

# Exclude tags of the form "sha256-*"
data "chainguard_image_tag" "tags" {
  repo_id           = "foo/bar"
  exclude_referrers = true
}

# List tags updated after a given date
data "chainguard_image_tag" "tags" {
  repo_id       = "foo/bar"
  updated_since = "2023-12-01T00:00:00Z"
}
