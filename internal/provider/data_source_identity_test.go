/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

// Only works when pointing to enforce.dev
// TODO(colin): env vars for iss/sub? That's alotta env vars...
//const accDataIdentity = `
//data "chainguard_identity" "test" {
//	issuer  = "https://accounts.google.com"
//	subject = "107599433400283128388"
//}
//`
//
//func TestAccIdentityDataSource(t *testing.T) {
//	resource.Test(t, resource.TestCase{
//		PreCheck:                 func() { testAccPreCheck(t) },
//		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
//		Steps: []resource.TestStep{
//			// Read testing
//			{
//				Config: providerConfig + accDataIdentity,
//				Check: resource.ComposeAggregateTestCheckFunc(
//					// Verify the identity ID returned is valid.
//					resource.TestCheckResourceAttrWith("data.chainguard_identity.test", "id", func(id string) error {
//						if !uidp.Valid(id) {
//							return fmt.Errorf("%q is not a valid UIDP", id)
//						}
//						return nil
//					}),
//				),
//			},
//		},
//	})
//}
