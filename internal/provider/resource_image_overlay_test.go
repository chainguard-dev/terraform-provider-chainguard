/*
Copyright 2026 Chainguard, Inc.
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

type testOverlay struct {
	parentID string
	name     string
	packages string
	selector string
}

func TestImageOverlay(t *testing.T) {
	clients := testAccV2Client(t)
	parentID := os.Getenv("TF_ACC_GROUP_ID")
	name := acctest.RandString(10)

	original := testOverlay{
		parentID: parentID,
		name:     name,
		packages: `["curl"]`,
		selector: `
    kind = "EXACT"
    tags = ["latest"]
`,
	}

	// Changing packages forces replacement of the overlay, which in turn
	// forces replacement of the binding (overlay_id changes).
	update := testOverlay{
		parentID: parentID,
		name:     name,
		packages: `["curl", "jq"]`,
		selector: `
    kind = "VARIANT"
    variant_type = "DEV"
`,
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: resource.ComposeTestCheckFunc(
			checkImageOverlayDestroy(clients),
			checkImageOverlayBindingDestroy(clients),
		),
		Steps: []resource.TestStep{
			// Create and Read testing: repo, then overlay, then binding.
			{
				Config: testImageOverlay(original),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(`chainguard_image_overlay.example`, `name`, name),
					resource.TestCheckResourceAttr(`chainguard_image_overlay.example`, `parent_id`, parentID),
					resource.TestCheckResourceAttr(`chainguard_image_overlay.example`, `packages.0`, "curl"),
					resource.TestCheckResourceAttrSet(`chainguard_image_overlay.example`, `id`),
					resource.TestCheckResourceAttr(`chainguard_image_overlay_binding.example`, `tag_selector.kind`, "EXACT"),
					resource.TestCheckResourceAttr(`chainguard_image_overlay_binding.example`, `tag_selector.tags.0`, "latest"),
					resource.TestCheckResourceAttrPair(
						`chainguard_image_overlay_binding.example`, `overlay_id`,
						`chainguard_image_overlay.example`, `id`),
					resource.TestCheckResourceAttrPair(
						`chainguard_image_overlay_binding.example`, `repo_id`,
						`chainguard_image_repo.overlay_example`, `id`),
				),
			},
			// ImportState testing.
			{
				ResourceName:      "chainguard_image_overlay.example",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				ResourceName:      "chainguard_image_overlay_binding.example",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Replacement testing: new packages replace the overlay and
			// (via overlay_id) the binding; the new selector kind also
			// forces binding replacement.
			{
				Config: testImageOverlay(update),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(`chainguard_image_overlay.example`, `packages.0`, "curl"),
					resource.TestCheckResourceAttr(`chainguard_image_overlay.example`, `packages.1`, "jq"),
					resource.TestCheckResourceAttr(`chainguard_image_overlay_binding.example`, `tag_selector.kind`, "VARIANT"),
					resource.TestCheckResourceAttr(`chainguard_image_overlay_binding.example`, `tag_selector.variant_type`, "DEV"),
					resource.TestCheckNoResourceAttr(`chainguard_image_overlay_binding.example`, `tag_selector.tags`),
				),
			},
		},
	})
}

func testImageOverlay(o testOverlay) string {
	const tmpl = `
# The binding needs a repo to attach to.
resource "chainguard_image_repo" "overlay_example" {
  parent_id = %q
  name      = %q
}

resource "chainguard_image_overlay" "example" {
  parent_id = %q
  name      = %q
  packages  = %s
}

resource "chainguard_image_overlay_binding" "example" {
  repo_id    = chainguard_image_repo.overlay_example.id
  overlay_id = chainguard_image_overlay.example.id

  tag_selector {%s  }
}
`
	return fmt.Sprintf(tmpl, o.parentID, o.name, o.parentID, o.name, o.packages, o.selector)
}

func TestImageOverlayConfig(t *testing.T) {
	clients := testAccV2Client(t)
	parentID := os.Getenv("TF_ACC_GROUP_ID")
	name := acctest.RandString(10)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkImageOverlayDestroy(clients),
		Steps: []resource.TestStep{
			// Create and Read testing with a full config instead of packages.
			{
				Config: testImageOverlayConfig(parentID, name),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(`chainguard_image_overlay.config_example`, `name`, name),
					resource.TestCheckResourceAttr(`chainguard_image_overlay.config_example`, `parent_id`, parentID),
					resource.TestCheckResourceAttrSet(`chainguard_image_overlay.config_example`, `config`),
					resource.TestCheckNoResourceAttr(`chainguard_image_overlay.config_example`, `packages`),
					resource.TestCheckResourceAttrSet(`chainguard_image_overlay.config_example`, `id`),
				),
			},
			// ImportState testing. An imported overlay is packages-managed
			// until the practitioner's config says otherwise, so the
			// content attributes are excluded from verification.
			{
				ResourceName:            "chainguard_image_overlay.config_example",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"config", "packages"},
			},
		},
	})
}

func testImageOverlayConfig(parentID, name string) string {
	const tmpl = `
resource "chainguard_image_overlay" "config_example" {
  parent_id = %q
  name      = %q
  config = jsonencode({
    contents = {
      packages = ["curl"]
    }
    environment = {
      TZ = "UTC"
    }
    annotations = {
      "com.example/note" = "hi"
    }
  })
}
`
	return fmt.Sprintf(tmpl, parentID, name)
}

func TestValidOverlayConfigValue(t *testing.T) {
	for _, c := range []struct {
		name    string
		config  string
		wantErr bool
	}{{
		name: "every customer-settable field",
		config: `{
			"contents": {
				"packages": ["curl"],
				"runtime_repositories": ["https://apk.example.com"],
				"runtime_keyring": [{"name": "mirror.rsa.pub", "content": "PEM"}]
			},
			"environment": {"TZ": "UTC"},
			"annotations": {"com.example/note": "hi"},
			"accounts": {"run_as": "65532", "users": [{"username": "app", "uid": 65532}], "groups": [{"groupname": "app", "gid": 65532}]},
			"certificates": {"additional": [{"name": "corp-ca", "content": "PEM"}]}
		}`,
	}, {
		name:    "unknown field rejected",
		config:  `{"entrypoint": {"command": "/bin/sh"}}`,
		wantErr: true,
	}, {
		name:    "malformed json rejected",
		config:  `{`,
		wantErr: true,
	}} {
		t.Run(c.name, func(t *testing.T) {
			err := validOverlayConfigValue(c.config)
			if gotErr := err != nil; gotErr != c.wantErr {
				t.Errorf("validOverlayConfigValue: got err = %v, want error = %t", err, c.wantErr)
			}
		})
	}
}
