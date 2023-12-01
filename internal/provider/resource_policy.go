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

	"chainguard.dev/sdk/policy"
	iam "chainguard.dev/sdk/proto/platform/iam/v1"
	"chainguard.dev/sdk/uidp"
	"github.com/chainguard-dev/terraform-provider-chainguard/internal/validators"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &policyResource{}
	_ resource.ResourceWithConfigure   = &policyResource{}
	_ resource.ResourceWithImportState = &policyResource{}
)

// NewPolicyResource is a helper function to simplify the provider implementation.
func NewPolicyResource() resource.Resource {
	return &policyResource{}
}

// policyResource is the resource implementation.
type policyResource struct {
	managedResource
}

type policyResourceModel struct {
	ID          types.String `tfsdk:"id"`
	ParentID    types.String `tfsdk:"parent_id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Document    types.String `tfsdk:"document"`
}

func (r *policyResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.configure(ctx, req, resp)
}

// Metadata returns the resource type name.
func (r *policyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_policy"
}

// Schema defines the schema for the resource.
func (r *policyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Cluster image policy.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "The id of the policy.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"parent_id": schema.StringAttribute{
				Description:   "IAM group the policy will belong to.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{validators.UIDP(false /* allowRootSentinel */)},
			},
			"name": schema.StringAttribute{
				Description:   "Name of the policy",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"description": schema.StringAttribute{
				Description: "Description of the policy",
				Optional:    true,
			},
			"document": schema.StringAttribute{
				Description: "Body of cluster image policy.",
				Required:    true,
				Validators:  []validator.String{validators.ValidateStringFuncs(validPolicyDocument)},
			},
		},
	}
}

func validPolicyDocument(doc string) error {
	_, err := policy.Validate(context.Background(), doc)
	return err
}

// ImportState imports resources by ID into the current Terraform state.
func (r *policyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// Create creates the resource and sets the initial Terraform state.
func (r *policyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// Read the plan data into the resource model.
	var plan policyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("create policy request: parent_id=%s", plan.ParentID))

	pol, err := r.prov.client.IAM().Policies().Create(ctx, &iam.CreatePolicyRequest{
		ParentId: plan.ParentID.ValueString(),
		Policy: &iam.Policy{
			Description: plan.Description.ValueString(),
			Document:    plan.Document.ValueString(),
		},
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to create policy"))
		return
	}

	// Save policy details in the state.
	plan.ID = types.StringValue(pol.Id)
	plan.Name = types.StringValue(pol.Name)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state with the latest data.
func (r *policyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Read the current state into the resource model.
	var state policyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("read policy request: %s", state.ID))

	id := state.ID.ValueString()
	polList, err := r.prov.client.IAM().Policies().List(ctx, &iam.PolicyFilter{
		Id: id,
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to list policies"))
		return
	}

	switch c := len(polList.GetItems()); {
	case c == 0:
		// Policy was deleted outside TF, remove from state
		resp.State.RemoveResource(ctx)

	case c == 1:
		pol := polList.Items[0]
		state.ID = types.StringValue(pol.Id)
		state.Name = types.StringValue(pol.Name)
		if !(state.Description.IsNull() && pol.Description == "") {
			state.Description = types.StringValue(pol.Description)
		}
		state.ParentID = types.StringValue(uidp.Parent(pol.Id))
		state.Document = types.StringValue(pol.Document)

		// Set state.
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)

	default:
		tflog.Error(ctx, fmt.Sprintf("policy list returned %d policies for id %s", c, id))
		resp.Diagnostics.AddError("failed to list policy", fmt.Sprintf("more than one policy found matching id %s", id))
	}
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *policyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Read the plan into the resource model.
	var data policyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("update policy request: %s", data.ID))

	pol, err := r.prov.client.IAM().Policies().Update(ctx, &iam.Policy{
		Id:          data.ID.ValueString(),
		Description: data.Description.ValueString(),
		Document:    data.Document.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to update policy"))
		return
	}

	// Name since it may have changed in the document.
	data.Name = types.StringValue(pol.Name)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *policyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Read the current state into the resource model.
	var state policyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("delete policy request: %s", state.ID))

	id := state.ID.ValueString()
	_, err := r.prov.client.IAM().Policies().Delete(ctx, &iam.DeletePolicyRequest{
		Id: id,
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, fmt.Sprintf("failed to delete policy %q", id)))
	}
}
