# Fetch a root group by name
data "chainguard_group" "root_group" {
  name = "my-root-group"
}

# Fetch a subgroup from a previously known group
data "chainguard_group" "sub_group" {
  name      = "sub-group"
  parent_id = data.chainguard_group.root_group.id
}
