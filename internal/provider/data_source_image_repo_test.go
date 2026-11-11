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

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccImageRepoDataSource(t *testing.T) {
	parentID := os.Getenv(EnvAccGroupID)
	name := acctest.RandString(10)

	// The data source fails when it finds no repo under parent_id, so create
	// one rather than relying on whatever the group already holds.
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "chainguard_image_repo" "test" {
  parent_id = %q
  name      = %q
}

data "chainguard_image_repo" "test" {
  parent_id  = %q
  depends_on = [chainguard_image_repo.test]
}
`, parentID, name, parentID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.chainguard_image_repo.test", "items.#"),
				),
			},
		},
	})
}

func TestAccImageRepoDataSource_InvalidParentID(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
data "chainguard_image_repo" "bad" {
  parent_id = "not-valid"
}
`,
				ExpectError: regexp.MustCompile(`valid UIDP`),
			},
		},
	})
}
