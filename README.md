# Terraform Provider Chainguard

The Chainguard Terraform provider manages Chainguard resources (IAM groups,
clusters, policy etc) using Terraform.

The provider is written to be compatible with the [Terraform Plugin Framework](https://developer.hashicorp.com/terraform/tutorials/providers-plugin-framework/providers-plugin-framework-provider)

## Requirements

-	[Terraform](https://www.terraform.io/downloads.html) >= 0.13.x
-	[Go](https://golang.org/doc/install) >= 1.17

## Building The Provider

1. Clone the repository
1. Enter the repository directory
1. Build the provider using the Go `install` command:

```sh
$ go install
```

## Adding Dependencies

This provider uses [Go modules](https://github.com/golang/go/wiki/Modules).
Please see the Go documentation for the most up to date information about using
Go modules.

To add a new dependency `github.com/author/dependency` to your Terraform
provider:

```
go get github.com/author/dependency
go mod tidy
```

Then commit the changes to `go.mod` and `go.sum`.

## Using the provider

The Chainguard provider isn't currently published to the Terraform registry but
it is publically accessible on a network_mirror. To use the network mirror add
the following to `~/.terraformrc` (or `%APPDATA/terraform.rc` on Windows):

```
# ~/.terraformrc
provider_installation {
  network_mirror {
    url = "https://storage.googleapis.com/us.artifacts.prod-enforce-fabc.appspot.com/terraform-provider/"
    # For staging, uncomment below
    # url = "https://storage.googleapis.com/us.artifacts.staging-enforce-cd1e.appspot.com/terraform-provider/"

    include = [
      "registry.terraform.io/chainguard-dev/chainguard",
    ]
  }

  direct {
    exclude = [
      "registry.terraform.io/chainguard-dev/chainguard",
    ]
  }
}
```

Once configured to use the mirror, configure the provider to use your
environment of choice by setting the `console_api` parameter (defaults to
`https://console-api.enforce.dev`).

```terraform
terraform {
  required_providers {
    chainguard = {
      source = "chainguard-dev/chainguard"
    }
  }
}

provider "chainguard" {
  console_api = "https://console-api.enforce.dev"
  # For staging, uncomment below
  # console_api = "https://console-api.chainops.dev"
}
```

Detailed documentation on all available resources can be found under
`/docs`.

### Using the provider as a developer

If you'd like to compile the provider locally and use it instead
of pulling from the Terraform registry (where it will eventually
be published), you can configure your Terraform CLI to do so.

```bash
cat <<EOF > dev.tfrc
provider_installation {
  dev_overrides {
    "chainguard-dev/chainguard" = "/path/to/terraform-provider-chainguard"
  }
}
EOF

export TF_CLI_CONFIG_FILE=dev.tfrc
```

## Developing the Provider

If you wish to work on the provider, you'll first need
[Go](http://www.golang.org) installed on your machine (see
[Requirements](#requirements) above).

To compile the provider, run `go install`. This will build the provider and put
the provider binary in the `$GOPATH/bin` directory.

To generate or update documentation, run `go generate`.

In order to run the full suite of Acceptance tests, run

```sh
# Select an existing group id to root tests under
TF_ACC_GROUP_ID=foo
TF_ACC_CONSOLE_API=https://console-api.example.com
TF_ACC_AUDIENCE=https://console-api.example.com
TF_ACC_ISSUER=https://issuer.example.com

# To test cluster resources point to a context in your
# kubeconfig. Must be kind cluster that is reachable
# from your saas environment
TF_ACC_KUBE_CONTEXT=bar

TF_ACC=1 go test ./... -v
```

*Note:* Acceptance tests create real resources, and often cost money to run.
