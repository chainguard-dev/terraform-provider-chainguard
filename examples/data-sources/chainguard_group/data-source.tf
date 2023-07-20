# Fetch a root group by name
data "chainguard_dev" "root_group" {
  name = "my-root-group"
}

# Fetch a subgroup from a previously known group
data "chainguard_dev" "sub_group" {
  name      = "sub-group"
  parent_id = data.chainguard_dev.root_group.id
}
