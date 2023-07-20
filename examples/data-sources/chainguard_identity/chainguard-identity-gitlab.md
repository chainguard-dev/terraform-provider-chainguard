# Chainguard and GitLab Terraform Provider Configuration Example

This example demonstrates the use of the Chainguard and GitLab Terraform providers to manage role bindings in the Chainguard platform using the GitLab group members.

## Prerequisites

To use this example, you'll need:

1. Terraform installed (version 1.x.x or higher)
2. A GitLab Personal Access Token with the appropriate permissions
3. Access to the Chainguard platform and API

## Provider Configuration

### GitLab Provider

Configure the GitLab provider by setting the `gitlab_token` variable. This configuration assumes that the `GITLAB_TOKEN` environment variable is set with your GitLab Personal Access Token.

```hcl
variable "gitlab_token" {
  type      = string
  sensitive = true
}

provider "gitlab" {
  token = var.gitlab_token
}
```

### Chainguard Provider

Configure the Chainguard provider with the Console API URL.

```hcl
provider "chainguard" {
  console_api = "https://console-api.nocve.xyz"
}
```

## Data Sources

### Predefined Viewer Role

Retrieve the predefined viewer role from Chainguard.

```hcl
data "chainguard_role" "viewer" {
  name = "viewer"
}
```

### GitLab Group Membership

Fetch the GitLab group members for the group with the full path "ajay-cg-test".

```hcl
data "gitlab_group_membership" "ajay-cg-test" {
  full_path = "ajay-cg-test"
}
```

### Chainguard Identity for GitLab Users

Convert the GitLab group members into Chainguard identities.

```hcl
data "chainguard_identity" "identities" {
  for_each = toset([for x in data.gitlab_group_membership.ajay-cg-test.members : tostring(x.id)])
  issuer   = "https://auth.chainguard.dev/"
  subject  = "oauth2|gitlab|${each.key}"
}
```

## Local Variables

Handle invitees who haven't logged in to accept the invite, as their Chainguard identity will not be created.

```hcl
locals {
  identities_with_ids = [
    for identity in data.chainguard_identity.identities : try(identity.id, null)
  ]
  filtered_identities = compact(local.identities_with_ids)
}
```

## Resources

### Chainguard Role Binding

Grant access to a Chainguard group using the identity.

```hcl
resource "chainguard_rolebinding" "binding" {
  for_each = toset(local.filtered_identities)
  identity = each.value
  group    = "1a6bcc003e8fac173e2c60c98011c79f5d561901"
  role     = data.chainguard_roles.viewer.items[0].id
}
```

### Direct User Lookup

We can also grant access using direct user lookup. First, fetch the GitLab user with the username "nfsmith".

```hcl
data "gitlab_user" "nfsmith" {
  username = "nfsmith"
}
```

Next, retrieve the Chainguard identity for the GitLab user.

```hcl
data "chainguard_identity" "nfsmith" {
  issuer  = "https://auth.chainguard.dev/"
  subject = "oauth2|gitlab|${data.gitlab_user.nfsmith.id}"
}
```

Finally, grant access to a Chainguard group using the identity.

```hcl
resource "chainguard_rolebinding" "nfsmith-binding" {
  identity = data.chainguard_identity.nfsmith.id
  group    = "1a6bcc003e8fac173e2c60c98011c79f5d561901"
  role     = data.chainguard_roles.viewer.items[0].id
}
```
