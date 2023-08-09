/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"google.golang.org/protobuf/types/known/durationpb"

	"chainguard.dev/api/proto/platform/iam"
	"github.com/chainguard-dev/terraform-provider-chainguard/internal/validators"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &groupInviteResource{}
	_ resource.ResourceWithConfigure   = &groupInviteResource{}
	_ resource.ResourceWithImportState = &groupInviteResource{}
)

// NewGroupInviteResource is a helper function to simplify the provider implementation.
func NewGroupInviteResource() resource.Resource {
	return &groupInviteResource{}
}

// groupInviteResource is the resource implementation.
type groupInviteResource struct {
	managedResource
}

type groupInviteResourceModel struct {
	ID         types.String `tfsdk:"id"`
	Group      types.String `tfsdk:"group"`
	Expiration types.String `tfsdk:"expiration"`
	Role       types.String `tfsdk:"role"`
	Email      types.String `tfsdk:"email"`
	Code       types.String `tfsdk:"code"`
}

func (r *groupInviteResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.configure(ctx, req, resp)
}

// Metadata returns the resource type name.
func (r *groupInviteResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_group_invite"
}

// Schema defines the schema for the resource.
func (r *groupInviteResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "IAM group invite on the Chainguard platform.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "The id of the group invite.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"group": schema.StringAttribute{
				Description:   "The Group to which this invite code grants access.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{validators.UIDP(false /* allowRoot */)},
			},
			"expiration": schema.StringAttribute{
				Description:   "The RFC3339 encoded date and time at which this invitation will no longer be valid.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{
					validators.RunFuncs(checkRFC3339),
				},
			},
			"role": schema.StringAttribute{
				Description:   "The role that this invite code grants.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{validators.UIDP(false /* allowRoot */)},
			},
			"email": schema.StringAttribute{
				Description:   "The email address of the identity that is allowed to accept this invite code.",
				Optional:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				// TODO: Check valid email address
			},
			"code": schema.StringAttribute{
				Description: "A time-bounded token that may be used at registration to obtain access to a prespecified group with a prespecified role.",
				Computed:    true,
				// This keeps the value from being printed by terraform, but it
				// is stored in the terraform state, so the state should be
				// treated as sensitive.
				// https://developer.hashicorp.com/terraform/language/state/sensitive-data
				Sensitive: true,
			},
		},
	}
}

// ImportState imports resources by ID into the current Terraform state.
func (r *groupInviteResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// Create creates the resource and sets the initial Terraform state.
func (r *groupInviteResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// Read the plan data into the resource model.
	var plan groupInviteResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("create group invite request: group=%s, role=%s, expiration=%s", plan.Group, plan.Role, plan.Expiration))

	ts, err := time.Parse(time.RFC3339, plan.Expiration.ValueString())
	if err != nil {
		// This shouldn't happen with our validation.
		resp.Diagnostics.Append(errorToDiagnostic(err, "error parsing expiration"))
		return
	}

	invite, err := r.prov.client.IAM().GroupInvites().Create(ctx, &iam.GroupInviteRequest{
		Group: plan.Group.ValueString(),
		Ttl:   durationpb.New(time.Until(ts)),
		Role:  plan.Role.ValueString(),
		Email: plan.Email.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to create group invite"))
		return
	}

	// Save group invite details in the state.
	plan.ID = types.StringValue(invite.Id)
	plan.Code = types.StringValue(invite.Code)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state with the latest data.
func (r *groupInviteResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Read the current state into the resource model.
	var state groupInviteResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("read group invite request: %s", state.ID))

	// Query for the group to update state
	inviteList, err := r.prov.client.IAM().GroupInvites().List(ctx, &iam.GroupInviteFilter{
		Id: state.ID.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to list group invites"))
		return
	}

	switch c := len(inviteList.GetItems()); {
	case c == 0:
		// Group was already deleted outside TF, remove from state
		resp.State.RemoveResource(ctx)

	case c == 1:
		// TODO: We cannot read the code, so are there any useful fields to set?

		// Set state
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)

	default:
		tflog.Error(ctx, fmt.Sprintf("group invite list returned %d invites for id %s", c, state.ID.ValueString()))
		resp.Diagnostics.AddError("failed to list group invites", fmt.Sprintf("more than one group invite found matching id %s", state.ID.ValueString()))
	}
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *groupInviteResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Read the plan into the resource model.
	var data groupInviteResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("update group invite request: %s", data.ID))

	// TODO: do we throw an error? This should be unreachable.

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *groupInviteResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Read the current state into the resource model.
	var state groupInviteResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("delete group invite request: %s", state.ID))

	id := state.ID.ValueString()
	_, err := r.prov.client.IAM().GroupInvites().Delete(ctx, &iam.DeleteGroupInviteRequest{
		Id: state.ID.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, fmt.Sprintf("failed to delete group invite %q", id)))
	}
}
