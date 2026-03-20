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

func TestAccImageRepoDataSource(t *testing.T) {
	parentID := os.Getenv(EnvAccGroupID)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
data "chainguard_image_repo" "test" {
  parent_id = %q
}
`, parentID),
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
