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
	original := testIDP{
		parentID:    os.Getenv("TF_ACC_GROUP_ID"),
		name:        acctest.RandString(10),
		description: acctest.RandString(10),
		defaultRole: "data.chainguard_role.viewer_test.items[0].id",
		oidc: oidc{
			issuer:           "https://example.com",
			clientID:         acctest.RandString(10),
			clientSecret:     acctest.RandString(10),
			additionalScopes: `["foo", "bar"]`,
		},
	}

	update := testIDP{
		parentID:    os.Getenv("TF_ACC_GROUP_ID"),
		name:        acctest.RandString(10),
		description: acctest.RandString(10),
		defaultRole: "data.chainguard_role.viewer_test.items[0].id",
		oidc: oidc{
			issuer:           "https://new.example.com",
			clientID:         acctest.RandString(10),
			clientSecret:     acctest.RandString(10),
			additionalScopes: `["email", "bar"]`,
		},
	}

	childpattern := regexp.MustCompile(fmt.Sprintf(`%s\/[a-z0-9]{16}`, original.parentID))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: accDataRoleViewer + testAccResourceIdentityProvider(original),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(`chainguard_identity_provider.example`, `parent_id`, original.parentID),
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
				Config: accDataRoleViewer + testAccResourceIdentityProvider(update),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(`chainguard_identity_provider.example`, `parent_id`, update.parentID),
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

func testAccResourceIdentityProvider(idp testIDP) string {
	const tmpl = `	
resource "chainguard_identity_provider" "example" {
  parent_id    = %q
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
