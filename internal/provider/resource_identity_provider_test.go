/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

type oidc struct {
	issuer           string
	clientID         string
	clientSecret     string
	additionalScopes string
}

type testIDP struct {
	parentID    string
	name        string
	description string
	defaultRole string
	oidc        oidc
}

func TestAccResourceIdentityProvider(t *testing.T) {
	clients := testAccPlatformClient(t)
	parentID := os.Getenv("TF_ACC_GROUP_ID")
	subgroupName := acctest.RandString(10)

	original := testIDP{
		// Use a reference to the child group created in the HCL below,
		// not the root test group, to avoid "already has an IDP" collisions
		// when multiple matrix jobs run in parallel.
		parentID:    "chainguard_group.idp_test.id",
		name:        acctest.RandString(10),
		description: acctest.RandString(10),
		defaultRole: "data.chainguard_role.viewer_test.items[0].id",
		oidc: oidc{
			issuer:           "https://justtrustme.dev",
			clientID:         acctest.RandString(10),
			clientSecret:     acctest.RandString(10),
			additionalScopes: `["foo", "bar"]`,
		},
	}

	update := testIDP{
		parentID:    "chainguard_group.idp_test.id",
		name:        acctest.RandString(10),
		description: acctest.RandString(10),
		defaultRole: "data.chainguard_role.viewer_test.items[0].id",
		oidc: oidc{
			issuer:           "https://justtrustme.dev",
			clientID:         acctest.RandString(10),
			clientSecret:     acctest.RandString(10),
			additionalScopes: `["email", "bar"]`,
		},
	}

	childpattern := regexp.MustCompile(fmt.Sprintf(`%s\/[a-z0-9]{16}`, parentID))

	subgroupHCL := fmt.Sprintf(`
resource "chainguard_group" "idp_test" {
  parent_id = %q
  name      = %q
}
`, parentID, subgroupName)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkIdentityProviderDestroy(clients),
		Steps: []resource.TestStep{
			{
				Config: accDataRoleViewer + subgroupHCL + testAccResourceIdentityProviderRef(original),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(`chainguard_identity_provider.example`, `parent_id`),
					resource.TestCheckResourceAttr(`chainguard_identity_provider.example`, `name`, original.name),
					resource.TestCheckResourceAttr(`chainguard_identity_provider.example`, `description`, original.description),
					resource.TestCheckResourceAttr(`chainguard_identity_provider.example`, `oidc.issuer`, original.oidc.issuer),
					resource.TestCheckResourceAttr(`chainguard_identity_provider.example`, `oidc.client_id`, original.oidc.clientID),
					resource.TestCheckResourceAttr(`chainguard_identity_provider.example`, `oidc.client_secret`, original.oidc.clientSecret),
					resource.TestMatchResourceAttr(`chainguard_identity_provider.example`, `id`, childpattern),
				),
			},
			// ImportState testing.
			{
				ResourceName:            "chainguard_identity_provider.example",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"oidc.client_secret"},
			},
			{
				Config: accDataRoleViewer + subgroupHCL + testAccResourceIdentityProviderRef(update),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(`chainguard_identity_provider.example`, `parent_id`),
					resource.TestCheckResourceAttr(`chainguard_identity_provider.example`, `name`, update.name),
					resource.TestCheckResourceAttr(`chainguard_identity_provider.example`, `description`, update.description),
					resource.TestCheckResourceAttr(`chainguard_identity_provider.example`, `oidc.issuer`, update.oidc.issuer),
					resource.TestCheckResourceAttr(`chainguard_identity_provider.example`, `oidc.client_id`, update.oidc.clientID),
					resource.TestCheckResourceAttr(`chainguard_identity_provider.example`, `oidc.client_secret`, update.oidc.clientSecret),
					resource.TestMatchResourceAttr(`chainguard_identity_provider.example`, `id`, childpattern),
				),
			},
		},
	})
}

// testAccResourceIdentityProviderRef generates HCL where parent_id is a Terraform
// reference (e.g. chainguard_group.idp_test.id) rather than a literal string.
func testAccResourceIdentityProviderRef(idp testIDP) string {
	const tmpl = `
resource "chainguard_identity_provider" "example" {
  parent_id    = %s
  name         = %q
  description  = %q
  default_role = %s
  oidc {
    issuer            = %q
    client_id         = %q
    client_secret     = %q
    additional_scopes = %s
  }
}
`
	return fmt.Sprintf(tmpl, idp.parentID, idp.name, idp.description, idp.defaultRole, idp.oidc.issuer, idp.oidc.clientID, idp.oidc.clientSecret, idp.oidc.additionalScopes)
}
