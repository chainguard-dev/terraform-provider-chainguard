# List all Chainguard managed roles.
data "chainguard_roles" "managed" {
  parent = "/"
}

# Look up the owner role
data "chainguard_roles" "owner_role" {
  name   = "owner"
  parent = "/"
}
