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
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccResourceGroupInvite(t *testing.T) {
	group := os.Getenv("TF_ACC_GROUP_ID")

	role := "viewer"
	newRole := "owner"

	childpattern := regexp.MustCompile(fmt.Sprintf(`%s\/[a-z0-9]{16}`, group))

	b64pattern := regexp.MustCompile(`[0-9a-fA-F+/=]+`)

	expiration := time.Now().Add(3 * time.Hour).Format(time.RFC3339)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccResourceGroupInvite(group, role, expiration),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_group_invite.invite`, `id`, childpattern),
					resource.TestMatchResourceAttr(`chainguard_group_invite.invite`, `code`, b64pattern),
				),
			},
			{
				Config: testAccResourceGroupInvite(group, newRole, expiration),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_group_invite.invite`, `id`, childpattern),
					resource.TestMatchResourceAttr(`chainguard_group_invite.invite`, `code`, b64pattern),
				),
			},
		},
	})
}

func testAccResourceGroupInvite(group, role, expiration string) string {
	tmpl := `
data "chainguard_role" %q {
  name = %q
}

resource "chainguard_group_invite" "invite" {
  group      = %q
  role       = data.chainguard_role.%s.items[0].id
  expiration = %q
}
`
	return fmt.Sprintf(
		tmpl,
		role,
		role,
		group,
		role,
		expiration,
	)
}
