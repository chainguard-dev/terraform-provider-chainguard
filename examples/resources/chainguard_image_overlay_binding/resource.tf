resource "chainguard_group" "example" {
  name = "example-group"
}

resource "chainguard_image_repo" "example" {
  parent_id = chainguard_group.example.id
  name      = "example-repo"
}

resource "chainguard_image_overlay" "example" {
  parent_id = chainguard_group.example.id
  name      = "debug-tools"
  packages  = ["curl", "jq"]
}

# Apply the overlay to two specific tags.
resource "chainguard_image_overlay_binding" "exact" {
  repo_id    = chainguard_image_repo.example.id
  overlay_id = chainguard_image_overlay.example.id

  tag_selector {
    kind = "EXACT"
    tags = ["latest", "latest-dev"]
  }
}

# Apply the overlay to every tag of the repo (at most one ALL binding
# per repo).
resource "chainguard_image_overlay_binding" "all" {
  repo_id    = chainguard_image_repo.example.id
  overlay_id = chainguard_image_overlay.example.id

  tag_selector {
    kind = "ALL"
  }
}

# Apply the overlay to all "-dev" tags of the repo.
resource "chainguard_image_overlay_binding" "dev_variant" {
  repo_id    = chainguard_image_repo.example.id
  overlay_id = chainguard_image_overlay.example.id

  tag_selector {
    kind         = "VARIANT"
    variant_type = "DEV"
  }
}
