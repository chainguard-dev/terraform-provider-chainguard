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

type testRepo struct {
	parentID   string
	name       string
	bundles    string
	readme     string
	tier       string
	aliases    string
	activeTags string
}

func TestImageRepo(t *testing.T) {
	parentID := os.Getenv("TF_ACC_GROUP_ID")
	name := acctest.RandString(10)

	original := testRepo{
		parentID: parentID,
		name:     name,
	}

	// Add bundles and readme and aliases and active_tags
	update1 := testRepo{
		parentID:   parentID,
		name:       name,
		bundles:    `["application", "base", "fips"]`,
		readme:     "# hello",
		tier:       "APPLICATION",
		aliases:    `["image:1", "image:2", "image:latest"]`,
		activeTags: `["tag1", "tag2", "tag3"]`,
	}

	// Modify bundles and readme and aliases and active_tags
	update2 := testRepo{
		parentID:   parentID,
		name:       name,
		bundles:    `["byol", "base", "featured"]`,
		readme:     "# goodbye",
		tier:       "BASE",
		aliases:    `["image:97", "image:98", "image:99"]`,
		activeTags: `["tag4", "tag5", "tag6"]`,
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
					resource.TestCheckNoResourceAttr(`chainguard_image_repo.example`, `aliases`),
					resource.TestCheckNoResourceAttr(`chainguard_image_repo.example`, `active_tags`),
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
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `bundles.0`, "application"),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `bundles.1`, "base"),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `bundles.2`, "fips"),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `readme`, "# hello"),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `tier`, "APPLICATION"),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `aliases.0`, "image:1"),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `aliases.1`, "image:2"),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `aliases.2`, "image:latest"),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `active_tags.0`, "tag1"),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `active_tags.1`, "tag2"),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `active_tags.2`, "tag3"),
				),
			},

			// Update and Read testing. (2)
			{
				Config: testImageRepo(update2),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `parent_id`, parentID),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `name`, name),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `bundles.0`, "byol"),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `bundles.1`, "base"),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `bundles.2`, "featured"),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `readme`, "# goodbye"),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `tier`, "BASE"),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `aliases.0`, "image:97"),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `aliases.1`, "image:98"),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `aliases.2`, "image:99"),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `active_tags.0`, "tag4"),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `active_tags.1`, "tag5"),
					resource.TestCheckResourceAttr(`chainguard_image_repo.example`, `active_tags.2`, "tag6"),
				),
			},
		},
	})
}

func testImageRepo(repo testRepo) string {
	const tmpl = `
resource "chainguard_image_repo" "example" {
  parent_id = %q
  name      = %q
  %s
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

	var tierLine string
	if repo.tier != "" {
		tierLine = fmt.Sprintf("tier = %q", repo.tier)
	}

	var aliasesLine string
	if repo.aliases != "" {
		aliasesLine = fmt.Sprintf("aliases = %s", repo.aliases)
	}

	var activeTagsLine string
	if repo.activeTags != "" {
		activeTagsLine = fmt.Sprintf("active_tags = %s", repo.activeTags)
	}

	return fmt.Sprintf(tmpl, repo.parentID, repo.name, bundlesLine, readmeLine, tierLine, aliasesLine, activeTagsLine)
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
