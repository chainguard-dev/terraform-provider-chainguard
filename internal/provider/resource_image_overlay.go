/*
Copyright 2026 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	regv2 "chainguard.dev/sdk/proto/chainguard/platform/registry/v2beta1"
	"chainguard.dev/sdk/uidp"
	"github.com/chainguard-dev/terraform-provider-chainguard/internal/validators"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &imageOverlayResource{}
	_ resource.ResourceWithConfigure   = &imageOverlayResource{}
	_ resource.ResourceWithImportState = &imageOverlayResource{}
)

// NewImageOverlayResource is a helper function to simplify the provider implementation.
func NewImageOverlayResource() resource.Resource {
	return &imageOverlayResource{}
}

// imageOverlayResource is the resource implementation.
type imageOverlayResource struct {
	managedResource
}

type imageOverlayResourceModel struct {
	ID       types.String `tfsdk:"id"`
	ParentID types.String `tfsdk:"parent_id"`
	Name     types.String `tfsdk:"name"`
	Packages types.List   `tfsdk:"packages"`
}

func (r *imageOverlayResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.configure(ctx, req, resp)
}

// Metadata returns the resource type name.
func (r *imageOverlayResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_image_overlay"
}

// Schema defines the schema for the resource.
func (r *imageOverlayResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A named, reusable Custom Assembly overlay, attached to image repos with chainguard_image_overlay_binding.",
		// NB: There is no overlay update method so all attributes must
		// have a RequiresReplace PlanModifier. The API workflow is
		// delete-and-recreate.
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "The UIDP of this overlay.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"parent_id": schema.StringAttribute{
				Description:   "The IAM group that owns the overlay.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{
					validators.UIDP(false /* allowRootSentinel */),
				},
			},
			"name": schema.StringAttribute{
				Description:   "The name of this overlay, unique among the group's overlays.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{
					validators.Name(),
				},
			},
			"packages": schema.ListAttribute{
				Description: "Packages to append to images the overlay is bound to. At least one is required.",
				Required:    true,
				ElementType: types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
				},
				Validators: []validator.List{
					listvalidator.SizeAtLeast(1),
				},
			},
		},
	}
}

// ImportState imports resources by ID into the current Terraform state.
func (r *imageOverlayResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// Create creates the resource and sets the initial Terraform state.
func (r *imageOverlayResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// Read the plan data into the resource model.
	var plan imageOverlayResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("create image overlay request: parent_id=%s, name=%s", plan.ParentID, plan.Name))

	packages := make([]string, 0, len(plan.Packages.Elements()))
	resp.Diagnostics.Append(plan.Packages.ElementsAs(ctx, &packages, false /* allowUnhandled */)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Retry on PermissionDenied to handle eventual consistency when the
	// parent group was just created in the same apply.
	overlay, err := retryOnPermissionDenied(ctx, func() (*regv2.Overlay, error) {
		return r.prov.clientV2.Registry().OverlaysService().CreateOverlay(ctx, &regv2.CreateOverlayRequest{
			Parent: plan.ParentID.ValueString(),
			Overlay: &regv2.Overlay{
				Name: plan.Name.ValueString(),
				Config: &regv2.CustomOverlay{
					Contents: &regv2.CustomOverlay_ImageContents{
						Packages: packages,
					},
				},
			},
		})
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to create image overlay"))
		return
	}

	// Save overlay details in the state.
	plan.ID = types.StringValue(overlay.GetUid())
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state with the latest data.
func (r *imageOverlayResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Read the current state into the resource model.
	var state imageOverlayResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("read image overlay request: %s", state.ID))

	overlay, err := r.prov.clientV2.Registry().OverlaysService().GetOverlay(ctx, &regv2.GetOverlayRequest{
		Uid: state.ID.ValueString(),
	})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			// Overlay was deleted outside TF, remove from state.
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to get image overlay"))
		return
	}

	state.ID = types.StringValue(overlay.GetUid())
	state.ParentID = types.StringValue(uidp.Parent(overlay.GetUid()))
	state.Name = types.StringValue(overlay.GetName())

	packages, diags := types.ListValueFrom(ctx, types.StringType, overlay.GetConfig().GetContents().GetPackages())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.Packages = packages

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *imageOverlayResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	// All attributes require replacement, so this is never invoked.
	resp.Diagnostics.AddError("update unsupported", "Updating an image overlay is not supported; overlays are replaced.")
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *imageOverlayResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Read the current state into the resource model.
	var state imageOverlayResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("delete image overlay request: %s", state.ID))

	id := state.ID.ValueString()
	if _, err := r.prov.clientV2.Registry().OverlaysService().DeleteOverlay(ctx, &regv2.DeleteOverlayRequest{
		Uid: id,
	}); err != nil {
		// Deleting an overlay still referenced by bindings fails with
		// FailedPrecondition; surface the API error as-is.
		resp.Diagnostics.Append(errorToDiagnostic(err, fmt.Sprintf("failed to delete image overlay %q", id)))
	}
}
