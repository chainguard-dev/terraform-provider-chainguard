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

func testAccResourceGroup(parent, name, description string) string {
	const tmpl = `
resource "chainguard_group" "test" {
	parent_id   = %q
	name 	    = %q
    description = %q
}
`
	return fmt.Sprintf(tmpl, parent, name, description)
}

func TestAccGroupResource(t *testing.T) {
	name := acctest.RandString(10)
	description := acctest.RandString(10)
	parent := os.Getenv(EnvAccGroupID)

	newName := acctest.RandString(10)
	newDescription := acctest.RandString(10)

	childpattern := regexp.MustCompile(fmt.Sprintf(`%s\/[a-z0-9]{16}`, parent))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing.
			{
				Config: providerConfig + testAccResourceGroup(parent, name, description),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("chainguard_group.test", "parent_id", parent),
					resource.TestCheckResourceAttr("chainguard_group.test", "name", name),
					resource.TestCheckResourceAttr("chainguard_group.test", "description", description),
					resource.TestMatchResourceAttr("chainguard_group.test", "id", childpattern),
				),
			},

			// ImportState testing.
			{
				ResourceName:      "chainguard_group.test",
				ImportState:       true,
				ImportStateVerify: true,
			},

			// Update and Read testing.
			{
				Config: providerConfig + testAccResourceGroup(parent, newName, newDescription),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("chainguard_group.test", "parent_id", parent),
					resource.TestCheckResourceAttr("chainguard_group.test", "name", newName),
					resource.TestCheckResourceAttr("chainguard_group.test", "description", newDescription),
					resource.TestMatchResourceAttr("chainguard_group.test", "id", childpattern),
				),
			},

			// Delete testing automatically occurs in TestCase.
		},
	})
}
