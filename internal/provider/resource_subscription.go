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

	events "chainguard.dev/sdk/proto/platform/events/v1"
	"chainguard.dev/sdk/uidp"
	"github.com/chainguard-dev/terraform-provider-chainguard/internal/validators"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &subscriptionResource{}
	_ resource.ResourceWithConfigure   = &subscriptionResource{}
	_ resource.ResourceWithImportState = &subscriptionResource{}
)

// NewSubscriptionResource is a helper function to simplify the provider implementation.
func NewSubscriptionResource() resource.Resource {
	return &subscriptionResource{}
}

// subscriptionResource is the resource implementation.
type subscriptionResource struct {
	managedResource
}

type subscriptionResourceModel struct {
	ID       types.String `tfsdk:"id"`
	ParentID types.String `tfsdk:"parent_id"`
	Sink     types.String `tfsdk:"sink"`
}

func (r *subscriptionResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.configure(ctx, req, resp)
}

// Metadata returns the resource type name.
func (r *subscriptionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_subscription"
}

// Schema defines the schema for the resource.
func (r *subscriptionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Event subscription.",
		// NB: There is no subscription update method so all attributes must
		// have a RequireReplace PlanModifier.
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "The id of the subscription.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"parent_id": schema.StringAttribute{
				Description:   "Parent IAM group of subscription. Sets the scope of the events subscribed to.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{validators.UIDP(false /* allowRootSentinel */)},
			},
			"sink": schema.StringAttribute{
				Description:   "Address to which events will be sent using the selected protocol",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{validators.IsURL(false /* requireHTTPS */)},
			},
		},
	}
}

// ImportState imports resources by ID into the current Terraform state.
func (r *subscriptionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// Create creates the resource and sets the initial Terraform state.
func (r *subscriptionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// Read the plan data into the resource model.
	var plan subscriptionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("create subscription request: parent_id=%s, sink=%s", plan.ParentID, plan.Sink))

	sub, err := r.prov.client.IAM().Subscriptions().Create(ctx, &events.CreateSubscriptionRequest{
		ParentId: plan.ParentID.ValueString(),
		Subscription: &events.Subscription{
			Sink: plan.Sink.ValueString(),
		},
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to create subscription"))
		return
	}

	// Save subscription details in the state.
	plan.ID = types.StringValue(sub.Id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state with the latest data.
func (r *subscriptionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Read the current state into the resource model.
	var state subscriptionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("read subscription request: %s", state.ID))

	subList, err := r.prov.client.IAM().Subscriptions().List(ctx, &events.SubscriptionFilter{
		Id: state.ID.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to list subscriptions"))
		return
	}

	switch c := len(subList.GetItems()); c {
	case 0:
		// Subscription was deleted outside TF, remove from state
		resp.State.RemoveResource(ctx)

	case 1:
		sub := subList.Items[0]
		state.ID = types.StringValue(sub.Id)
		state.Sink = types.StringValue(sub.Sink)
		state.ParentID = types.StringValue(uidp.Parent(sub.Id))

		// Set state
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)

	default:
		tflog.Error(ctx, fmt.Sprintf("subscriptions list returned %d subs for id %s", c, state.ID.ValueString()))
		resp.Diagnostics.AddError("failed to list subscriptions", fmt.Sprintf("more than one subscription found matching id %s", state.ID.ValueString()))
	}
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *subscriptionResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("update unsupported", "Updating a subscription is not supported.")
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *subscriptionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Read the current state into the resource model.
	var state subscriptionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("delete subscription request: %s", state.ID))

	id := state.ID.ValueString()
	_, err := r.prov.client.IAM().Subscriptions().Delete(ctx, &events.DeleteSubscriptionRequest{
		Id: id,
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, fmt.Sprintf("failed to delete subscription %q", id)))
	}
}
