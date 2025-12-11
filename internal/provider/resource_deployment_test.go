/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func testAccResourceDeployment(repoID string, charts []map[string]any) string {
	return testAccResourceDeploymentWithIgnoreErrors(repoID, charts, false)
}

func testAccResourceDeploymentWithIgnoreErrors(repoID string, charts []map[string]any, ignoreErrors bool) string {
	const tmpl = `
resource "chainguard_image_repo_deployment" "test" {
	id = %q
	charts = [
%s	]%s
}
`
	var chartConfigs []string
	for _, chart := range charts {
		chartConfig := fmt.Sprintf("\t\t{\n\t\t\trepo = %q\n", chart["repo"])
		if source, ok := chart["source"]; ok && source != "" {
			chartConfig += fmt.Sprintf("\t\t\tsource = %q\n", source)
		}
		if chartName, ok := chart["chart"]; ok && chartName != "" {
			chartConfig += fmt.Sprintf("\t\t\tchart = %q\n", chartName)
		}
		chartConfig += "\t\t},\n"
		chartConfigs = append(chartConfigs, chartConfig)
	}
	var ignoreErrorsLine string
	if ignoreErrors {
		ignoreErrorsLine = "\n\tignore_errors = true"
	}
	return fmt.Sprintf(tmpl, repoID, strings.Join(chartConfigs, ""), ignoreErrorsLine)
}

func TestAccResourceDeployment_basic(t *testing.T) {
	repoID := os.Getenv("TF_ACC_REPO_ID")
	if repoID == "" {
		t.Skip("TF_ACC_REPO_ID not set - skipping deployment acceptance test")
		return
	}

	charts := []map[string]any{
		{
			"repo":   "oci://ghcr.io/stefanprodan/charts/podinfo",
			"source": "https://github.com/stefanprodan/podinfo",
		},
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: testAccResourceDeployment(repoID, charts),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("chainguard_image_repo_deployment.test", "id", repoID),
					resource.TestCheckResourceAttr("chainguard_image_repo_deployment.test", "charts.#", "1"),
					resource.TestCheckResourceAttr("chainguard_image_repo_deployment.test", "charts.0.repo", "oci://ghcr.io/stefanprodan/charts/podinfo"),
					resource.TestCheckResourceAttr("chainguard_image_repo_deployment.test", "charts.0.source", "https://github.com/stefanprodan/podinfo"),
					resource.TestCheckNoResourceAttr("chainguard_image_repo_deployment.test", "charts.0.chart"),
				),
			},
			// ImportState testing
			{
				ResourceName:      "chainguard_image_repo_deployment.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Update and Read testing
			{
				Config: testAccResourceDeployment(repoID, []map[string]any{
					{
						"repo":  "https://kyverno.github.io/kyverno/",
						"chart": "kyverno",
					},
					{
						"repo":   "oci://ghcr.io/stefanprodan/charts/podinfo",
						"source": "https://github.com/stefanprodan/podinfo",
					},
				}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("chainguard_image_repo_deployment.test", "charts.#", "2"),
					resource.TestCheckResourceAttr("chainguard_image_repo_deployment.test", "charts.0.repo", "https://kyverno.github.io/kyverno/"),
					resource.TestCheckNoResourceAttr("chainguard_image_repo_deployment.test", "charts.0.source"),
					resource.TestCheckResourceAttr("chainguard_image_repo_deployment.test", "charts.0.chart", "kyverno"),
					resource.TestCheckResourceAttr("chainguard_image_repo_deployment.test", "charts.1.repo", "oci://ghcr.io/stefanprodan/charts/podinfo"),
					resource.TestCheckResourceAttr("chainguard_image_repo_deployment.test", "charts.1.source", "https://github.com/stefanprodan/podinfo"),
					resource.TestCheckNoResourceAttr("chainguard_image_repo_deployment.test", "charts.1.chart"),
				),
			},
		},
	})
}

func TestAccResourceDeployment_chartsOnly(t *testing.T) {
	repoID := os.Getenv("TF_ACC_REPO_ID")
	if repoID == "" {
		t.Skip("TF_ACC_REPO_ID not set - skipping deployment acceptance test")
		return
	}

	// Test charts without source field (repo-only charts)
	chartsNoSource := []map[string]any{
		{
			"repo": "https://kyverno.github.io/kyverno/",
		},
		{
			"repo": "https://prometheus-community.github.io/helm-charts",
		},
	}

	// Test single chart with ignore_errors
	chartsWithIgnoreErrors := []map[string]any{
		{
			"repo": "https://charts.bitnami.com/bitnami",
		},
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Test multiple charts without source fields
			{
				Config: testAccResourceDeployment(repoID, chartsNoSource),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("chainguard_image_repo_deployment.test", "id", repoID),
					resource.TestCheckResourceAttr("chainguard_image_repo_deployment.test", "charts.#", "2"),
					resource.TestCheckResourceAttr("chainguard_image_repo_deployment.test", "charts.0.repo", "https://kyverno.github.io/kyverno/"),
					resource.TestCheckNoResourceAttr("chainguard_image_repo_deployment.test", "charts.0.source"),
					resource.TestCheckNoResourceAttr("chainguard_image_repo_deployment.test", "charts.0.chart"),
					resource.TestCheckResourceAttr("chainguard_image_repo_deployment.test", "charts.1.repo", "https://prometheus-community.github.io/helm-charts"),
					resource.TestCheckNoResourceAttr("chainguard_image_repo_deployment.test", "charts.1.source"),
					resource.TestCheckNoResourceAttr("chainguard_image_repo_deployment.test", "charts.1.chart"),
				),
			},
			// Test ignore_errors = true
			{
				Config: testAccResourceDeploymentWithIgnoreErrors(repoID, chartsWithIgnoreErrors, true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("chainguard_image_repo_deployment.test", "id", repoID),
					resource.TestCheckResourceAttr("chainguard_image_repo_deployment.test", "ignore_errors", "true"),
					resource.TestCheckResourceAttr("chainguard_image_repo_deployment.test", "charts.#", "1"),
					resource.TestCheckResourceAttr("chainguard_image_repo_deployment.test", "charts.0.repo", "https://charts.bitnami.com/bitnami"),
					resource.TestCheckNoResourceAttr("chainguard_image_repo_deployment.test", "charts.0.source"),
					resource.TestCheckNoResourceAttr("chainguard_image_repo_deployment.test", "charts.0.chart"),
				),
			},
		},
	})
}
