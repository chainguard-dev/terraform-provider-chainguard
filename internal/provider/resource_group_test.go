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

	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	iam "chainguard.dev/sdk/proto/platform/iam/v1"
	iamtest "chainguard.dev/sdk/proto/platform/iam/v1/test"
	platformtest "chainguard.dev/sdk/proto/platform/test"
	"github.com/chainguard-dev/clog/slogtest"
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

func TestGroupResource_update(t *testing.T) {
	for _, c := range []struct {
		name     string
		onUpdate iamtest.GroupOnUpdate
		data     groupResourceModel
		state    groupResourceModel
		wantDiag diag.Diagnostic
	}{{
		name: "update description",
		onUpdate: iamtest.GroupOnUpdate{
			Given: &iam.Group{
				Id:          "id",
				Name:        "name",
				Description: "foo",
				Verified:    false,
			},
			Updated: &iam.Group{
				Id:          "id",
				Name:        "name",
				Description: "foo",
				Verified:    false,
			},
		},
		data: groupResourceModel{
			ID:          types.StringValue("id"),
			Name:        types.StringValue("name"),
			Description: types.StringValue("foo"),
		},
		state: groupResourceModel{
			ID:   types.StringValue("id"),
			Name: types.StringValue("name"),
		},
	}, {
		name: "set verified when null",
		onUpdate: iamtest.GroupOnUpdate{
			Given: &iam.Group{
				Id:       "id",
				Name:     "name",
				Verified: true,
			},
			Updated: &iam.Group{
				Id:       "id",
				Name:     "name",
				Verified: true,
			},
		},
		data: groupResourceModel{
			ID:       types.StringValue("id"),
			Name:     types.StringValue("name"),
			Verified: types.BoolValue(true),
		},
		state: groupResourceModel{
			ID:   types.StringValue("id"),
			Name: types.StringValue("name"),
		},
	}, {
		name: "set verified when false",
		onUpdate: iamtest.GroupOnUpdate{
			Given: &iam.Group{
				Id:       "id",
				Name:     "name",
				Verified: true,
			},
			Updated: &iam.Group{
				Id:       "id",
				Name:     "name",
				Verified: true,
			},
		},
		data: groupResourceModel{
			ID:       types.StringValue("id"),
			Name:     types.StringValue("name"),
			Verified: types.BoolValue(true),
		},
		state: groupResourceModel{
			ID:       types.StringValue("id"),
			Name:     types.StringValue("name"),
			Verified: types.BoolValue(false),
		},
	}, {
		name: "unverify with verified_protection false",
		onUpdate: iamtest.GroupOnUpdate{
			Given: &iam.Group{
				Id:       "id",
				Name:     "name",
				Verified: false,
			},
			Updated: &iam.Group{
				Id:       "id",
				Name:     "name",
				Verified: false,
			},
		},
		data: groupResourceModel{
			ID:                 types.StringValue("id"),
			Name:               types.StringValue("name"),
			Verified:           types.BoolValue(false),
			VerifiedProtection: types.BoolValue(false),
		},
		state: groupResourceModel{
			ID:                 types.StringValue("id"),
			Name:               types.StringValue("name"),
			Verified:           types.BoolValue(true),
			VerifiedProtection: types.BoolValue(false),
		},
	}, {
		name: "unverify with verified_protection true",
		data: groupResourceModel{
			ID:                 types.StringValue("id"),
			Name:               types.StringValue("name"),
			Verified:           types.BoolValue(false),
			VerifiedProtection: types.BoolValue(true),
		},
		state: groupResourceModel{
			ID:                 types.StringValue("id"),
			Name:               types.StringValue("name"),
			Verified:           types.BoolValue(true),
			VerifiedProtection: types.BoolValue(true),
		},
		wantDiag: diag.NewErrorDiagnostic("cannot unverify group", "group id is verified and verified_protection is true or null; apply verified_protection = false before attempting to unverify this group"),
	}, {
		name: "unverify with verified_protection null",
		data: groupResourceModel{
			ID:       types.StringValue("id"),
			Name:     types.StringValue("name"),
			Verified: types.BoolValue(false),
		},
		state: groupResourceModel{
			ID:       types.StringValue("id"),
			Name:     types.StringValue("name"),
			Verified: types.BoolValue(true),
		},
		wantDiag: diag.NewErrorDiagnostic("cannot unverify group", "group id is verified and verified_protection is true or null; apply verified_protection = false before attempting to unverify this group"),
	}} {
		t.Run(c.name, func(t *testing.T) {
			ctx := slogtest.Context(t)
			r := &groupResource{
				managedResource: managedResource{
					prov: &providerData{
						client: platformtest.MockPlatformClients{
							IAMClient: iamtest.MockIAMClient{
								GroupsClient: iamtest.MockGroupsClient{
									OnUpdate: []iamtest.GroupOnUpdate{c.onUpdate},
								},
							},
						},
					},
				},
			}

			dc := groupResourceModel{
				ID:                 c.data.ID,
				Name:               c.data.Name,
				Description:        c.data.Description,
				Verified:           c.data.Verified,
				VerifiedProtection: c.data.VerifiedProtection,
			}
			gotDiag := r.update(ctx, &dc, c.state)
			if (gotDiag == nil) != (c.wantDiag == nil) {
				t.Fatalf("update did not return expected diagnostics: want=%v, got=%v", c.wantDiag, gotDiag)
			}
			if diff := cmp.Diff(c.wantDiag, gotDiag); diff != "" {
				t.Fatalf("update did not return expected diagnostics (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(c.data, dc); diff != "" {
				t.Fatalf("updated data not what expected (-want +got):\n%s", diff)
			}
		})
	}

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
