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

	"chainguard.dev/api/pkg/uidp"
	"chainguard.dev/api/proto/platform"
	"chainguard.dev/api/proto/platform/common"
	"chainguard.dev/api/proto/platform/iam"
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
	client platform.Clients
}

type groupResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	ParentID    types.String `tfsdk:"parent_id"`
}

func (r *groupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(platform.Clients)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected platform.Clients, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.client = client
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
				Validators:    []validator.String{validators.UIDPValidator{}},
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"parent_id": schema.StringAttribute{
				Description:   "Parent IAM group of this group. If not set, this group is assumed to be a root group.",
				Optional:      true,
				Validators:    []validator.String{validators.UIDPValidator{}},
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
	tflog.Info(ctx, "read group request", map[string]interface{}{"request": req})

	// Read the plan data into the resource model.
	var plan groupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Create the group.
	cr := &iam.CreateGroupRequest{
		Parent: plan.ParentID.ValueString(),
		Group: &iam.Group{
			Name:        plan.Name.ValueString(),
			Description: plan.Description.ValueString(),
		},
	}
	g, err := r.client.IAM().Groups().Create(ctx, cr)
	if err != nil {
		resp.Diagnostics.Append(protoErrorToDiagnostic(err, fmt.Sprintf("failed to create group %q", cr.Group.Name)))
		return
	}

	// Save group details in the state.
	plan.ID = types.StringValue(g.Id)
	diags := resp.State.Set(ctx, &plan)
	resp.Diagnostics.Append(diags...)
}

// Read refreshes the Terraform state with the latest data.
func (r *groupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	tflog.Info(ctx, "read group request", map[string]interface{}{"request": req})

	// Read the current state into the resource model.
	var state groupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Query for the group to update state
	uf := &common.UIDPFilter{}
	if state.ParentID.ValueString() != "" {
		uf.ChildrenOf = state.ParentID.ValueString()
	}
	f := &iam.GroupFilter{
		Id:   state.ID.ValueString(),
		Name: state.Name.ValueString(),
		Uidp: uf,
	}
	groupList, err := r.client.IAM().Groups().List(ctx, f)
	if err != nil {
		resp.Diagnostics.Append(protoErrorToDiagnostic(err, "failed to list groups"))
		return
	}

	switch c := len(groupList.GetItems()); {
	case c == 0:
		// Group was already deleted outside TF, remove from state
		state = groupResourceModel{}
		resp.State.RemoveResource(ctx)
	case c == 1:
		g := groupList.GetItems()[0]
		state.ID = types.StringValue(g.Id)
		state.Name = types.StringValue(g.Name)
		state.Description = types.StringValue(g.Description)
		state.ParentID = types.StringValue(uidp.Parent(g.Id))
	default:
		tflog.Error(ctx, fmt.Sprintf("group list returned %d groups for filter %v", c, f))
		resp.Diagnostics.AddError("more than one group found matching filters", fmt.Sprintf("filters=%v\nPlease provide more context to narrow query (e.g. parent_id).", state))
		return
	}

	// Set state
	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *groupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	tflog.Info(ctx, "update group request", map[string]interface{}{"request": req})

	// Read the plan into the resource model.
	var data groupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	g, err := r.client.IAM().Groups().Update(ctx, &iam.Group{
		Id:          data.ID.ValueString(),
		Name:        data.Name.ValueString(),
		Description: data.Description.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.Append(protoErrorToDiagnostic(err, fmt.Sprintf("failed to update group %q", data.ID.ValueString())))
		return
	}

	data.ID = types.StringValue(g.Id)
	data.Name = types.StringValue(g.GetName())
	data.Description = types.StringValue(g.GetDescription())
	diags := resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *groupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	tflog.Info(ctx, "delete group request", map[string]interface{}{"request": req})

	// Read the current state into the resource model.
	var state groupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Info(ctx, fmt.Sprintf("deleting group %q", id))
	_, err := r.client.IAM().Groups().Delete(ctx, &iam.DeleteGroupRequest{
		Id: id,
	})
	if err != nil {
		resp.Diagnostics.Append(protoErrorToDiagnostic(err, fmt.Sprintf("failed to delete group %q", id)))
		return
	}
	tflog.Info(ctx, fmt.Sprintf("group %q deleted", id))
}
