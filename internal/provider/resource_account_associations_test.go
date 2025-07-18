/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"chainguard.dev/sdk/uidp"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccResourceAccountAssociations(t *testing.T) {
	awsAccount := "123456789012"
	googleProjectID := acctest.RandString(10)
	googleProjectNumber := acctest.RandString(10)
	azureTenantId := "10000000-0000-0000-0000-000000000000"
	azureClientIds := map[string]string{
		"canary": "20000000-0000-0000-0000-000000000000",
	}

	newAwsAccount := "210987654321"
	newGoogleProjectID := acctest.RandString(10)
	newGoogleProjectNumber := acctest.RandString(10)
	newAzureTenantId := "30000000-0000-0000-0000-000000000000"
	newAzureClientIds := map[string]string{}

	group := os.Getenv("TF_ACC_GROUP_ID")
	subgroup := acctest.RandString(10)
	childpattern := regexp.MustCompile(fmt.Sprintf(`%s\/[a-z0-9]{16}`, group))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccResourceAccountAssociations("example", group, subgroup, awsAccount, googleProjectID, googleProjectNumber, azureTenantId, azureClientIds),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_account_associations.example`, `group`, childpattern),
					resource.TestCheckResourceAttr(`chainguard_account_associations.example`, `amazon.account`, awsAccount),
					resource.TestCheckResourceAttr(`chainguard_account_associations.example`, `google.project_id`, googleProjectID),
					resource.TestCheckResourceAttr(`chainguard_account_associations.example`, `google.project_number`, googleProjectNumber),
					resource.TestCheckResourceAttr(`chainguard_account_associations.example`, `azure.tenant_id`, azureTenantId),
					resource.TestCheckResourceAttr(`chainguard_account_associations.example`, `azure.client_ids.canary`, azureClientIds["canary"]),
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
				Config: testAccResourceAccountAssociations("example", group, subgroup, newAwsAccount, newGoogleProjectID, newGoogleProjectNumber, newAzureTenantId, newAzureClientIds),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_account_associations.example`, `group`, childpattern),
					resource.TestCheckResourceAttr(`chainguard_account_associations.example`, `amazon.account`, newAwsAccount),
					resource.TestCheckResourceAttr(`chainguard_account_associations.example`, `google.project_id`, newGoogleProjectID),
					resource.TestCheckResourceAttr(`chainguard_account_associations.example`, `google.project_number`, newGoogleProjectNumber),
					resource.TestCheckResourceAttr(`chainguard_account_associations.example`, `azure.tenant_id`, newAzureTenantId),
					resource.TestCheckResourceAttr(`chainguard_account_associations.example`, `azure.client_ids`, `{}`),
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
	azureTenantId := "10000000-0000-0000-0000-000000000000"
	azureClientIds := map[string]string{
		"canary": "20000000-0000-0000-0000-000000000000",
	}

	group := os.Getenv("TF_ACC_GROUP_ID")
	subgroup := acctest.RandString(10)
	childpattern := regexp.MustCompile(fmt.Sprintf(`%s\/[a-z0-9]{16}`, group))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccResourceAWSAccountAssociation("example", group, subgroup, awsAccount),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_account_associations.example`, `group`, childpattern),
					resource.TestCheckResourceAttr(`chainguard_account_associations.example`, `amazon.account`, awsAccount),
					resource.TestCheckNoResourceAttr(`chainguard_account_associations.example`, `google`),
					resource.TestCheckNoResourceAttr(`chainguard_account_associations.example`, `azure`),
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
				Config: testAccResourceGCPAccountAssociation("example", group, subgroup, googleProjectID, googleProjectNumber),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_account_associations.example`, `group`, childpattern),
					resource.TestCheckResourceAttr(`chainguard_account_associations.example`, `google.project_id`, googleProjectID),
					resource.TestCheckResourceAttr(`chainguard_account_associations.example`, `google.project_number`, googleProjectNumber),
					resource.TestCheckNoResourceAttr(`chainguard_account_associations.example`, `amazon`),
					resource.TestCheckNoResourceAttr(`chainguard_account_associations.example`, `azure`),
					resource.TestCheckNoResourceAttr(`chainguard_account_associations.example`, `chainguard`),
				),
			},

			// Update and Read testing.
			{
				Config: testAccResourceAzureAccountAssociation("example", group, subgroup, azureTenantId, azureClientIds),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_account_associations.example`, `group`, childpattern),
					resource.TestCheckResourceAttr(`chainguard_account_associations.example`, `azure.tenant_id`, azureTenantId),
					resource.TestCheckResourceAttr(`chainguard_account_associations.example`, `azure.client_ids.canary`, azureClientIds["canary"]),
					resource.TestCheckNoResourceAttr(`chainguard_account_associations.example`, `amazon`),
					resource.TestCheckNoResourceAttr(`chainguard_account_associations.example`, `google`),
					resource.TestCheckNoResourceAttr(`chainguard_account_associations.example`, `chainguard`),
				),
			},
		},
	})
}

func testAccResourceAccountAssociations(name, group, subgroup, awsAccount, googleProjectID, googleProjectNumber, azureTenantId string, azureClientIds map[string]string) string {
	const tmpl = `
resource "chainguard_group" "subgroup" {
  parent_id = %q
  name = %q
}

resource "chainguard_identity" "ingester" {
  parent_id         = chainguard_group.subgroup.id
  name              = "ingester"
  service_principal = "INGESTER"
}

resource "chainguard_identity" "cosigned" {
  parent_id         = chainguard_group.subgroup.id
  name              = "cosigned"
  service_principal = "COSIGNED"
}

resource "chainguard_account_associations" "example" {
  name = %q
  group = chainguard_group.subgroup.id

  amazon {
    account = %q
  }

  google {
    project_id     = %q
    project_number = %q
  }

  azure {
    tenant_id = %q
    client_ids = %q
  }

  chainguard {
	service_bindings = {
	  "INGESTER": chainguard_identity.ingester.id,
	  "COSIGNED": chainguard_identity.cosigned.id,
	}
  }
}
`
	return fmt.Sprintf(tmpl, group, subgroup, name, awsAccount, googleProjectID, googleProjectNumber, azureTenantId, mapToTF(azureClientIds))
}

func testAccResourceGCPAccountAssociation(name, group, subgroup, googleProjectID, googleProjectNumber string) string {
	const tmpl = `
resource "chainguard_group" "subgroup" {
  parent_id = %q
  name = %q
}

resource "chainguard_account_associations" "example" {
  name = %q
  group = chainguard_group.subgroup.id

  google {
    project_id     = %q
    project_number = %q
  }
}
`
	return fmt.Sprintf(tmpl, group, subgroup, name, googleProjectID, googleProjectNumber)
}

func testAccResourceAWSAccountAssociation(name, group, subgroup, awsAccount string) string {
	const tmpl = `
resource "chainguard_group" "subgroup" {
  parent_id = %q
  name = %q
}

resource "chainguard_account_associations" "example" {
  name = %q
  group = chainguard_group.subgroup.id

  amazon {
    account = %q
  }
}
`
	return fmt.Sprintf(tmpl, group, subgroup, name, awsAccount)
}

func testAccResourceAzureAccountAssociation(name, group, subgroup, azureTenantId string, azureClientIds map[string]string) string {
	const tmpl = `
resource "chainguard_group" "subgroup" {
  parent_id = %q
  name = %q
}

resource "chainguard_account_associations" "example" {
  name = %q
  group = chainguard_group.subgroup.id

  azure {
    tenant_id = %q
    client_ids = %q
  }
}
`
	return fmt.Sprintf(tmpl, group, subgroup, name, azureTenantId, mapToTF(azureClientIds))
}

// mapToTF converts a map of client IDs to a Terraform-compatible string representation.
func mapToTF(m map[string]string) string {
	if len(m) == 0 {
		return `{}`
	}
	clientIdsBuilder := strings.Builder{}
	clientIdsBuilder.WriteString(`{`)
	first := true
	for component, clientid := range m {
		if !first {
			clientIdsBuilder.WriteString(`, `)
		}
		clientIdsBuilder.WriteString(fmt.Sprintf(`%s = "%s"`, component, clientid))
	}
	clientIdsBuilder.WriteString(`}`)
	return clientIdsBuilder.String()
}
