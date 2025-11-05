resource "chainguard_role" "my-role" {
  parent_id   = "root/group"
  name        = "my-custom-role"
  description = "A user-managed IAM role."
  capabilities = [
    "groups.list",
    "repo.list"
  ]
}
