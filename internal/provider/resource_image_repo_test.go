/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

type testRepo struct {
	parentID string
	name     string
	bundles  string
	readme   string
}

func TestImageRepo(t *testing.T) {
	parentID := os.Getenv("TF_ACC_GROUP_ID")
	name := "test-name"

	original := testRepo{
		parentID: parentID,
		name:     name,
		bundles:  `["a", "b", "c"]`,
		readme:   "# hello",
	}

	update := testRepo{
		parentID: parentID,
		name:     name,
		bundles:  `["x", "y", "z"]`,
		readme:   "# goodbye",
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing.
			{
				Config: testImageRepo(original),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `parent_id`, parentID),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `name`, name),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `bundles.0`, "a"),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `bundles.1`, "b"),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `bundles.2`, "c"),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `readme`, "# hello"),
				),
			},

			// ImportState testing.
			{
				ResourceName:      "chainguard_image_repo.example",
				ImportState:       true,
				ImportStateVerify: true,
			},

			// Update and Read testing.
			{
				Config: testImageRepo(update),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `parent_id`, parentID),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `name`, name),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `bundles.0`, "x"),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `bundles.1`, "y"),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `bundles.2`, "z"),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `readme`, "# goodbye"),
				),
			},
		},
	})
}

func testImageRepo(repo testRepo) string {
	const tmpl = `
resource "chainguard_image_repo" "example" {
  parent_id   = %q
  name        = %q
  bundles     = %s
  readme      = %q
}
`
	return fmt.Sprintf(tmpl, repo.parentID, repo.name, repo.bundles, repo.readme)
}
