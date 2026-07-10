/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"fmt"
	"os"
	"regexp"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"

	registry "chainguard.dev/sdk/proto/platform/registry/v1"
	"chainguard.dev/sdk/uidp"
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
	clients := testAccPlatformClient(t)
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
		CheckDestroy:             checkImageRepoDestroy(clients),
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

func TestImageRepo_InvalidParentID(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "chainguard_image_repo" "bad" {
  parent_id = "not-a-valid-uidp"
  name      = "test"
}
`,
				ExpectError: regexp.MustCompile(`valid UIDP`),
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

func TestImageRepo_CustomOverlayReservedEnvPrefix(t *testing.T) {
	// Fails during plan (schema validator), before any registry RPC.
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "chainguard_image_repo" "bad" {
  parent_id = %q
  name      = "test"
  custom_overlay {
    environment = {
      "CHAINGUARD_FOO" = "bar"
    }
  }
}
`, os.Getenv("TF_ACC_GROUP_ID")),
				ExpectError: regexp.MustCompile(`reserved prefix`),
			},
		},
	})
}

func TestImageRepo_CustomOverlayReservedAnnotationPrefix(t *testing.T) {
	// Fails during plan (schema validator), before any registry RPC.
	for _, key := range []string{"dev.chainguard.foo", "org.opencontainers.image.title"} {
		resource.Test(t, resource.TestCase{
			PreCheck:                 func() { testAccPreCheck(t) },
			ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: fmt.Sprintf(`
resource "chainguard_image_repo" "bad" {
  parent_id = %q
  name      = "test"
  custom_overlay {
    annotations = {
      %q = "value"
    }
  }
}
`, os.Getenv("TF_ACC_GROUP_ID"), key),
					ExpectError: regexp.MustCompile(`reserved prefix`),
				},
			},
		})
	}
}

// testImageRepoCustomOverlay renders a config for the pre-provisioned
// custom-assembly repo. The sync_config block must always be present and echo
// the live values: UpdateRepo is a full replacement, so omitting it would
// strip the repo's sync_config (and with it custom assembly).
func testImageRepoCustomOverlay(parentID, name, source, expiration, overlay string) string {
	var expirationLine string
	if expiration != "" {
		expirationLine = fmt.Sprintf("expiration = %q", expiration)
	}
	return fmt.Sprintf(`
resource "chainguard_image_repo" "ca" {
  parent_id = %q
  name      = %q
  sync_config {
    source = %q
    %s
  }
  %s
}
`, parentID, name, source, expirationLine, overlay)
}

// TestAccImageRepo_CustomOverlay exercises the custom_overlay lifecycle
// end-to-end against a real custom-assembly-enabled repo. It is gated on
// TF_ACC_CUSTOM_OVERLAY_REPO_ID naming a pre-provisioned repo (the CI
// identity cannot create one: it cannot authorize any sync_config.source).
//
// The shared repo must never be destroyed: the test imports it, ends with
// the overlay cleared, and forgets the resource with a removed block so the
// framework's final destroy has nothing to delete.
func TestAccImageRepo_CustomOverlay(t *testing.T) {
	repoID := os.Getenv("TF_ACC_CUSTOM_OVERLAY_REPO_ID")
	if repoID == "" {
		t.Skip("TF_ACC_CUSTOM_OVERLAY_REPO_ID not set - skipping custom_overlay acceptance test")
		return
	}
	// Guard the pre-flight RPCs below, not just resource.Test: without this,
	// a unit-test run with the repo ID set would still hit the real API.
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set - skipping custom_overlay acceptance test")
		return
	}

	clients := testAccPlatformClient(t)
	if clients == nil {
		t.Skip("acceptance test env vars not set")
		return
	}

	// Fetch the repo so the config mirrors its live identity and
	// sync_config; see testImageRepoCustomOverlay.
	repoList, err := clients.Registry().Registry().ListRepos(t.Context(), &registry.RepoFilter{Id: repoID})
	if err != nil {
		t.Fatalf("failed to fetch repo %s: %s", repoID, err)
	}
	if len(repoList.GetItems()) != 1 {
		t.Fatalf("expected exactly one repo for %s, got %d", repoID, len(repoList.GetItems()))
	}
	repo := repoList.GetItems()[0]
	if repo.GetSyncConfig().GetApkoOverlay() == "" {
		t.Fatalf("repo %s is not custom-assembly enabled (sync_config.apko_overlay is empty)", repoID)
	}
	parentID := uidp.Parent(repoID)
	name := repo.GetName()
	source := repo.GetSyncConfig().GetSource()
	expiration := ""
	if exp := repo.GetSyncConfig().GetExpiration(); exp != nil && !exp.AsTime().IsZero() {
		expiration = exp.AsTime().Format(time.RFC3339)
	}

	overlay1 := `
  custom_overlay {
    contents {
      packages = ["curl"]
    }
    environment = {
      "HTTP_PROXY" = "http://proxy.example.com:3128"
    }
    annotations = {
      "com.example.team" = "platform"
    }
    accounts {
      run_as = "65532"
    }
  }`

	overlay2 := `
  custom_overlay {
    contents {
      packages = ["curl", "jq"]
    }
    annotations = {
      "com.example.team" = "infra"
    }
  }`

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		// The final step's removed block (destroy = false) needs Terraform
		// 1.7+. Skipping up front on older CLIs matters: if that step merely
		// errored, the framework's cleanup destroy would run with the shared
		// repo still in state — and delete it.
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_7_0),
		},
		Steps: []resource.TestStep{
			// Adopt the pre-provisioned repo.
			{
				Config:             testImageRepoCustomOverlay(parentID, name, source, expiration, ""),
				ResourceName:       "chainguard_image_repo.ca",
				ImportState:        true,
				ImportStateId:      repoID,
				ImportStatePersist: true,
			},
			// Set an overlay.
			{
				Config: testImageRepoCustomOverlay(parentID, name, source, expiration, overlay1),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("chainguard_image_repo.ca", "custom_overlay.contents.packages.0", "curl"),
					resource.TestCheckResourceAttr("chainguard_image_repo.ca", "custom_overlay.environment.HTTP_PROXY", "http://proxy.example.com:3128"),
					resource.TestCheckResourceAttr("chainguard_image_repo.ca", "custom_overlay.accounts.run_as", "65532"),
				),
			},
			// Mutate it in place.
			{
				Config: testImageRepoCustomOverlay(parentID, name, source, expiration, overlay2),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("chainguard_image_repo.ca", "custom_overlay.contents.packages.1", "jq"),
					resource.TestCheckResourceAttr("chainguard_image_repo.ca", "custom_overlay.annotations.com.example.team", "infra"),
					resource.TestCheckNoResourceAttr("chainguard_image_repo.ca", "custom_overlay.environment.HTTP_PROXY"),
				),
			},
			// Clear it by removing the block.
			{
				Config: testImageRepoCustomOverlay(parentID, name, source, expiration, ""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckNoResourceAttr("chainguard_image_repo.ca", "custom_overlay"),
				),
			},
			// The cleared overlay reads back from the API as an empty
			// message; the nil ≡ empty normalization must yield an empty
			// plan or every clear would leave a perpetual diff.
			{
				Config:   testImageRepoCustomOverlay(parentID, name, source, expiration, ""),
				PlanOnly: true,
			},
			// Forget the resource without destroying the shared repo.
			{
				Config: `
removed {
  from = chainguard_image_repo.ca
  lifecycle {
    destroy = false
  }
}
`,
			},
		},
	})
}

func TestLockRepo_SameKeySerializes(t *testing.T) {
	var counter atomic.Int32

	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			unlock := lockRepo("same-key")
			defer unlock()

			// If the lock works, only one goroutine is in this section at a time.
			cur := counter.Add(1)
			if cur != 1 {
				t.Errorf("expected exclusive access, got %d concurrent holders", cur)
			}
			time.Sleep(time.Millisecond)
			counter.Add(-1)
		})
	}
	wg.Wait()
}

func TestLockRepo_DifferentKeysConcurrent(t *testing.T) {
	// Two different keys should not block each other.
	unlock1 := lockRepo("key-a")

	done := make(chan struct{})
	go func() {
		unlock2 := lockRepo("key-b")
		unlock2()
		close(done)
	}()

	select {
	case <-done:
		// key-b acquired while key-a held — correct.
	case <-time.After(time.Second):
		t.Fatal("different keys blocked each other")
	}
	unlock1()
}

func TestLockRepo_CleansUpEntries(t *testing.T) {
	unlock := lockRepo("ephemeral-key")
	unlock()

	repoLocks.Lock()
	_, exists := repoLocks.refs["ephemeral-key"]
	repoLocks.Unlock()

	if exists {
		t.Error("expected map entry to be cleaned up after unlock")
	}
}
