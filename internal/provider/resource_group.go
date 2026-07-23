/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"google.golang.org/protobuf/types/known/fieldmaskpb"

	iamv2 "chainguard.dev/sdk/proto/chainguard/platform/iam/v2beta1"
	iam "chainguard.dev/sdk/proto/platform/iam/v1"
	"chainguard.dev/sdk/uidp"
	"github.com/chainguard-dev/terraform-provider-chainguard/internal/protoutil"
	"github.com/chainguard-dev/terraform-provider-chainguard/internal/token"
	"github.com/chainguard-dev/terraform-provider-chainguard/internal/validators"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &groupResource{}
	_ resource.ResourceWithConfigure   = &groupResource{}
	_ resource.ResourceWithImportState = &groupResource{}
)

// NewGroupResource is a helper function to simplify the provider implementation.
func NewGroupResource() resource.Resource {
	return &groupResource{}
}

// groupResource is the resource implementation.
type groupResource struct {
	managedResource
}

type groupResourceModel struct {
	ID                 types.String `tfsdk:"id"`
	Name               types.String `tfsdk:"name"`
	Description        types.String `tfsdk:"description"`
	ParentID           types.String `tfsdk:"parent_id"`
	Verified           types.Bool   `tfsdk:"verified"`
	VerifiedProtection types.Bool   `tfsdk:"verified_protection"`
	Kind               types.String `tfsdk:"kind"`
	ResourceLimits     types.Map    `tfsdk:"resource_limits"`
}

func (r *groupResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.configure(ctx, req, resp)
}

// Metadata returns the resource type name.
func (r *groupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_group"
}

// Schema defines the schema for the resource.
func (r *groupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	// Sorted so the generated docs are stable across map iteration order.
	orgKinds := make([]string, 0, len(iamv2.OrgKind_value))
	for name, val := range iamv2.OrgKind_value {
		if val == 0 {
			continue
		}
		orgKinds = append(orgKinds, strings.TrimPrefix(name, "ORG_KIND_"))
	}
	slices.Sort(orgKinds)

	resp.Schema = schema.Schema{
		Description: "IAM Group on the Chainguard platform.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "The exact UIDP of this IAM group.",
				Computed:      true,
				Validators:    []validator.String{validators.UIDP(false /* allowRootSentinel */)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"parent_id": schema.StringAttribute{
				Description:   "Parent group (organization or folder) of this group. If not set, this group is an organization.",
				Optional:      true,
				Validators:    []validator.String{validators.UIDP(false /* allowRootSentinel */)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Description: "Name of this IAM group.",
				Required:    true,
			},
			"description": schema.StringAttribute{
				Description: "Description of this IAM group.",
				Optional:    true,
			},
			"verified": schema.BoolAttribute{
				Description: "Whether the organization has been verified by a Chainguardian. Only applicable to organizations (top-level groups).",
				Optional:    true,
			},
			"verified_protection": schema.BoolAttribute{
				Description: "Prevent the group from being unverified through Terraform. Null is treated as true.",
				Optional:    true,
			},
			"kind": schema.StringAttribute{
				Description: "The organization kind. Required when creating a verified organization (top-level group); may only be set on verified organizations. Subgroups inherit kind from their organization. One of: " + strings.Join(orgKinds, ", ") + ".",
				Optional:    true,
				Computed:    true,
				Validators: []validator.String{
					stringvalidator.OneOf(orgKinds...),
					stringvalidator.ConflictsWith(path.MatchRoot("parent_id")),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"resource_limits": schema.MapAttribute{
				Description: "Maximum number of resources allowed by type.",
				Computed:    true,
				ElementType: types.Int32Type,
			},
		},
	}
}

// ImportState imports resources by ID into the current Terraform state.
func (r *groupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// Create creates the resource and sets the initial Terraform state.
func (r *groupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// Read the plan data into the resource model.
	var plan groupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("create group request: name=%s, parent_id=%s", plan.Name, plan.ParentID))

	// Create the group.
	cr := &iamv2.CreateGroupRequest{
		Group: &iamv2.Group{
			Name:        plan.Name.ValueString(),
			Description: plan.Description.ValueString(),
			Verified:    plan.Verified.ValueBool(),
			Kind:        iamv2.OrgKind(iamv2.OrgKind_value["ORG_KIND_"+plan.Kind.ValueString()]),
		},
	}
	// Only include Parent UIDP for non-organization (folder) groups.
	// Due to validation, we are guaranteed ParentID is either a valid UIDP or "/".
	if uidp.Valid(plan.ParentID.ValueString()) {
		cr.Parent = plan.ParentID.ValueString()
	}

	g, err := r.prov.clientV2.IAM().GroupsService().CreateGroup(ctx, cr)
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, fmt.Sprintf("failed to create group %q", cr.Group.Name)))
		return
	}

	// Save group details in the state.
	plan.ID = types.StringValue(g.GetUid())
	// Kind is computed and must resolve to a known value: subgroups
	// inherit it from their organization, unverified groups leave it null.
	if k := g.GetKind(); k != iamv2.OrgKind_ORG_KIND_UNSPECIFIED {
		plan.Kind = types.StringValue(strings.TrimPrefix(k.String(), "ORG_KIND_"))
	} else {
		plan.Kind = types.StringNull()
	}
	if len(g.GetResourceLimits()) > 0 {
		rl, diags := types.MapValueFrom(ctx, types.Int32Type, g.GetResourceLimits())
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		plan.ResourceLimits = rl
	} else {
		plan.ResourceLimits = types.MapNull(types.Int32Type)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)

	// Reauthenticate if an organization was created so the cached token has
	// the new organization in scope.
	if uidp.InRoot(g.GetUid()) {
		if err := r.waitForRoleBindingPropagation(ctx, g.GetUid(), r.prov.loginConfig); err != nil {
			resp.Diagnostics.Append(errorToDiagnostic(err, fmt.Sprintf("failed to verify root group access for %q", g.GetUid())))
			return
		}
	}
}

// waitForRoleBindingPropagation waits for role binding propagation after
// organization creation. Each attempt force-refreshes the cached token so the
// new organization is in scope — clients attach the cached token per RPC, so
// the shared connections pick it up without being replaced. (Replacing them
// would close connections that concurrent resource operations may still have
// RPCs in flight on.) It polls with exponential backoff until the group is
// accessible or times out.
func (r *groupResource) waitForRoleBindingPropagation(
	ctx context.Context,
	groupID string,
	cfg token.LoginConfig,
) error {
	const (
		maxAttempts = 5
		baseDelay   = 2 * time.Second
		maxDelay    = 16 * time.Second
	)

	for attempt := range maxAttempts {
		// Refresh token to pick up new capabilities
		if _, err := token.Get(ctx, cfg, true /* forceRefresh */); err != nil {
			return fmt.Errorf("failed to refresh token: %w", err)
		}

		// Verify group is accessible by attempting to list it
		gl, err := r.prov.client.IAM().Groups().List(ctx, &iam.GroupFilter{
			Id: groupID,
		})

		// Auth errors during propagation are transient — the role binding
		// hasn't propagated yet, so the refreshed token may not have
		// capabilities for the new group. Retry these like empty results.
		if err != nil {
			code := status.Code(err)
			if code != codes.Unauthenticated && code != codes.PermissionDenied {
				// Non-auth errors are not recoverable.
				return fmt.Errorf("failed to list group: %w", err)
			}
			tflog.Debug(ctx, "Auth error during propagation (retrying)", map[string]any{
				"group_id": groupID,
				"attempt":  attempt + 1,
				"error":    err.Error(),
			})
			// Fall through to retry with backoff below.
		} else if len(gl.GetItems()) > 0 {
			// Success: group is accessible.
			tflog.Info(ctx, "Root group accessible", map[string]any{
				"group_id": groupID,
				"attempts": attempt + 1,
			})
			return nil
		}

		// Group not yet visible - retry with backoff (unless this was the last attempt)
		if attempt < maxAttempts-1 {
			// Exponential backoff: 2s, 4s, 8s, 16s
			delay := min(baseDelay<<attempt, maxDelay)

			tflog.Debug(ctx, "Waiting for role binding propagation", map[string]any{
				"group_id": groupID,
				"attempt":  attempt + 1,
				"delay":    delay.String(),
			})

			select {
			case <-time.After(delay):
				// Continue to next attempt
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	return fmt.Errorf("group %q not found after %d attempts", groupID, maxAttempts)
}

// Read refreshes the Terraform state with the latest data.
func (r *groupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Read the current state into the resource model.
	var state groupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("read group request: %s", state.ID))

	// Query for the group using GetGroup instead of List-by-ID.
	groupID := state.ID.ValueString()
	g, err := r.prov.clientV2.IAM().GroupsService().GetGroup(ctx, &iamv2.GetGroupRequest{
		Uid: groupID,
	})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			// Group was already deleted outside TF, remove from state
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to get group"))
		return
	}

	state.ID = types.StringValue(g.GetUid())
	state.Name = types.StringValue(g.GetName())
	// Only update the state description if it started as non-null or we receive a description.
	if !state.Description.IsNull() || g.GetDescription() != "" {
		state.Description = types.StringValue(g.GetDescription())
	}
	// Allow ParentID to remain null for organizations, but ensure it is populated
	// for when importing folders.
	if !state.ParentID.IsNull() || !uidp.InRoot(g.GetUid()) {
		state.ParentID = types.StringValue(uidp.Parent(g.GetUid()))
	}
	// Keep null verified fields null for backward compatibility
	// if it hasn't changed upstream.
	if !state.Verified.IsNull() || g.GetVerified() {
		state.Verified = types.BoolValue(g.GetVerified())
	}
	// Kind is computed: reflect the server value (null when unset).
	if k := g.GetKind(); k != iamv2.OrgKind_ORG_KIND_UNSPECIFIED {
		state.Kind = types.StringValue(strings.TrimPrefix(k.String(), "ORG_KIND_"))
	} else {
		state.Kind = types.StringNull()
	}

	if len(g.GetResourceLimits()) > 0 {
		rl, diags := types.MapValueFrom(ctx, types.Int32Type, g.GetResourceLimits())
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		state.ResourceLimits = rl
	} else {
		state.ResourceLimits = types.MapNull(types.Int32Type)
	}

	// Set state
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *groupResource) update(ctx context.Context, data *groupResourceModel, state groupResourceModel) diag.Diagnostic {
	// Check if the group is attempting to update from verified to unverified,
	// and is protected from being unverified in the current state.
	// NB: When not set, verified_protection is treated as true.
	// This requires unverifying to happen in two steps:
	// apply verified_protection = false, then apply verified = false (or remove the attribute).
	if state.Verified.ValueBool() && !data.Verified.ValueBool() && protoutil.DefaultBool(state.VerifiedProtection, true) {
		return diag.NewErrorDiagnostic("cannot unverify group", fmt.Sprintf("group %s is verified and verified_protection is true or null; apply verified_protection = false before attempting to unverify this group", state.ID.ValueString()))
	}

	ug := &iamv2.Group{
		Uid:         data.ID.ValueString(),
		Name:        data.Name.ValueString(),
		Description: data.Description.ValueString(),
		Verified:    data.Verified.ValueBool(),
	}
	// The server treats a wildcard mask as intent to update every field --
	// including kind and status, which this resource does not set -- and
	// rejects such updates once those fields hold values. Kind must also be
	// named explicitly to take effect, so name only the managed fields and
	// kind only when it changes.
	paths := []string{"name", "description", "verified"}
	if !data.Kind.IsUnknown() && !data.Kind.IsNull() && !data.Kind.Equal(state.Kind) {
		ug.Kind = iamv2.OrgKind(iamv2.OrgKind_value["ORG_KIND_"+data.Kind.ValueString()])
		paths = append(paths, "kind")
	}
	g, err := r.prov.clientV2.IAM().GroupsService().UpdateGroup(ctx, &iamv2.UpdateGroupRequest{
		Group:      ug,
		UpdateMask: &fieldmaskpb.FieldMask{Paths: paths},
	})
	if err != nil {
		return errorToDiagnostic(err, fmt.Sprintf("failed to update group %q", data.ID.ValueString()))
	}

	// Update data from the returned value.
	data.ID = types.StringValue(g.GetUid())
	data.Name = types.StringValue(g.GetName())
	if !data.Description.IsNull() || g.GetDescription() != "" {
		data.Description = types.StringValue(g.GetDescription())
	}
	if !data.Verified.IsNull() || g.GetVerified() {
		data.Verified = types.BoolValue(g.GetVerified())
	}
	if len(g.GetResourceLimits()) > 0 {
		rl, diags := types.MapValueFrom(ctx, types.Int32Type, g.GetResourceLimits())
		if diags.HasError() {
			return diags[0]
		}
		data.ResourceLimits = rl
	} else {
		data.ResourceLimits = types.MapNull(types.Int32Type)
	}
	return nil
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *groupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Read the plan into the resource model.
	var data groupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("update group request: %s", data.ID))

	// Fetch the state to compare the verified property
	var state groupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Attempt to update the group
	resp.Diagnostics.Append(r.update(ctx, &data, state))
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *groupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Read the current state into the resource model.
	var state groupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("delete group request: %s", state.ID))

	id := state.ID.ValueString()
	_, err := r.prov.clientV2.IAM().GroupsService().DeleteGroup(ctx, &iamv2.DeleteGroupRequest{
		Uid: id,
	})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return
		}
		resp.Diagnostics.Append(errorToDiagnostic(err, fmt.Sprintf("failed to delete group %q", id)))
		return
	}
}
