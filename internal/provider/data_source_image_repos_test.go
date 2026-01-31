/*
Copyright 2025 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccImageReposDataSource validates the image_repos data source works correctly.
func TestAccImageReposDataSource(t *testing.T) {
	parentID := os.Getenv(EnvAccGroupID)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
data "chainguard_image_repos" "test" {
  parent_id = %q
}
`, parentID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.chainguard_image_repos.test", "id"),
					resource.TestCheckResourceAttrSet("data.chainguard_image_repos.test", "items.#"),
				),
			},
		},
	})
}
