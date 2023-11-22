/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func testAccResourceRole(group, subgroup, name, desc string, caps []string) string {
	tmpl := `
resource "chainguard_group" "subgroup" {
  parent_id = %q
  name = %q
}

resource "chainguard_role" "test" {
  parent_id = chainguard_group.subgroup.id
  name = %q
  description = %q
  capabilities = [%s]
}
`
	c := `"` + strings.Join(caps, `", "`) + `"`
	return fmt.Sprintf(tmpl, group, subgroup, name, desc, c)
}

func TestAccRoleResource(t *testing.T) {
	name := acctest.RandString(10)
	description := acctest.RandString(10)
	parent := os.Getenv(EnvAccGroupID)
	subgroup := acctest.RandString(10)
	caps := []string{"groups.list"}

	newName := acctest.RandString(10)
	newDescription := acctest.RandString(10)
	newCaps := []string{"groups.list", "policy.list"}

	childpattern := regexp.MustCompile(fmt.Sprintf(`%s\/[a-z0-9]{16}`, parent))
	grandchildpattern := regexp.MustCompile(fmt.Sprintf(`%s\/[a-z0-9]{16}\/[a-z0-9]{16}`, parent))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing.
			{
				Config: testAccResourceRole(parent, subgroup, name, description, caps),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("chainguard_role.test", "parent_id", childpattern),
					resource.TestCheckResourceAttr("chainguard_role.test", "name", name),
					resource.TestCheckResourceAttr("chainguard_role.test", "description", description),
					resource.TestCheckResourceAttr("chainguard_role.test", "capabilities.#", "1"),
					resource.TestMatchResourceAttr("chainguard_role.test", "id", grandchildpattern),
				),
			},

			// ImportState testing.
			{
				ResourceName:      "chainguard_role.test",
				ImportState:       true,
				ImportStateVerify: true,
			},

			// Update and Read testing.
			{
				Config: testAccResourceRole(parent, subgroup, newName, newDescription, newCaps),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("chainguard_role.test", "parent_id", childpattern),
					resource.TestCheckResourceAttr("chainguard_role.test", "name", newName),
					resource.TestCheckResourceAttr("chainguard_role.test", "description", newDescription),
					resource.TestCheckResourceAttr("chainguard_role.test", "capabilities.#", "2"),
					resource.TestMatchResourceAttr("chainguard_role.test", "id", grandchildpattern),
				),
			},

			// Delete testing automatically occurs in TestCase.
		},
	})
}
