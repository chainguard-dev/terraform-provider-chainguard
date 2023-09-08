/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"fmt"

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
	registry "chainguard.dev/sdk/proto/platform/registry/v1"
	"github.com/chainguard-dev/terraform-provider-chainguard/internal/validators"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &imageTagResource{}
	_ resource.ResourceWithConfigure   = &imageTagResource{}
	_ resource.ResourceWithImportState = &imageTagResource{}
)

// NewImageTagResource is a helper function to simplify the provider implementation.
func NewImageTagResource() resource.Resource {
	return &imageTagResource{}
}

// imageTagResource is the resource implementation.
type imageTagResource struct {
	managedResource
}

type imageTagResourceModel struct {
	ID      types.String `tfsdk:"id"`
	Name    types.String `tfsdk:"name"`
	RepoID  types.String `tfsdk:"repo_id"`
	Bundles types.List   `tfsdk:"bundles"`
}

func (r *imageTagResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.configure(ctx, req, resp)
}

// Metadata returns the resource type name.
func (r *imageTagResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_image_tag"
}

// Schema defines the schema for the resource.
func (r *imageTagResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Image tag (note: delete is purposefully a no-op).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "The UIDP of this tag.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Description: "The name of this tag.",
				Required:    true,
			},
			"repo_id": schema.StringAttribute{
				Description:   "The repo that owns the repo.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{
					validators.UIDP(false /* allowRootSentinel */),
				},
			},
			"bundles": schema.ListAttribute{
				Description: "List of bundles associated with this repo (a-z freeform keywords for sales purposes).",
				Optional:    true,
				ElementType: types.StringType,
				Validators: []validator.List{
					listvalidator.ValueStringsAre(validators.ValidateStringFuncs(validBundlesValue)),
				},
			},
		},
	}
}

// ImportState imports resources by ID into the current Terraform state.
func (r *imageTagResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// Create creates the resource and sets the initial Terraform state.
func (r *imageTagResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// Read the plan data into the resource model.
	var plan imageTagResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("create image tag request: name=%s, repo_id=%s", plan.Name, plan.RepoID))

	bundles := make([]string, 0, len(plan.Bundles.Elements()))
	resp.Diagnostics.Append(plan.Bundles.ElementsAs(ctx, &bundles, false /* allowUnhandled */)...)
	if resp.Diagnostics.HasError() {
		return
	}
	repo, err := r.prov.client.Registry().Registry().CreateTag(ctx, &registry.CreateTagRequest{
		RepoId: plan.RepoID.ValueString(),
		Tag: &registry.Tag{
			Name:    plan.Name.ValueString(),
			Bundles: bundles,
		},
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to create image tag"))
		return
	}

	// Save tag details in the state.
	plan.ID = types.StringValue(repo.Id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state with the latest data.
func (r *imageTagResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Read the current state into the resource model.
	var state imageTagResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("read image tag request: %s", state.ID))

	// Query for the tag to update state
	id := state.ID.ValueString()
	tagList, err := r.prov.client.Registry().Registry().ListTags(ctx, &registry.TagFilter{
		Id: id,
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to list image tags"))
		return
	}

	switch c := len(tagList.GetItems()); {
	case c == 0:
		// Tag doesn't exist or was deleted outside TF
		resp.State.RemoveResource(ctx)
		return
	case c > 1:
		resp.Diagnostics.AddError("internal error", fmt.Sprintf("fatal data corruption: id %s matched more than one image tag", id))
		return
	}

	// Update the state with values returned from the API.
	tag := tagList.GetItems()[0]
	state.ID = types.StringValue(tag.Id)
	state.RepoID = types.StringValue(uidp.Parent(tag.Id))
	state.Name = types.StringValue(tag.Name)

	var diags diag.Diagnostics
	state.Bundles, diags = types.ListValueFrom(ctx, types.StringType, tag.Bundles)
	if diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	// Set state
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *imageTagResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Read the plan into the resource model.
	var data imageTagResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("update image tag request: %s", data.ID))

	bundles := make([]string, 0, len(data.Bundles.Elements()))
	resp.Diagnostics.Append(data.Bundles.ElementsAs(ctx, &bundles, false /* allowUnhandled */)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tag, err := r.prov.client.Registry().Registry().UpdateTag(ctx, &registry.Tag{
		Id:      data.ID.ValueString(),
		Name:    data.Name.ValueString(),
		Bundles: bundles,
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to update image tag"))
		return
	}

	// Update the state with values returned from the API.
	data.ID = types.StringValue(tag.Id)
	data.Name = types.StringValue(tag.Name)

	var diags diag.Diagnostics
	data.Bundles, diags = types.ListValueFrom(ctx, types.StringType, tag.Bundles)
	if diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete is purposefully a no-op so tags aren't accidentally deleted with terraform.
// Instead, delete them with normal OCI calls (e.g. "crane delete")
func (r *imageTagResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// When not running acceptance tests, add an error to resp so Terraform does not automatically remove this resource from state.
	// See https://developer.hashicorp.com/terraform/plugin/framework/resources/delete#caveats for details.
	if !r.prov.testing {
		resp.Diagnostics.AddError("not implemented", "Image tags cannot be deleted through Terraform. Use `crane delete` to manually delete.")
		return
	}

	// Read the current state into the resource model.
	var state imageTagResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("ACCEPTANCE TEST: delete image tag request: %s", state.ID))

	id := state.ID.ValueString()
	_, err := r.prov.client.Registry().Registry().DeleteTag(ctx, &registry.DeleteTagRequest{
		Id: id,
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, fmt.Sprintf("failed to delete image tag %q", id)))
		return
	}
}
