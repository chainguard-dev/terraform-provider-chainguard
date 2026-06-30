/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"fmt"

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
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	iamv2 "chainguard.dev/sdk/proto/chainguard/platform/iam/v2beta1"
	"chainguard.dev/sdk/uidp"
	"github.com/chainguard-dev/terraform-provider-chainguard/internal/validators"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &rolebindingResource{}
	_ resource.ResourceWithConfigure   = &rolebindingResource{}
	_ resource.ResourceWithImportState = &rolebindingResource{}
)

// NewRolebindingResource is a helper function to simplify the provider implementation.
func NewRolebindingResource() resource.Resource {
	return &rolebindingResource{}
}

// rolebindingResource is the resource implementation.
type rolebindingResource struct {
	managedResource
}

type rolebindingResourceModel struct {
	ID       types.String `tfsdk:"id"`
	Group    types.String `tfsdk:"group"`
	Identity types.String `tfsdk:"identity"`
	Role     types.String `tfsdk:"role"`
}

func (r *rolebindingResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.configure(ctx, req, resp)
}

// Metadata returns the resource type name.
func (r *rolebindingResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_rolebinding"
}

// Schema defines the schema for the resource.
func (r *rolebindingResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "IAM role-binding on the Chainguard platform.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "The UIDP of this rolebinding.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"group": schema.StringAttribute{
				Description:   "The id of the IAM group to grant the identity access to with the role's capabilities.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{validators.UIDP(false /* allowRootSentinel */)},
			},
			"identity": schema.StringAttribute{
				Description: "The id of an identity to grant role's capabilities to at the scope of the IAM group.",
				Required:    true,
				Validators:  []validator.String{validators.UIDP(false /* allowRootSentinel */)},
			},
			"role": schema.StringAttribute{
				Description: "The role to grant identity at the scope of the IAM group.",
				Required:    true,
				Validators:  []validator.String{validators.UIDP(false /* allowRootSentinel */)},
			},
		},
	}
}

// ImportState imports resources by ID into the current Terraform state.
func (r *rolebindingResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// Create creates the resource and sets the initial Terraform state.
func (r *rolebindingResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// Read the plan data into the resource model.
	var plan rolebindingResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("create rolebinding request: group=%s, role=%s, identity=%s", plan.Group, plan.Role, plan.Identity))

	// Create the rolebinding. Retry on PermissionDenied to handle eventual
	// consistency when the parent group was just created in the same apply.
	binding, err := retryOnPermissionDenied(ctx, func() (*iamv2.RoleBinding, error) {
		return r.prov.clientV2.IAM().RoleBindingsService().CreateRoleBinding(ctx, &iamv2.CreateRoleBindingRequest{
			Parent: plan.Group.ValueString(),
			RoleBinding: &iamv2.RoleBinding{
				IdentityUid: plan.Identity.ValueString(),
				RoleUid:     plan.Role.ValueString(),
			},
		})
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to create rolebinding"))
		return
	}

	// Save binding details in the state.
	plan.ID = types.StringValue(binding.GetUid())
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state with the latest data.
func (r *rolebindingResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Read the current state into the resource model.
	var state rolebindingResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("read rolebinding request: id=%s", state.ID))

	// Query for the role binding using GetRoleBinding instead of List-by-ID.
	rbID := state.ID.ValueString()
	binding, err := r.prov.clientV2.IAM().RoleBindingsService().GetRoleBinding(ctx, &iamv2.GetRoleBindingRequest{
		Uid: rbID,
	})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			// Role binding doesn't exist or was deleted outside TF
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to get rolebinding"))
		return
	}

	state.ID = types.StringValue(binding.GetUid())
	if g := binding.GetGroup(); g != nil {
		state.Group = types.StringValue(g.GetUid())
	} else {
		state.Group = types.StringValue(uidp.Parent(binding.GetUid()))
	}
	if uid := binding.GetIdentityUid(); uid != "" {
		state.Identity = types.StringValue(uid)
	} else if id := binding.GetIdentity(); id != nil {
		state.Identity = types.StringValue(id.GetUid())
	}
	if uid := binding.GetRoleUid(); uid != "" {
		state.Role = types.StringValue(uid)
	} else if r := binding.GetRole(); r != nil {
		state.Role = types.StringValue(r.GetUid())
	}

	// Set state
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *rolebindingResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Read the plan into the resource model.
	var data rolebindingResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("update rolebinding request: id=%s", data.ID))

	binding, err := r.prov.clientV2.IAM().RoleBindingsService().UpdateRoleBinding(ctx, &iamv2.UpdateRoleBindingRequest{
		RoleBinding: &iamv2.RoleBinding{
			Uid:         data.ID.ValueString(),
			IdentityUid: data.Identity.ValueString(),
			RoleUid:     data.Role.ValueString(),
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"*"}},
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, fmt.Sprintf("failed to update rolebinding %q", data.ID.ValueString())))
		return
	}

	// Set state
	data.ID = types.StringValue(binding.GetUid())
	if uid := binding.GetIdentityUid(); uid != "" {
		data.Identity = types.StringValue(uid)
	} else if id := binding.GetIdentity(); id != nil {
		data.Identity = types.StringValue(id.GetUid())
	}
	if uid := binding.GetRoleUid(); uid != "" {
		data.Role = types.StringValue(uid)
	} else if r := binding.GetRole(); r != nil {
		data.Role = types.StringValue(r.GetUid())
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *rolebindingResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Read the current state into the resource model.
	var state rolebindingResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("delete rolebinding request: id=%s", state.ID))

	id := state.ID.ValueString()
	_, err := r.prov.clientV2.IAM().RoleBindingsService().DeleteRoleBinding(ctx, &iamv2.DeleteRoleBindingRequest{
		Uid: id,
	})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return
		}
		resp.Diagnostics.Append(errorToDiagnostic(err, fmt.Sprintf("failed to delete rolebinding %q", id)))
		return
	}
}
