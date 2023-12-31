---
# generated by https://github.com/hashicorp/terraform-plugin-docs
page_title: "chainguard_group Data Source - terraform-provider-chainguard"
subcategory: ""
description: |-
  Lookup a group with the given name.
---

# chainguard_group (Data Source)

Lookup a group with the given name.

## Example Usage

```terraform
# Fetch a root group by name
data "chainguard_dev" "root_group" {
  name = "my-root-group"
}

# Fetch a subgroup from a previously known group
data "chainguard_dev" "sub_group" {
  name      = "sub-group"
  parent_id = data.chainguard_dev.root_group.id
}
```

<!-- schema generated by tfplugindocs -->
## Schema

### Optional

- `id` (String) The exact UIDP of the group.
- `name` (String) The name of the group to lookup
- `parent_id` (String) The UIDP of the group in which to lookup the named group.

### Read-Only

- `description` (String) Description of the matched IAM group
