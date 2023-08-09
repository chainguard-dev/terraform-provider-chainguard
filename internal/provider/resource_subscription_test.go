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

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccResourceSubscription(t *testing.T) {
	parent := os.Getenv("TF_ACC_GROUP_ID")
	sink := `https://localhost/callback`
	childpattern := regexp.MustCompile(fmt.Sprintf(`%s\/[a-z0-9]{16}`, parent))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccResourceSubscription(parent, sink),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(`chainguard_subscription.example`, `parent_id`, parent),
					resource.TestCheckResourceAttr(`chainguard_subscription.example`, `sink`, sink),
					resource.TestMatchResourceAttr(`chainguard_subscription.example`, `id`, childpattern),
				),
			},
			// ImportState testing.
			{
				ResourceName:      "chainguard_subscription.example",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccResourceSubscription(parent, sink string) string {
	const tmpl = `	
resource "chainguard_subscription" "example" {
  parent_id   = "%s"
  sink        = "%s"
}
`
	return fmt.Sprintf(tmpl, parent, sink)
}
