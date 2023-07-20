data "chainguard_cluster_discovery" "example" {
  id        = "foo/bar"
  profiles  = ["observer"]
  providers = ["CLOUD_RUN"]
}
