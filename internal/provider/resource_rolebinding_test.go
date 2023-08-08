/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

//import (
//	"fmt"
//	"os"
//	"regexp"
//	"testing"
//
//	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
//)
//
//func TestAccRolebindingResource(t *testing.T) {
//	group := os.Getenv(EnvAccGroupID)
//	childpattern := regexp.MustCompile(fmt.Sprintf(`%s\/[a-z0-9]{16}`, group))
//	rootpattern := regexp.MustCompile(`[a-z0-9]{40}`)
//
//	role := testAccResourceRole(group, "role", "", []string{"groups.list"})
//	viewer := accDataRoleViewer
//	// TODO
//	// ident := testAccResourceIdentity()
//	ident := ""
//	customRoleBinding := testAccResourceRoleBinding(group, "chainguard_identity.test.id", "chainguard_role.test.id")
//	viewerRoleBinding := testAccResourceRoleBinding(group, "chainguard_identity.test.id", "chainguard_role.viewer_test.id")
//
//	resource.Test(t, resource.TestCase{
//		PreCheck:                 func() { testAccPreCheck(t) },
//		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
//		Steps: []resource.TestStep{
//			// Create and Read testing.
//			{
//				Config: providerConfig + role + ident + customRoleBinding,
//				Check: resource.ComposeAggregateTestCheckFunc(
//					resource.TestCheckResourceAttr("chainguard_rolebinding.test", "group", group),
//					resource.TestMatchResourceAttr("chainguard_rolebinding.test", "identity", childpattern),
//					resource.TestMatchResourceAttr("chainguard_rolebinding.test", "role", childpattern),
//					resource.TestMatchResourceAttr("chainguard_rolebinding.test", "id", childpattern),
//				),
//			},
//
//			// ImportState testing.
//			{
//				ResourceName:      "chainguard_rolebinding.test",
//				ImportState:       true,
//				ImportStateVerify: true,
//			},
//
//			// Update and Read testing.
//			{
//				Config: providerConfig + viewer + ident + viewerRoleBinding,
//				Check: resource.ComposeAggregateTestCheckFunc(
//					resource.TestCheckResourceAttr("chainguard_rolebinding.test", "group", group),
//					resource.TestMatchResourceAttr("chainguard_rolebinding.test", "identity", childpattern),
//					resource.TestMatchResourceAttr("chainguard_rolebinding.test", "role", rootpattern),
//					resource.TestMatchResourceAttr("chainguard_rolebinding.test", "id", childpattern),
//				),
//			},
//
//			// Delete testing automatically occurs in TestCase.
//		},
//	})
//}
//
//func testAccResourceRoleBinding(groupID, identityID, roleID string) string {
//	tmpl := `
//resource "chainguard_rolebinding" "test" {
//  identity = %q
//  group    = %q
//  role     = %q
//}
//`
//	return fmt.Sprintf(tmpl, identityID, groupID, roleID)
//}
