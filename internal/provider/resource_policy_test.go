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

func TestAccResourcePolicy(t *testing.T) {
	description := acctest.RandString(10)
	document := `
apiVersion: policy.sigstore.dev/v1beta1
kind: ClusterImagePolicy
metadata:
  name: foo
spec:
  images:
  - glob: "example.com/foo/*"
  authorities:
  - keyless:
      url: https://fulcio.sigstore.dev
      identities:
      - issuerRegExp: .*kubernetes.default.*
        subjectRegExp: .*kubernetes.io/namespaces/default/serviceaccounts/default
`
	parent := os.Getenv("TF_ACC_GROUP_ID")

	newDescription := acctest.RandString(10)
	newDocument := `
apiVersion: policy.sigstore.dev/v1beta1
kind: ClusterImagePolicy
metadata:
  name: foo
spec:
  images:
  - glob: "example.com/bar/*"
  authorities:
  - keyless:
      url: https://fulcio.sigstage.dev
      identities:
      - issuerRegExp: .*kubernetes.default.*
        subjectRegExp: .*kubernetes.io/namespaces/default/serviceaccounts/default
`

	childpattern := regexp.MustCompile(fmt.Sprintf(`%s\/[a-z0-9]{16}`, parent))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccResourcePolicy(parent, description, document),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(`chainguard_policy.example`, `parent_id`, parent),
					resource.TestCheckResourceAttr(`chainguard_policy.example`, `name`, `foo`),
					resource.TestCheckResourceAttr(`chainguard_policy.example`, `description`, description),
					resource.TestMatchResourceAttr(`chainguard_policy.example`, `id`, childpattern),
				),
			},
			// ImportState testing.
			{
				ResourceName:      "chainguard_policy.example",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccResourcePolicy(parent, newDescription, newDocument),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(`chainguard_policy.example`, `parent_id`, parent),
					resource.TestCheckResourceAttr(`chainguard_policy.example`, `name`, `foo`),
					resource.TestCheckResourceAttr(`chainguard_policy.example`, `description`, newDescription),
					resource.TestMatchResourceAttr(`chainguard_policy.example`, `document`, regexp.MustCompile(`https://fulcio.sigstage.dev`)),
					resource.TestMatchResourceAttr(`chainguard_policy.example`, `id`, childpattern),
				),
			},
		},
	})
}

func testAccResourcePolicy(parent, description, document string) string {
	const tmpl = `
resource "chainguard_policy" "example" {
  parent_id   = "%s"
  description = "%s"
  document    = <<EOF
  %s
  EOF
}
`
	return fmt.Sprintf(tmpl, parent, description, document)
}
