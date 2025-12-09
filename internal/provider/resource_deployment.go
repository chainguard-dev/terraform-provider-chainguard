/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	registry "chainguard.dev/sdk/proto/platform/registry/v1"
	"github.com/chainguard-dev/terraform-provider-chainguard/internal/validators"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &deploymentResource{}
	_ resource.ResourceWithConfigure   = &deploymentResource{}
	_ resource.ResourceWithImportState = &deploymentResource{}
)

// NewDeploymentResource is a helper function to simplify the provider implementation.
func NewDeploymentResource() resource.Resource {
	return &deploymentResource{}
}

// deploymentResource is the resource implementation.
type deploymentResource struct {
	managedResource
}

type deploymentResourceModel struct {
	ID           types.String `tfsdk:"id"`
	Charts       types.List   `tfsdk:"charts"`
	IgnoreErrors types.Bool   `tfsdk:"ignore_errors"`
}

type helmChartModel struct {
	Source types.String `tfsdk:"source"`
	Repo   types.String `tfsdk:"repo"`
}

func (r *deploymentResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.configure(ctx, req, resp)
}

// Metadata returns the resource type name.
func (r *deploymentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_image_repo_deployment"
}

// Schema defines the schema for the resource.
func (r *deploymentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Deployment configuration for a repository, containing Helm chart sources.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "The UIDP of the repository this deployment belongs to.",
				Required:      true,
				Validators:    []validator.String{validators.UIDP(false /* allowRootSentinel */)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"charts": schema.ListNestedAttribute{
				Description: "List of Helm charts for this deployment.",
				Required:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"source": schema.StringAttribute{
							Description: "Link to the Helm chart source code (e.g., 'https://github.com/kyverno/kyverno').",
							Optional:    true,
						},
						"repo": schema.StringAttribute{
							Description: "Repository URL of the chart (e.g., 'oci://ghcr.io/stefanprodan/charts/podinfo' or 'https://kyverno.github.io/kyverno/').",
							Required:    true,
						},
					},
				},
			},
			"ignore_errors": schema.BoolAttribute{
				Description: "If true, deployment errors (like permission denied) will be logged as warnings instead of blocking the operation. Useful to prevent deployment failures from blocking image builds.",
				Optional:    true,
			},
		},
	}
}

// ImportState imports resources by ID into the current Terraform state.
func (r *deploymentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// Create creates the resource and sets the initial Terraform state.
func (r *deploymentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// Read the plan data into the resource model.
	var plan deploymentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("create deployment request: repo_id=%s", plan.ID))

	// Convert charts from Terraform types to proto
	helmCharts := r.convertChartsToProto(ctx, plan.Charts, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// Try to create the deployment first
	d, err := r.prov.client.Registry().Registry().CreateDeployment(ctx, &registry.CreateDeploymentRequest{
		ParentId: plan.ID.ValueString(), // The repo UIDP
		Charts:   helmCharts,
	})

	// If creation fails with AlreadyExists, update the existing deployment instead
	if err != nil {
		if st, ok := status.FromError(err); ok && st.Code() == codes.AlreadyExists {
			tflog.Info(ctx, fmt.Sprintf("Deployment already exists for repo %s, updating instead", plan.ID.ValueString()))
			d, err = r.prov.client.Registry().Registry().UpdateDeployment(ctx, &registry.UpdateDeploymentRequest{
				RepoId: plan.ID.ValueString(), // The repo UIDP
				Charts: helmCharts,
			})
		}
	}
	if err != nil {
		if plan.IgnoreErrors.ValueBool() {
			// Log as warning instead of failing
			tflog.Warn(ctx, fmt.Sprintf("deployment creation failed but continuing due to ignore_errors=true: %v", err))
			resp.Diagnostics.AddWarning(
				"Deployment Creation Failed",
				fmt.Sprintf("Failed to create deployment for repo %q: %v. Continuing due to ignore_errors=true.", plan.ID.ValueString(), err),
			)
			// Keep the planned charts in state for consistency between plan and apply
			resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
			return
		}
		resp.Diagnostics.Append(errorToDiagnostic(err, fmt.Sprintf("failed to create deployment for repo %q", plan.ID.ValueString())))
		return
	}

	// Update plan with returned data
	plan.ID = types.StringValue(d.Id)
	plan.Charts = r.convertChartsFromProto(ctx, d.Charts, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state with the latest data.
func (r *deploymentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Read the current state into the resource model.
	var state deploymentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("read deployment request: %s", state.ID))

	// Get the deployment for the repository
	d, err := r.prov.client.Registry().Registry().GetDeployment(ctx, &registry.GetDeploymentRequest{
		RepoId: state.ID.ValueString(), // The repo UIDP
	})
	if err != nil {
		if state.IgnoreErrors.ValueBool() {
			// Log as warning and remove from state
			tflog.Warn(ctx, fmt.Sprintf("deployment read failed but continuing due to ignore_errors=true: %v", err))
			resp.Diagnostics.AddWarning(
				"Deployment Read Failed",
				fmt.Sprintf("Failed to read deployment for repo %q: %v. Removing from state due to ignore_errors=true.", state.ID.ValueString(), err),
			)
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.Append(errorToDiagnostic(err, fmt.Sprintf("failed to get deployment for repo %q", state.ID.ValueString())))
		return
	}

	// If deployment is nil or has no charts, remove from state
	if d == nil || len(d.Charts) == 0 {
		resp.State.RemoveResource(ctx)
		return
	}

	// Update state with deployment data
	state.ID = types.StringValue(d.Id)
	state.Charts = r.convertChartsFromProto(ctx, d.Charts, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *deploymentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Read the plan into the resource model.
	var data deploymentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("update deployment request: %s", data.ID))

	// Convert charts from Terraform types to proto
	helmCharts := r.convertChartsToProto(ctx, data.Charts, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// Update the deployment
	d, err := r.prov.client.Registry().Registry().UpdateDeployment(ctx, &registry.UpdateDeploymentRequest{
		RepoId: data.ID.ValueString(), // The repo UIDP
		Charts: helmCharts,
	})
	if err != nil {
		if data.IgnoreErrors.ValueBool() {
			// Log as warning and keep existing state
			tflog.Warn(ctx, fmt.Sprintf("deployment update failed but continuing due to ignore_errors=true: %v", err))
			resp.Diagnostics.AddWarning(
				"Deployment Update Failed",
				fmt.Sprintf("Failed to update deployment for repo %q: %v. Keeping existing state due to ignore_errors=true.", data.ID.ValueString(), err),
			)
			// Keep the planned state as-is
			resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
			return
		}
		resp.Diagnostics.Append(errorToDiagnostic(err, fmt.Sprintf("failed to update deployment for repo %q", data.ID.ValueString())))
		return
	}

	// Update data with returned values
	data.ID = types.StringValue(d.Id)
	data.Charts = r.convertChartsFromProto(ctx, d.Charts, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *deploymentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Read the current state into the resource model.
	var state deploymentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("delete deployment request: %s", state.ID))

	// Delete by clearing charts
	_, err := r.prov.client.Registry().Registry().UpdateDeployment(ctx, &registry.UpdateDeploymentRequest{
		RepoId: state.ID.ValueString(),  // The repo UIDP
		Charts: []*registry.HelmChart{}, // Empty charts array to clear deployment
	})
	if err != nil {
		if state.IgnoreErrors.ValueBool() {
			// Log as warning and consider deletion successful
			tflog.Warn(ctx, fmt.Sprintf("deployment deletion failed but continuing due to ignore_errors=true: %v", err))
			resp.Diagnostics.AddWarning(
				"Deployment Deletion Failed",
				fmt.Sprintf("Failed to delete deployment for repo %q: %v. Considering deletion successful due to ignore_errors=true.", state.ID.ValueString(), err),
			)
			return
		}
		resp.Diagnostics.Append(errorToDiagnostic(err, fmt.Sprintf("failed to delete deployment for repo %q", state.ID.ValueString())))
		return
	}
}

// convertChartsToProto converts Terraform chart models to proto HelmChart slice.
func (r *deploymentResource) convertChartsToProto(ctx context.Context, charts types.List, diags *diag.Diagnostics) []*registry.HelmChart {
	var chartModels []helmChartModel
	diags.Append(charts.ElementsAs(ctx, &chartModels, false)...)
	if diags.HasError() {
		return nil
	}

	var helmCharts []*registry.HelmChart
	for _, chartModel := range chartModels {
		chart := &registry.HelmChart{
			Repo: chartModel.Repo.ValueString(),
		}
		if !chartModel.Source.IsNull() {
			source := chartModel.Source.ValueString()
			chart.Source = &source
		}
		helmCharts = append(helmCharts, chart)
	}
	return helmCharts
}

// convertChartsFromProto converts proto HelmChart slice to Terraform types.List.
func (r *deploymentResource) convertChartsFromProto(ctx context.Context, protoCharts []*registry.HelmChart, diags *diag.Diagnostics) types.List {
	chartElements := make([]helmChartModel, len(protoCharts))
	for i, chart := range protoCharts {
		chartElements[i] = helmChartModel{
			Repo: types.StringValue(chart.Repo),
		}
		if chart.Source != nil {
			chartElements[i].Source = types.StringValue(*chart.Source)
		} else {
			chartElements[i].Source = types.StringNull()
		}
	}

	chartsList, chartsDiags := types.ListValueFrom(ctx, types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"source": types.StringType,
			"repo":   types.StringType,
		},
	}, chartElements)
	diags.Append(chartsDiags...)
	return chartsList
}
