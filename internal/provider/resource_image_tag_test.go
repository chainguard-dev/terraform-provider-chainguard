/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

type testTag struct {
	parentID string
	name     string
	bundles  string
}

func TestImageTag(t *testing.T) {
	parentID := os.Getenv("TF_ACC_GROUP_ID")
	name := acctest.RandString(10)

	original := testTag{
		parentID: parentID,
		name:     name,
		bundles:  `["aa", "bb", "cc"]`,
	}

	update := testTag{
		parentID: parentID,
		name:     name,
		bundles:  `["xx", "yy", "zz"]`,
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing.
			{
				Config: testImageTag(original),
				Check: resource.ComposeTestCheckFunc(
					// We do not check the repo_id because it will be dynamic based on chainguard_image_repo.tag_example
					resource.TestCheckResourceAttr(`chainguard_image_tag.tag_example`, `name`, name),
					resource.TestCheckResourceAttr(`chainguard_image_tag.tag_example`, `bundles.0`, "aa"),
					resource.TestCheckResourceAttr(`chainguard_image_tag.tag_example`, `bundles.1`, "bb"),
					resource.TestCheckResourceAttr(`chainguard_image_tag.tag_example`, `bundles.2`, "cc"),
				),
			},
			// ImportState testing.
			{
				ResourceName:      "chainguard_image_tag.tag_example",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Update and Read testing.
			{
				Config: testImageTag(update),
				Check: resource.ComposeTestCheckFunc(
					// We do not check the repo_id because it will be dynamic based on chainguard_image_repo.tag_example
					resource.TestCheckResourceAttr(`chainguard_image_tag.tag_example`, `name`, name),
					resource.TestCheckResourceAttr(`chainguard_image_tag.tag_example`, `bundles.0`, "xx"),
					resource.TestCheckResourceAttr(`chainguard_image_tag.tag_example`, `bundles.1`, "yy"),
					resource.TestCheckResourceAttr(`chainguard_image_tag.tag_example`, `bundles.2`, "zz"),
				),
			},
		},
	})
}

func testImageTag(tag testTag) string {
	const tmpl = `
# a repo must first be created in order to get a parent_id for tag
resource "chainguard_image_repo" "tag_example" {
  parent_id   = %q
  name        = %q
  bundles     = %s
}

resource "chainguard_image_tag" "tag_example" {
  repo_id   = chainguard_image_repo.tag_example.id
  name        = %q
  bundles     = %s
}
`
	return fmt.Sprintf(tmpl, tag.parentID, tag.name, tag.bundles, tag.name, tag.bundles)
}
