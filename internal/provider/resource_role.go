/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"fmt"

	"github.com/chainguard-dev/terraform-provider-chainguard/internal/validators"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"chainguard.dev/sdk/pkg/uidp"
	iam "chainguard.dev/sdk/proto/platform/iam/v1"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &roleResource{}
	_ resource.ResourceWithConfigure   = &roleResource{}
	_ resource.ResourceWithImportState = &roleResource{}
)

// NewRoleResource is a helper function to simplify the provider implementation.
func NewRoleResource() resource.Resource {
	return &roleResource{}
}

// roleResource is the resource implementation.
type roleResource struct {
	managedResource
}

type roleResourceModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	Description  types.String `tfsdk:"description"`
	ParentID     types.String `tfsdk:"parent_id"`
	Capabilities types.List   `tfsdk:"capabilities"`
}

func (r *roleResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.configure(ctx, req, resp)
}

// Metadata returns the resource type name.
func (r *roleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role"
}

// Schema defines the schema for the resource.
func (r *roleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "IAM Role in the Chainguard platform.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "The UIDP of this role.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Description: "The name of this role.",
				Required:    true,
			},
			"description": schema.StringAttribute{
				Description: "An optional longer description of this role.",
				Optional:    true,
			},
			"parent_id": schema.StringAttribute{
				Description:   "The group containing this role",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"capabilities": schema.ListAttribute{
				Description: "The list of capabilities to grant this role",
				Required:    true,
				ElementType: types.StringType,
				Validators: []validator.List{
					listvalidator.SizeAtLeast(1),
					listvalidator.ValueStringsAre(validators.Capability()),
				},
			},
		},
	}
}

// ImportState imports resources by ID into the current Terraform state.
func (r *roleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// Create creates the resource and sets the initial Terraform state.
func (r *roleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// Read the plan data into the resource model.
	var plan roleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("create role request: name=%s, parent_id=%s", plan.Name, plan.ParentID))

	// Create the role.
	caps := make([]string, 0, len(plan.Capabilities.Elements()))
	resp.Diagnostics.Append(plan.Capabilities.ElementsAs(ctx, &caps, false /* allowUnhandled */)...)
	if resp.Diagnostics.HasError() {
		return
	}

	role, err := r.prov.client.IAM().Roles().Create(ctx, &iam.CreateRoleRequest{
		ParentId: plan.ParentID.ValueString(),
		Role: &iam.Role{
			Name:         plan.Name.ValueString(),
			Description:  plan.Description.ValueString(),
			Capabilities: caps,
		},
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to create role"))
		return
	}

	// Save role details in the state.
	plan.ID = types.StringValue(role.Id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state with the latest data.
func (r *roleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Read the current state into the resource model.
	var state roleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("read role request: %s", state.ID))

	// Query for the role to update state
	roleID := state.ID.ValueString()
	roleList, err := r.prov.client.IAM().Roles().List(ctx, &iam.RoleFilter{
		Id: roleID,
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to list roles"))
		return
	}

	switch c := len(roleList.GetItems()); {
	case c == 0:
		// Role doesn't exist or was deleted outside TF
		resp.State.RemoveResource(ctx)

	case c == 1:
		r := roleList.GetItems()[0]
		state.ID = types.StringValue(r.Id)
		state.Name = types.StringValue(r.Name)
		state.Description = types.StringValue(r.Description)
		state.ParentID = types.StringValue(uidp.Parent(r.Id))

		var diags diag.Diagnostics
		state.Capabilities, diags = types.ListValueFrom(ctx, types.StringType, r.Capabilities)
		if diags.HasError() {
			resp.Diagnostics.Append(diags...)
			return
		}

		// Set state
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)

	default:
		tflog.Error(ctx, fmt.Sprintf("role list returned %d roles for id %q", c, roleID))
		resp.Diagnostics.AddError("internal error", fmt.Sprintf("fatal data corruption: id %s matched more than one role", roleID))
	}
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *roleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Read the plan into the resource model.
	var data roleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("update role request: %s", data.ID))

	caps := make([]string, 0, len(data.Capabilities.Elements()))
	resp.Diagnostics.Append(data.Capabilities.ElementsAs(ctx, &caps, false /* allowUnhandled */)...)
	if resp.Diagnostics.HasError() {
		return
	}

	role, err := r.prov.client.IAM().Roles().Update(ctx, &iam.Role{
		Id:           data.ID.ValueString(),
		Name:         data.Name.ValueString(),
		Description:  data.Description.ValueString(),
		Capabilities: caps,
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, fmt.Sprintf("failed to update role %q", data.ID.ValueString())))
		return
	}

	// Set state
	var diags diag.Diagnostics
	data.ID = types.StringValue(role.Id)
	data.Name = types.StringValue(role.GetName())
	data.Description = types.StringValue(role.GetDescription())
	data.Capabilities, diags = types.ListValueFrom(ctx, types.StringType, role.Capabilities)
	if diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *roleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Read the current state into the resource model.
	var state roleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("delete role request: %s", state.ID))

	id := state.ID.ValueString()
	_, err := r.prov.client.IAM().Roles().Delete(ctx, &iam.DeleteRoleRequest{
		Id: id,
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, fmt.Sprintf("failed to delete role %q", id)))
		return
	}
}
