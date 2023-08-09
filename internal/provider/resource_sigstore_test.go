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

func TestAccResourceSigstore(t *testing.T) {
	parent := os.Getenv("TF_ACC_GROUP_ID")

	name := acctest.RandString(10)
	description := acctest.RandString(10)

	newName := acctest.RandString(10)
	newDescription := acctest.RandString(10)

	childpattern := regexp.MustCompile(fmt.Sprintf(`%s\/[a-z0-9]{16}`, parent))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccResourceSigstore(parent, name, description),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(`chainguard_sigstore.example`, `parent_id`, parent),
					resource.TestCheckResourceAttr(`chainguard_sigstore.example`, `name`, name),
					resource.TestCheckResourceAttr(`chainguard_sigstore.example`, `description`, description),
					resource.TestMatchResourceAttr(`chainguard_sigstore.example`, `id`, childpattern),
				),
			},
			// ImportState testing.
			{
				ResourceName:      "chainguard_sigstore.example",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccResourceSigstore(parent, newName, newDescription),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(`chainguard_sigstore.example`, `parent_id`, parent),
					resource.TestCheckResourceAttr(`chainguard_sigstore.example`, `name`, newName),
					resource.TestCheckResourceAttr(`chainguard_sigstore.example`, `description`, newDescription),
					resource.TestMatchResourceAttr(`chainguard_sigstore.example`, `id`, childpattern),
				),
			},
		},
	})
}

func testAccResourceSigstore(parent, name, description string) string {
	const tmpl = `	
resource "chainguard_sigstore" "example" {
  parent_id   = %q
  name        = %q
  description = %q
  kms_ca {
    key_ref = "foobar"
    cert_chain = "foobarerer"
  }
}
`
	return fmt.Sprintf(tmpl, parent, name, description)
}
