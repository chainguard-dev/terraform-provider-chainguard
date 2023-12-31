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

func TestAccRolebindingResource(t *testing.T) {
	group := os.Getenv(EnvAccGroupID)
	subgroup := acctest.RandString(10)

	childpattern := regexp.MustCompile(fmt.Sprintf(`%s\/[a-z0-9]{16}`, group))
	grandchildpattern := regexp.MustCompile(fmt.Sprintf(`%s\/[a-z0-9]{16}\/[a-z0-9]{16}`, group))
	rootpattern := regexp.MustCompile(`[a-z0-9]{40}`)

	role := testAccResourceRole(group, subgroup, "role", "", []string{"groups.list"})
	viewer := accDataRoleViewer
	customRoleBinding := testAccResourceRoleBinding(group, "chainguard_group.subgroup.id", "chainguard_role.test.id")
	viewerRoleBinding := testAccResourceRoleBinding(group, "chainguard_group.subgroup.id", "data.chainguard_role.viewer_test.items.0.id")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing.
			{
				Config: role + customRoleBinding,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("chainguard_rolebinding.test", "group", childpattern),
					resource.TestMatchResourceAttr("chainguard_rolebinding.test", "identity", childpattern),
					resource.TestMatchResourceAttr("chainguard_rolebinding.test", "role", childpattern),
					resource.TestMatchResourceAttr("chainguard_rolebinding.test", "id", grandchildpattern),
				),
			},

			// ImportState testing.
			{
				ResourceName:      "chainguard_rolebinding.test",
				ImportState:       true,
				ImportStateVerify: true,
			},

			// Update and Read testing.
			{
				Config: viewer + role + viewerRoleBinding,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("chainguard_rolebinding.test", "group", childpattern),
					resource.TestMatchResourceAttr("chainguard_rolebinding.test", "identity", childpattern),
					resource.TestMatchResourceAttr("chainguard_rolebinding.test", "role", rootpattern),
					resource.TestMatchResourceAttr("chainguard_rolebinding.test", "id", grandchildpattern),
				),
			},

			// Delete testing automatically occurs in TestCase.
		},
	})

}

func testAccResourceRoleBinding(groupID, subgroup, roleID string) string {
	tmpl := `
resource "chainguard_identity" "user" {
  parent_id = %q
  name = "something"
  claim_match {
    issuer = "https://issuer.example.com"
    subject = "something:something:subject"
  }
}

resource "chainguard_rolebinding" "test" {
 identity = chainguard_identity.user.id
 group    = %s
 role     = %s
}
`
	return fmt.Sprintf(tmpl, groupID, subgroup, roleID)
}
