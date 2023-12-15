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

	common "chainguard.dev/sdk/proto/platform/common/v1"
	iam "chainguard.dev/sdk/proto/platform/iam/v1"
	"chainguard.dev/sdk/uidp"
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
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	ParentID    types.String `tfsdk:"parent_id"`
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
				Description:   "Parent IAM group of this group. If not set, this group is assumed to be a root group.",
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
	cr := &iam.CreateGroupRequest{
		Group: &iam.Group{
			Name:        plan.Name.ValueString(),
			Description: plan.Description.ValueString(),
		},
	}
	// Only include Parent UIDP for non-root groups.
	// Due to validation, we are guaranteed ParentID is either a valid UIDP or "/".
	if uidp.Valid(plan.ParentID.ValueString()) {
		cr.Parent = plan.ParentID.ValueString()
	}

	g, err := r.prov.client.IAM().Groups().Create(ctx, cr)
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, fmt.Sprintf("failed to create group %q", cr.Group.Name)))
		return
	}

	// Save group details in the state.
	plan.ID = types.StringValue(g.Id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)

	// Attempt to reauthenticate if root group was created so token
	// has new root group in scope.
	if uidp.InRoot(g.Id) {
		cfg := r.prov.loginConfig
		cgToken, err := token.Get(ctx, cfg, true /* forceRefresh */)
		if err != nil {
			resp.Diagnostics.Append(errorToDiagnostic(err, "failed to refresh Chainguard token"))
			return
		}
		clients, err := newPlatformClients(ctx, string(cgToken), r.prov.consoleAPI)
		if err != nil {
			resp.Diagnostics.Append(errorToDiagnostic(err, "failed to create new platform clients"))
			return
		}
		r.prov.client = clients
	}
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

	// Query for the group to update state
	uf := &common.UIDPFilter{}
	if uidp.Valid(state.ParentID.ValueString()) {
		uf.ChildrenOf = state.ParentID.ValueString()
	}
	f := &iam.GroupFilter{
		Id:   state.ID.ValueString(),
		Name: state.Name.ValueString(),
		Uidp: uf,
	}
	groupList, err := r.prov.client.IAM().Groups().List(ctx, f)
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to list groups"))
		return
	}

	switch c := len(groupList.GetItems()); {
	case c == 0:
		// Group was already deleted outside TF, remove from state
		resp.State.RemoveResource(ctx)

	case c == 1:
		g := groupList.GetItems()[0]
		state.ID = types.StringValue(g.Id)
		state.Name = types.StringValue(g.Name)
		// Only update the state description if it started as non-null or we receive a description.
		if !(state.Description.IsNull() && g.Description == "") {
			state.Description = types.StringValue(g.Description)
		}
		// Allow ParentID to remain null for root groups, but ensure it is populated
		// for when importing non-root groups.
		if !state.ParentID.IsNull() || !uidp.InRoot(g.Id) {
			state.ParentID = types.StringValue(uidp.Parent(g.Id))
		}

		// Set state
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)

	default:
		tflog.Error(ctx, fmt.Sprintf("group list returned %d groups for filter %v", c, f))
		resp.Diagnostics.AddError("more than one group found matching filters", fmt.Sprintf("filters=%v\nPlease provide more context to narrow query (e.g. parent_id).", state))
	}
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

	g, err := r.prov.client.IAM().Groups().Update(ctx, &iam.Group{
		Id:          data.ID.ValueString(),
		Name:        data.Name.ValueString(),
		Description: data.Description.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, fmt.Sprintf("failed to update group %q", data.ID.ValueString())))
		return
	}

	// Set state.
	data.ID = types.StringValue(g.Id)
	data.Name = types.StringValue(g.GetName())
	if !(data.Description.IsNull() && g.Description != "") {
		data.Description = types.StringValue(g.GetDescription())
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
	_, err := r.prov.client.IAM().Groups().Delete(ctx, &iam.DeleteGroupRequest{
		Id: id,
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, fmt.Sprintf("failed to delete group %q", id)))
		return
	}
}
