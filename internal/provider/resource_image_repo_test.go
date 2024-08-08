/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"chainguard.dev/sdk/uidp"
)

type testRepo struct {
	parentID string
	name     string
	bundles  string
	readme   string
	synced   bool
	unique   bool
	apks     bool
	tier     string
}

func TestImageRepo(t *testing.T) {
	parentID := os.Getenv("TF_ACC_GROUP_ID")
	name := acctest.RandString(10)

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
		tier:     "APPLICATION",
	}

	// Modify bundles and readme
	update2 := testRepo{
		parentID: parentID,
		name:     name,
		bundles:  `["x", "y", "z"]`,
		readme:   "# goodbye",
		tier:     "BASE",
	}

	// Delete readme and bundles, add syncing
	update3 := testRepo{
		parentID: parentID,
		name:     name,
		synced:   true,
		apks:     true,
	}

	// Add unique tags
	update4 := testRepo{
		parentID: parentID,
		name:     name,
		synced:   true,
		unique:   true,
		apks:     false,
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
					resource.TestCheckNoResourceAttr(`chainguard_image_repo.example`, `tier`),
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
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `tier`, "APPLICATION"),
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
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `tier`, "BASE"),
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
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `sync_config.sync_apks`, "true"),
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
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `sync_config.sync_apks`, "false"),
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
  expiration  = %q
  unique_tags = %t
  sync_apks   = %t
}`, time.Now().Add(24*time.Hour).UTC().Format(time.RFC3339), repo.unique, repo.apks)
	}

	var tierLine string
	if repo.tier != "" {
		tierLine = fmt.Sprintf("tier = %q", repo.tier)
	}

	return fmt.Sprintf(tmpl, repo.parentID, repo.parentID, repo.name, bundlesLine, readmeLine, syncLine, tierLine)
}

// Multiple equivalent concurrent updates should not cause errors.
func TestImageRepo_ConcurrentUpdates(t *testing.T) {
	parentID := os.Getenv("TF_ACC_GROUP_ID")
	name := acctest.RandString(10)

	// One apply to create it.
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: fmt.Sprintf(`
		resource "chainguard_image_repo" "repo" {
		  parent_id = %q
		  name      = %q
		  readme    = "# hello"
		}
		`, parentID, name),
			Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr(`chainguard_image_repo.repo`, `parent_id`, parentID),
				resource.TestCheckResourceAttr(`chainguard_image_repo.repo`, `name`, name),
			),
		}},
	})

	// N concurrent updates, with no changes.
	for i := 0; i < 10; i++ {
		t.Run("concurrent_updates", func(t *testing.T) {
			t.Parallel()

			resource.Test(t, resource.TestCase{
				PreCheck:                 func() { testAccPreCheck(t) },
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Steps: []resource.TestStep{{
					Config: fmt.Sprintf(`
				resource "chainguard_image_repo" "repo" {
				  parent_id = %q
				  name      = %q
				  readme    = "# hello"
				}
				`, parentID, name),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr(`chainguard_image_repo.repo`, `parent_id`, parentID),
						resource.TestCheckResourceAttr(`chainguard_image_repo.repo`, `name`, name),
					),
				}},
			})
		})
	}
}
