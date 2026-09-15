resource "chainguard_group" "example" {
  name = "example-group"
}

# A reusable Custom Assembly overlay, owned by a group. Attach it to
# image repos with chainguard_image_overlay_binding.
resource "chainguard_image_overlay" "example" {
  parent_id = chainguard_group.example.id
  name      = "debug-tools"
  packages  = ["curl", "jq"]
}
