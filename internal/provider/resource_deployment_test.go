/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func testAccResourceDeployment(repoID string, charts []map[string]interface{}) string {
	const tmpl = `
resource "chainguard_deployment" "test" {
	id = %q
	charts = [
%s	]
}
`
	var chartConfigs []string
	for _, chart := range charts {
		chartConfig := fmt.Sprintf("\t\t{\n\t\t\trepo = %q\n", chart["repo"])
		if source, ok := chart["source"]; ok && source != "" {
			chartConfig += fmt.Sprintf("\t\t\tsource = %q\n", source)
		}
		chartConfig += "\t\t},\n"
		chartConfigs = append(chartConfigs, chartConfig)
	}
	return fmt.Sprintf(tmpl, repoID, strings.Join(chartConfigs, ""))
}

func TestAccResourceDeployment_basic(t *testing.T) {
	repoID := "" // Will need a valid repo UIDP when implemented
	charts := []map[string]interface{}{
		{
			"repo":   "oci://ghcr.io/stefanprodan/charts/podinfo",
			"source": "https://github.com/stefanprodan/podinfo",
		},
	}

	// Skip this test since we need a valid repo ID and the API may need testing
	t.Skip("Deployment resource needs a valid repo ID for testing")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: testAccResourceDeployment(repoID, charts),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("chainguard_deployment.test", "id", repoID),
					resource.TestCheckResourceAttr("chainguard_deployment.test", "charts.#", "1"),
					resource.TestCheckResourceAttr("chainguard_deployment.test", "charts.0.repo", "oci://ghcr.io/stefanprodan/charts/podinfo"),
					resource.TestCheckResourceAttr("chainguard_deployment.test", "charts.0.source", "https://github.com/stefanprodan/podinfo"),
				),
			},
			// ImportState testing
			{
				ResourceName:      "chainguard_deployment.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Update and Read testing
			{
				Config: testAccResourceDeployment(repoID, []map[string]interface{}{
					{
						"repo": "https://kyverno.github.io/kyverno/",
					},
					{
						"repo":   "oci://ghcr.io/stefanprodan/charts/podinfo",
						"source": "https://github.com/stefanprodan/podinfo",
					},
				}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("chainguard_deployment.test", "charts.#", "2"),
					resource.TestCheckResourceAttr("chainguard_deployment.test", "charts.0.repo", "https://kyverno.github.io/kyverno/"),
					resource.TestCheckResourceAttr("chainguard_deployment.test", "charts.1.repo", "oci://ghcr.io/stefanprodan/charts/podinfo"),
				),
			},
		},
	})
}

func TestAccResourceDeployment_validation(t *testing.T) {
	// Skip this test since we need a valid repo ID for proper testing
	t.Skip("Deployment resource validation needs a valid repo ID")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Test with invalid repo ID
			{
				Config: testAccResourceDeployment(
					"invalid-repo-id",
					[]map[string]interface{}{
						{
							"repo": "oci://ghcr.io/stefanprodan/charts/podinfo",
						},
					},
				),
				ExpectError: regexp.MustCompile("Invalid UIDP format"),
			},
		},
	})
}
