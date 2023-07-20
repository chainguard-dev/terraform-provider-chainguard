# Chainguard and GitHub Terraform Provider Configuration Example

This example demonstrates the use of the Chainguard and GitHub Terraform providers to manage role bindings in the Chainguard platform using the GitHub team members identities

## Prerequisites

To use this example, you'll need:

1. Terraform installed (version 1.x.x or higher)
2. A GitHub Personal Access Token (classic) with the appropriate permissions (minimum: org:read)
3. Access to the Chainguard platform and API

## Provider Configuration

### GitHub Provider

Configure the GitHub provider by setting the `github_token` variable and specifying the organization owner. This configuration assumes that the `GITHUB_TOKEN` environment variable is set with your GitHub Personal Access Token.

```hcl
variable "github_token" {
  type      = string
  sensitive = true
}

provider "github" {
  token = var.github_token
  owner = "chainguard-dev"
}
```

### Chainguard Provider

Configure the Chainguard provider with the Console API URL.

```hcl
provider "chainguard" {
  console_api = "https://console-api.enforce.dev"
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

### GitHub Team

Retrieve the GitHub team with the slug "platform".

```hcl
data "github_team" "platform" {
  slug = "platform"
}
```

### GitHub Team Members

Retrieve the GitHub team members for the platform team.

```hcl
data "github_user" "platform_team_members" {
  for_each = toset(data.github_team.platform.members)
  username = each.key
}
```

### Chainguard Identity for GitHub Users

Retrieve the Chainguard identity for each GitHub user in the platform team.

```hcl
data "chainguard_identity" "identities" {
  for_each = toset([for x in data.github_user.platform_team_members : x.id])

  issuer  = "https://auth.chainguard.dev/"
  subject = "github|${each.key}"
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
resource "chainguard_rolebinding" "cg-binding" {
  for_each = toset(local.filtered_identities)
  identity = each.value
  group    = "1a6bcc003e8fac173e2c60c98011c79f5d561901"
  role     = data.chainguard_roles.viewer.items[0].id
}
```

## Optional Configurations

These configurations optional and specific to certain users.

### Retrieve Information about a GitHub User

```hcl
data "github_user" "ajayk" {
  username = "ajayk"
}
```

### Retrieve the Chainguard Identity of the GitHub User

```hcl
data "chainguard_identity" "ajaykchainguardbinding" {
  issuer  = "https://auth.chainguard.dev/"
  subject = "github|${data.github_user.ajayk.id}"
}
```

### Grant Access to a Chainguard Group using the Identity

```hcl
resource "chainguard_rolebinding" "ajayk-binding" {
  identity = data.chainguard_identity.ajaykchainguardbinding.id
  group    = "1a6bcc003e8fac173e2c60c98011c79f5d561901"
  role     = data.chainguard_roles.viewer.items[0].id
}
```
