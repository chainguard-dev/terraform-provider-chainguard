# Example managed root group.
resource "chainguard_group" "example" {
  name        = "example-root"
  description = "My example root group."
}

# Example managed sub-group.
resource "chainguard_group" "example_sub" {
  name        = "example-sub-group"
  description = "My example sub group."
  parent_id   = chainguard_group.example.id
}
