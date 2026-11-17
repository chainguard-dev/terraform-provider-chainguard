# List all Chainguard managed roles.
data "chainguard_role" "managed" {
  parent = "/"
}

# Look up the owner role
data "chainguard_role" "owner_role" {
  name   = "owner"
  parent = "/"
}
