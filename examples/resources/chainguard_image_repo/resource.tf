resource "chainguard_image_repo" "example" {
  parent_id = "foo/bar"
  name      = "nginx"
  bundles   = ["application", "fips"]
}

# A repo with a custom assembly overlay. Requires custom assembly to be
# enabled for the repo; images are rebuilt asynchronously after changes.
resource "chainguard_image_repo" "custom" {
  parent_id = "foo/bar"
  name      = "custom-nginx"

  sync_config {
    source = "nginx"
  }

  custom_overlay {
    contents {
      packages = ["curl", "jq"]
    }

    environment = {
      "HTTP_PROXY" = "http://proxy.example.com:3128"
    }

    annotations = {
      "com.example.team" = "platform"
    }

    accounts {
      run_as = "65532"

      user {
        username = "app"
        uid      = 1000
        gid      = 1000
      }

      group {
        groupname = "app"
        gid       = 1000
      }
    }
  }
}
