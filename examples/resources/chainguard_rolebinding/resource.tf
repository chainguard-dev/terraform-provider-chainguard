resource "chainguard_rolebinding" "binding" {
  identity = chainguard_identity.user.id
  group    = "foo/bar"
  role     = data.chainguard_roles.owner.items[0].id
}
