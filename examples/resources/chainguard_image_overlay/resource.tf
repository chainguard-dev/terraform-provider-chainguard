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

# The full overlay configuration shape. config supersedes packages:
# exactly one of the two may be set.
resource "chainguard_image_overlay" "full_example" {
  parent_id = chainguard_group.example.id
  name      = "corp-base"
  config = jsonencode({
    contents = {
      packages = ["curl"]
    }
    environment = {
      TZ = "UTC"
    }
    annotations = {
      "com.example/team" = "platform"
    }
    accounts = {
      run_as = "65532"
    }
    certificates = {
      additional = [{
        name    = "corp-ca"
        content = file("corp-ca.pem")
      }]
    }
  })
}
