resource "chainguard_group_invite" "example" {
  group      = "foo/bar"
  role       = "role-id"
  expiration = "2024-01-01T00:00:00"
}
