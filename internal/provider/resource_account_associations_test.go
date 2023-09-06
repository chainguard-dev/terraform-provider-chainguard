/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"fmt"
	"os"
	"testing"

	"chainguard.dev/api/pkg/uidp"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccResourceAccountAssociations(t *testing.T) {
	awsAccount := "123456789012"
	googleProjectID := acctest.RandString(10)
	googleProjectNumber := acctest.RandString(10)

	newAwsAccount := "210987654321"
	newGoogleProjectID := acctest.RandString(10)
	newGoogleProjectNumber := acctest.RandString(10)

	group := os.Getenv("TF_ACC_GROUP_ID")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccResourceAccountAssociations("example", group, awsAccount, googleProjectID, googleProjectNumber),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(`chainguard_account_associations.example`, `group`, group),
					resource.TestCheckResourceAttr(`chainguard_account_associations.example`, `amazon.account`, awsAccount),
					resource.TestCheckResourceAttr(`chainguard_account_associations.example`, `google.project_id`, googleProjectID),
					resource.TestCheckResourceAttr(`chainguard_account_associations.example`, `google.project_number`, googleProjectNumber),
					resource.TestCheckResourceAttrWith(`chainguard_account_associations.example`, `chainguard.service_bindings.INGESTER`, func(value string) error {
						if !uidp.Valid(value) {
							return fmt.Errorf("not a UIDP: %q", value)
						}
						return nil
					}),
					resource.TestCheckResourceAttrWith(`chainguard_account_associations.example`, `chainguard.service_bindings.COSIGNED`, func(value string) error {
						if !uidp.Valid(value) {
							return fmt.Errorf("not a UIDP: %q", value)
						}
						return nil
					}),
				),
			},

			// ImportState testing.
			{
				ResourceName:      "chainguard_account_associations.example",
				ImportState:       true,
				ImportStateVerify: true,
			},

			// Update and Read testing.
			{
				Config: testAccResourceAccountAssociations("example", group, newAwsAccount, newGoogleProjectID, newGoogleProjectNumber),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(`chainguard_account_associations.example`, `group`, group),
					resource.TestCheckResourceAttr(`chainguard_account_associations.example`, `amazon.account`, newAwsAccount),
					resource.TestCheckResourceAttr(`chainguard_account_associations.example`, `google.project_id`, newGoogleProjectID),
					resource.TestCheckResourceAttr(`chainguard_account_associations.example`, `google.project_number`, newGoogleProjectNumber),
					resource.TestCheckResourceAttrWith(`chainguard_account_associations.example`, `chainguard.service_bindings.INGESTER`, func(value string) error {
						if !uidp.Valid(value) {
							return fmt.Errorf("not a UIDP: %q", value)
						}
						return nil
					}),
					resource.TestCheckResourceAttrWith(`chainguard_account_associations.example`, `chainguard.service_bindings.COSIGNED`, func(value string) error {
						if !uidp.Valid(value) {
							return fmt.Errorf("not a UIDP: %q", value)
						}
						return nil
					}),
				),
			},
		},
	})
}

func TestAccResourceAccountAssociationsProviderChange(t *testing.T) {
	awsAccount := "123456789012"
	googleProjectID := acctest.RandString(10)
	googleProjectNumber := acctest.RandString(10)

	group := os.Getenv("TF_ACC_GROUP_ID")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccResourceAWSAccountAssociation("example", group, awsAccount),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(`chainguard_account_associations.example`, `group`, group),
					resource.TestCheckResourceAttr(`chainguard_account_associations.example`, `amazon.account`, awsAccount),
					resource.TestCheckNoResourceAttr(`chainguard_account_associations.example`, `google`),
					resource.TestCheckNoResourceAttr(`chainguard_account_associations.example`, `chainguard`),
				),
			},

			// ImportState testing.
			{
				ResourceName:      "chainguard_account_associations.example",
				ImportState:       true,
				ImportStateVerify: true,
			},

			// Update and Read testing.
			{
				Config: testAccResourceGCPAccountAssociation("example", group, googleProjectID, googleProjectNumber),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(`chainguard_account_associations.example`, `group`, group),
					resource.TestCheckResourceAttr(`chainguard_account_associations.example`, `google.project_id`, googleProjectID),
					resource.TestCheckResourceAttr(`chainguard_account_associations.example`, `google.project_number`, googleProjectNumber),
					resource.TestCheckNoResourceAttr(`chainguard_account_associations.example`, `amazon`),
					resource.TestCheckNoResourceAttr(`chainguard_account_associations.example`, `chainguard`),
				),
			},
		},
	})
}

func testAccResourceAccountAssociations(name, group, awsAccount, googleProjectID, googleProjectNumber string) string {
	const tmpl = `
resource "chainguard_identity" "ingester" {
  parent_id         = %q
  name              = "ingester"
  service_principal = "INGESTER"
}

resource "chainguard_identity" "cosigned" {
  parent_id         = %q
  name              = "cosigned"
  service_principal = "COSIGNED"
}

resource "chainguard_account_associations" "example" {
  name = %q
  group = %q

  amazon {
    account = %q
  }

  google {
    project_id     = %q
    project_number = %q
  }

  chainguard {
	service_bindings = {
	  "INGESTER": chainguard_identity.ingester.id,
	  "COSIGNED": chainguard_identity.cosigned.id,
	}
  }
}
`
	return fmt.Sprintf(tmpl, group, group, name, group, awsAccount, googleProjectID, googleProjectNumber)
}

func testAccResourceGCPAccountAssociation(name, group, googleProjectID, googleProjectNumber string) string {
	const tmpl = `
resource "chainguard_account_associations" "example" {
  name = %q
  group = %q

  google {
    project_id     = %q
    project_number = %q
  }
}
`
	return fmt.Sprintf(tmpl, name, group, googleProjectID, googleProjectNumber)
}

func testAccResourceAWSAccountAssociation(name, group, awsAccount string) string {
	const tmpl = `
resource "chainguard_account_associations" "example" {
  name = %q
  group = %q

  amazon {
    account = %q
  }
}
`
	return fmt.Sprintf(tmpl, name, group, awsAccount)
}
