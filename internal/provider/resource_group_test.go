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

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func testAccResourceGroup(parent, name, description string) string {
	const tmpl = `
resource "chainguard_group" "test" {
	parent_id   = %q
	name 	    = %q
    description = %q
}
`
	return fmt.Sprintf(tmpl, parent, name, description)
}

func testAccResourceRootGroup(name, description string) string {
	const tmpl = `
resource "chainguard_group" "test" {
	name 	    = %q
    description = %q
}
`
	return fmt.Sprintf(tmpl, name, description)
}

func TestAccGroupResource(t *testing.T) {
	name := acctest.RandString(10)
	description := acctest.RandString(10)
	parent := os.Getenv(EnvAccGroupID)

	newName := acctest.RandString(10)
	newDescription := acctest.RandString(10)

	childpattern := regexp.MustCompile(fmt.Sprintf(`%s\/[a-z0-9]{16}`, parent))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing.
			{
				Config: testAccResourceGroup(parent, name, description),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("chainguard_group.test", "parent_id", parent),
					resource.TestCheckResourceAttr("chainguard_group.test", "name", name),
					resource.TestCheckResourceAttr("chainguard_group.test", "description", description),
					resource.TestMatchResourceAttr("chainguard_group.test", "id", childpattern),
				),
			},

			// ImportState testing.
			{
				ResourceName:      "chainguard_group.test",
				ImportState:       true,
				ImportStateVerify: true,
			},

			// Update and Read testing.
			{
				Config: testAccResourceGroup(parent, newName, newDescription),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("chainguard_group.test", "parent_id", parent),
					resource.TestCheckResourceAttr("chainguard_group.test", "name", newName),
					resource.TestCheckResourceAttr("chainguard_group.test", "description", newDescription),
					resource.TestMatchResourceAttr("chainguard_group.test", "id", childpattern),
				),
			},

			// Delete testing automatically occurs in TestCase.
		},
	})
}

func TestAccRootGroupResource(t *testing.T) {
	if os.Getenv(EnvAccAmbient) == "" && os.Getenv("TF_CHAINGUARD_IDENTITY_TOKEN") == "" {
		t.Skip("TF_CHAINGUARD_IDENTITY_TOKEN or TF_ACC_AMBIENT required for root group acceptance test")
	}
	name := acctest.RandString(10)
	description := acctest.RandString(10)

	newName := acctest.RandString(10)
	newDescription := acctest.RandString(10)

	rootPattern := regexp.MustCompile(`[a-z0-9]{40}`)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing.
			{
				Config: testAccResourceRootGroup(name, description),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("chainguard_group.test", "parent_id"),
					resource.TestCheckResourceAttr("chainguard_group.test", "name", name),
					resource.TestCheckResourceAttr("chainguard_group.test", "description", description),
					resource.TestMatchResourceAttr("chainguard_group.test", "id", rootPattern),
				),
			},

			// ImportState testing.
			{
				ResourceName:      "chainguard_group.test",
				ImportState:       true,
				ImportStateVerify: true,
			},

			// Update and Read testing.
			{
				Config: testAccResourceRootGroup(newName, newDescription),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("chainguard_group.test", "parent_id"),
					resource.TestCheckResourceAttr("chainguard_group.test", "name", newName),
					resource.TestCheckResourceAttr("chainguard_group.test", "description", newDescription),
					resource.TestMatchResourceAttr("chainguard_group.test", "id", rootPattern),
				),
			},

			// Delete testing automatically occurs in TestCase.
		},
	})
}

func TestAccRootGroupResourceWithSentinel(t *testing.T) {
	if os.Getenv(EnvAccAmbient) == "" && os.Getenv("TF_CHAINGUARD_IDENTITY_TOKEN") == "" {
		t.Skip("TF_CHAINGUARD_IDENTITY_TOKEN or TF_ACC_AMBIENT required for root group acceptance test")
	}
	name := acctest.RandString(10)
	description := acctest.RandString(10)
	parent := "/"

	newName := acctest.RandString(10)
	newDescription := acctest.RandString(10)

	rootPattern := regexp.MustCompile(`[a-z0-9]{40}`)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing.
			{
				Config: testAccResourceGroup(parent, name, description),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("chainguard_group.test", "parent_id", parent),
					resource.TestCheckResourceAttr("chainguard_group.test", "name", name),
					resource.TestCheckResourceAttr("chainguard_group.test", "description", description),
					resource.TestMatchResourceAttr("chainguard_group.test", "id", rootPattern),
				),
			},

			// ImportState testing.
			{
				ResourceName:      "chainguard_group.test",
				ImportState:       true,
				ImportStateVerify: true,
			},

			// Update and Read testing.
			{
				Config: testAccResourceGroup(parent, newName, newDescription),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("chainguard_group.test", "parent_id", parent),
					resource.TestCheckResourceAttr("chainguard_group.test", "name", newName),
					resource.TestCheckResourceAttr("chainguard_group.test", "description", newDescription),
					resource.TestMatchResourceAttr("chainguard_group.test", "id", rootPattern),
				),
			},

			// Delete testing automatically occurs in TestCase.
		},
	})
}
