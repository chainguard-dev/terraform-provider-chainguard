/*
Copyright 2026 Chainguard, Inc.
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

func TestAccGroupDataSource(t *testing.T) {
	parentID := os.Getenv(EnvAccGroupID)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Lookup by ID.
			{
				Config: fmt.Sprintf(`
data "chainguard_group" "by_id" {
  id = %q
}
`, parentID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.chainguard_group.by_id", "id", parentID),
					resource.TestCheckResourceAttrSet("data.chainguard_group.by_id", "name"),
				),
			},
		},
	})
}

func TestAccGroupDataSource_InvalidID(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
data "chainguard_group" "bad" {
  id = "not-a-valid-uidp"
}
`,
				ExpectError: regexp.MustCompile(`valid UIDP`),
			},
		},
	})
}
