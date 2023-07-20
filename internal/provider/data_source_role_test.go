package provider

import (
	"chainguard.dev/api/pkg/uidp"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const dataRoleViewer = `
data "chainguard_role" "viewer_test" {
  name = "viewer"
  parent = "/"
}
`

func TestAccRoleDataSource(t *testing.T) {

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Read testing
			{
				Config: providerConfig + dataRoleViewer,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify number of roles returned is 1.
					resource.TestCheckResourceAttr("data.chainguard_role.viewer_test", "items.#", "1"),
					// Verify the viewer attributes.
					resource.TestCheckResourceAttrWith("data.chainguard_role.viewer_test", "items.0.id", func(id string) error {
						if !uidp.Valid(id) {
							return fmt.Errorf("%q is not a valid UIDP", id)
						}
						return nil
					}),
					resource.TestCheckResourceAttr("data.chainguard_role.viewer_test", "items.0.name", "viewer"),
					// TODO: include the actual description here?
					resource.TestCheckResourceAttrSet("data.chainguard_role.viewer_test", "items.0.description"),
					// TODO: check for caps.
				),
			},
		},
	})
}
