/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

var (
	// testAccProtoV6ProviderFactories are used to instantiate a provider during
	// acceptance testing. The factory function will be invoked for every Terraform
	// CLI command executed to create a provider server to which the CLI can
	// reattach.
	testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
		"chainguard": providerserver.NewProtocol6WithError(New("acctest")()),
	}
)

func testAccPreCheck(t *testing.T) {
	m := "%s env var must be set to run acceptance tests"

	// TF_ACC environment variables must be set to run
	// acceptance tests.
	for _, v := range EnvAccVars {
		if os.Getenv(v) == "" {
			t.Fatalf(m, v)
		}
	}
}
