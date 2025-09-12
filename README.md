# Terraform Provider Chainguard

The Chainguard Terraform provider manages Chainguard resources (IAM groups,
identities, image repos, etc) using Terraform.

The provider is written to be compatible with the [Terraform Plugin Framework](https://developer.hashicorp.com/terraform/tutorials/providers-plugin-framework/providers-plugin-framework-provider)

## Configuring the Provider

Configure the provider in your Terraform config:

```terraform
terraform {
  required_providers {
    chainguard = { source = "chainguard-dev/chainguard" }
  }
}
```

By default, the provider will attempt to refresh your Chainguard token when it's expired. You can disable this with:

```terraform
provider "chainguard" {
  login_options {
    disabled = true
  }
}
```

Additional options include specifying an identity to assume when authenticating and a verified organization name
to use a custom identity provider rather than the Auth0 defaults (GitHub, GitLab, and Google).

Detailed documentation on all available resources can be found under `/docs`.

## Developing the Provider

### Requirements

- [Terraform](https://www.terraform.io/downloads.html) >= 1.5.x
- [Go](https://golang.org/doc/install) >= 1.21

If you wish to work on the provider, you'll first need
[Go](http://www.golang.org) installed on your machine.

### Using a locally compiled provider

If you'd like to compile the provider locally and use it instead
of pulling from the Terraform registry, you can configure your Terraform CLI to do so.

```bash
cat <<EOF > dev.tfrc
provider_installation {
  dev_overrides {
    "chainguard-dev/chainguard" = "/path/to/terraform-provider-chainguard"
  }
  direct {}
}
EOF

export TF_CLI_CONFIG_FILE=dev.tfrc
```

To compile the provider, run `go install`. This will build the provider and put
the provider binary in the `$GOPATH/bin` directory.

To generate or update documentation, run `go generate`.

### VSCode Debugger

You can use the provided `.vscode/launch.json` to debug the provider in VSCode. This will start a process that will output the environment variable `TF_REATTACH_PROVIDERS`. Copy and paste this into the shell where you will run `terraform` commands to attach the debugger to the provider process.

### Acceptance tests

In order to run the full suite of Acceptance tests, run

```sh
# Select an existing group id to root tests under
TF_ACC_GROUP_ID=foo
TF_ACC_CONSOLE_API=https://console-api.example.com
TF_ACC_AUDIENCE=https://console-api.example.com
TF_ACC_ISSUER=https://issuer.example.com

TF_ACC=1 go test ./... -v
```
