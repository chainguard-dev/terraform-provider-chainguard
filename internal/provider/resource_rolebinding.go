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

	"chainguard.dev/api/proto/platform/iam"
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
		Description: "IAM Rolebidning in the Chainguard platform.",
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
				Validators:    []validator.String{validators.UIDP(false /* allowRoot */)},
			},
			"identity": schema.StringAttribute{
				Description: "The id of an identity to grant role's capabilities to at the scope of the IAM group.",
				Required:    true,
				Validators:  []validator.String{validators.UIDP(false /* allowRoot */)},
			},
			"role": schema.StringAttribute{
				Description: "The role to grant identity at the scope of the IAM group.",
				Required:    true,
				Validators:  []validator.String{validators.UIDP(false /* allowRoot */)},
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

	// Create the rolebinding.
	binding, err := r.prov.client.IAM().RoleBindings().Create(ctx, &iam.CreateRoleBindingRequest{
		Parent: plan.Group.ValueString(),
		RoleBinding: &iam.RoleBinding{
			Identity: plan.Identity.ValueString(),
			Role:     plan.Role.ValueString(),
		},
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to create rolebinding"))
		return
	}

	// Save binding details in the state.
	plan.ID = types.StringValue(binding.Id)
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

	// Query for the role to update state
	rbID := state.ID.ValueString()
	bindingList, err := r.prov.client.IAM().RoleBindings().List(ctx, &iam.RoleBindingFilter{
		Id: rbID,
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to list rolebindings"))
		return
	}

	switch c := len(bindingList.GetItems()); {
	case c == 0:
		// Role doesn't exist or was deleted outside TF
		resp.State.RemoveResource(ctx)

	case c == 1:
		binding := bindingList.GetItems()[0]
		state.ID = types.StringValue(binding.Id)
		state.Group = types.StringValue(binding.Group.Id)
		state.Identity = types.StringValue(binding.Identity)
		state.Role = types.StringValue(binding.Role.Id)

		// Set state
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)

	default:
		tflog.Error(ctx, fmt.Sprintf("rolebinding list returned %d bindings for id %q", c, rbID))
		resp.Diagnostics.AddError("internal error", fmt.Sprintf("fatal data corruption: id %s matched more than one rolebinding", rbID))
	}
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

	binding, err := r.prov.client.IAM().RoleBindings().Update(ctx, &iam.RoleBinding{
		Id:       data.ID.ValueString(),
		Identity: data.Identity.ValueString(),
		Role:     data.Role.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, fmt.Sprintf("failed to update rolebinding %q", data.ID.ValueString())))
		return
	}

	// Set state
	data.ID = types.StringValue(binding.Id)
	data.Identity = types.StringValue(binding.Identity)
	data.Role = types.StringValue(binding.Role)
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
	_, err := r.prov.client.IAM().RoleBindings().Delete(ctx, &iam.DeleteRoleBindingRequest{
		Id: id,
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, fmt.Sprintf("failed to delete rolebinding %q", id)))
		return
	}
}
