resource "chainguard_image_repo_deployment" "example" {
  # The UIDP of the repository this deployment configuration applies to
  id = "example-repo-uidp-goes-here"

  # List of Helm charts for deployment
  charts = [
    {
      # OCI-based Helm chart
      repo   = "oci://ghcr.io/stefanprodan/charts/podinfo"
      source = "https://github.com/stefanprodan/podinfo"
    },
    {
      # Traditional Helm repository
      repo = "https://kyverno.github.io/kyverno/"
      # source is optional - only include if you want to reference the source code
    },
    {
      # Another example with a different chart
      repo   = "oci://registry-1.docker.io/bitnamicharts/nginx"
      source = "https://github.com/bitnami/charts/tree/main/bitnami/nginx"
    }
  ]
}

# Example with graceful error handling - useful when deployment failures
# shouldn't block other operations like image builds
resource "chainguard_image_repo_deployment" "example_with_error_handling" {
  id = "another-repo-uidp-goes-here"

  # Enable graceful error handling
  ignore_errors = true

  charts = [
    {
      repo   = "oci://ghcr.io/stefanprodan/charts/podinfo"
      source = "https://github.com/stefanprodan/podinfo"
    }
  ]
}