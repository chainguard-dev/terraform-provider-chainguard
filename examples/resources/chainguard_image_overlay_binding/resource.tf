resource "chainguard_group" "example" {
  name = "example-group"
}

resource "chainguard_image_repo" "example" {
  parent_id = chainguard_group.example.id
  name      = "example-repo"
}

# An overlay can be bound to a given repo only once, so each binding
# below references its own overlay.
resource "chainguard_image_overlay" "base_tools" {
  parent_id = chainguard_group.example.id
  name      = "base-tools"
  packages  = ["curl", "jq"]
}

resource "chainguard_image_overlay" "debug_tools" {
  parent_id = chainguard_group.example.id
  name      = "debug-tools"
  packages  = ["strace", "gdb"]
}

resource "chainguard_image_overlay" "latest_extras" {
  parent_id = chainguard_group.example.id
  name      = "latest-extras"
  packages  = ["git"]
}

# Apply an overlay to every tag of the repo. Several ALL bindings may
# coexist on a repo when their overlays don't conflict.
resource "chainguard_image_overlay_binding" "all" {
  repo_id    = chainguard_image_repo.example.id
  overlay_id = chainguard_image_overlay.base_tools.id

  tag_selector {
    kind = "ALL"
  }
}

# Apply an overlay to all "-dev" tags of the repo.
resource "chainguard_image_overlay_binding" "dev_variant" {
  repo_id    = chainguard_image_repo.example.id
  overlay_id = chainguard_image_overlay.debug_tools.id

  tag_selector {
    kind         = "VARIANT"
    variant_type = "DEV"
  }
}

# Apply an overlay to two specific tags.
resource "chainguard_image_overlay_binding" "exact" {
  repo_id    = chainguard_image_repo.example.id
  overlay_id = chainguard_image_overlay.latest_extras.id

  tag_selector {
    kind = "EXACT"
    tags = ["latest", "latest-dev"]
  }
}
