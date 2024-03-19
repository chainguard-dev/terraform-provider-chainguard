/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"fmt"
	"os"
	"testing"

	"chainguard.dev/sdk/uidp"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

type testRepo struct {
	parentID string
	name     string
	bundles  string
	readme   string
	synced   bool
	unique   bool
}

func TestImageRepo(t *testing.T) {
	parentID := os.Getenv("TF_ACC_GROUP_ID")
	name := "test-name"

	original := testRepo{
		parentID: parentID,
		name:     name,
	}

	// Add bundles and readme
	update1 := testRepo{
		parentID: parentID,
		name:     name,
		bundles:  `["a", "b", "c"]`,
		readme:   "# hello",
	}

	// Modify bundles and readme
	update2 := testRepo{
		parentID: parentID,
		name:     name,
		bundles:  `["x", "y", "z"]`,
		readme:   "# goodbye",
	}

	// Delete readme and bundles, add syncing
	update3 := testRepo{
		parentID: parentID,
		name:     name,
		synced:   true,
	}

	// Add unique tags
	update4 := testRepo{
		parentID: parentID,
		name:     name,
		synced:   true,
		unique:   true,
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
					resource.TestCheckNoResourceAttr(`chainguard_image_repo.example`, `bundles`),
					resource.TestCheckNoResourceAttr(`chainguard_image_repo.example`, `readme`),
				),
			},

			// ImportState testing.
			{
				ResourceName:      "chainguard_image_repo.example",
				ImportState:       true,
				ImportStateVerify: true,
			},

			// Update and Read testing. (1)
			{
				Config: testImageRepo(update1),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `parent_id`, parentID),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `name`, name),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `bundles.0`, "a"),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `bundles.1`, "b"),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `bundles.2`, "c"),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `readme`, "# hello"),
				),
			},

			// Update and Read testing. (2)
			{
				Config: testImageRepo(update2),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `parent_id`, parentID),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `name`, name),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `bundles.0`, "x"),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `bundles.1`, "y"),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `bundles.2`, "z"),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `readme`, "# goodbye"),
				),
			},

			// Update and Read testing. (3)
			{
				Config: testImageRepo(update3),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `parent_id`, parentID),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `name`, name),
					resource.TestCheckNoResourceAttr(`chainguard_image_repo.example`, `bundles`),
					resource.TestCheckNoResourceAttr(`chainguard_image_repo.example`, `readme`),
					resource.TestCheckResourceAttrWith(`chainguard_image_repo.example`, `sync_config.source`, func(value string) error {
						if !uidp.Valid(value) {
							return fmt.Errorf("not a UIDP: %q", value)
						}
						if uidp.Parent(value) != parentID {
							return fmt.Errorf("unexpected parent: %q, wanted %q", uidp.Parent(value), parentID)
						}
						return nil
					}),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `sync_config.unique_tags`, "false"),
				),
			},

			// Update and Read testing. (4)
			{
				Config: testImageRepo(update4),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `parent_id`, parentID),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `name`, name),
					resource.TestCheckResourceAttrWith(`chainguard_image_repo.example`, `sync_config.source`, func(value string) error {
						if !uidp.Valid(value) {
							return fmt.Errorf("not a UIDP: %q", value)
						}
						if uidp.Parent(value) != parentID {
							return fmt.Errorf("unexpected parent: %q, wanted %q", uidp.Parent(value), parentID)
						}
						return nil
					}),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `sync_config.unique_tags`, "true"),
				),
			},
		},
	})
}

func testImageRepo(repo testRepo) string {
	const tmpl = `
resource "chainguard_image_repo" "source" {
  parent_id = %q
  name      = "source-repo"
}

resource "chainguard_image_repo" "example" {
  parent_id = %q
  name      = %q
  %s
  %s
  %s
}
`
	var bundlesLine string
	if repo.bundles != "" {
		bundlesLine = fmt.Sprintf("bundles = %s", repo.bundles)
	}

	var readmeLine string
	if repo.readme != "" {
		readmeLine = fmt.Sprintf("readme = %q", repo.readme)
	}

	var syncLine string
	if repo.synced {
		syncLine = fmt.Sprintf(`sync_config {
  source      = chainguard_image_repo.source.id
  unique_tags = %t
}`, repo.unique)
	}

	return fmt.Sprintf(tmpl, repo.parentID, repo.parentID, repo.name, bundlesLine, readmeLine, syncLine)
}
