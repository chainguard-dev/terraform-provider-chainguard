/*
Copyright 2026 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	regv2 "chainguard.dev/sdk/proto/chainguard/platform/registry/v2beta1"
	"chainguard.dev/sdk/uidp"
	"github.com/chainguard-dev/terraform-provider-chainguard/internal/validators"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                   = &imageOverlayBindingResource{}
	_ resource.ResourceWithConfigure      = &imageOverlayBindingResource{}
	_ resource.ResourceWithImportState    = &imageOverlayBindingResource{}
	_ resource.ResourceWithValidateConfig = &imageOverlayBindingResource{}
)

// NewImageOverlayBindingResource is a helper function to simplify the provider implementation.
func NewImageOverlayBindingResource() resource.Resource {
	return &imageOverlayBindingResource{}
}

// imageOverlayBindingResource is the resource implementation.
type imageOverlayBindingResource struct {
	managedResource
}

type imageOverlayBindingResourceModel struct {
	ID          types.String `tfsdk:"id"`
	RepoID      types.String `tfsdk:"repo_id"`
	OverlayID   types.String `tfsdk:"overlay_id"`
	TagSelector types.Object `tfsdk:"tag_selector"`
}

type tagSelectorModel struct {
	Kind        types.String `tfsdk:"kind"`
	Tags        types.List   `tfsdk:"tags"`
	VariantType types.String `tfsdk:"variant_type"`
}

func (r *imageOverlayBindingResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.configure(ctx, req, resp)
}

// Metadata returns the resource type name.
func (r *imageOverlayBindingResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_image_overlay_binding"
}

// Schema defines the schema for the resource.
func (r *imageOverlayBindingResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Binds a chainguard_image_overlay to an image repo under a tag selector.",
		// NB: There is no binding update method so all attributes must
		// have a RequiresReplace PlanModifier. The API workflow is
		// delete-and-recreate.
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "The UIDP of this overlay binding.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"repo_id": schema.StringAttribute{
				Description:   "The UIDP of the repo the overlay is bound to.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{
					validators.UIDP(false /* allowRootSentinel */),
				},
			},
			"overlay_id": schema.StringAttribute{
				Description:   "The UIDP of the overlay to bind.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{
					validators.UIDP(false /* allowRootSentinel */),
				},
			},
		},
		Blocks: map[string]schema.Block{
			"tag_selector": schema.SingleNestedBlock{
				Description: "Selects which tags on the repo the overlay applies to. " +
					"When multiple bindings match a tag, they layer in fixed precedence: ALL, then VARIANT, then EXACT.",
				PlanModifiers: []planmodifier.Object{objectplanmodifier.RequiresReplace()},
				Attributes: map[string]schema.Attribute{
					"kind": schema.StringAttribute{
						Description: "The matching mode: EXACT (tags listed in `tags`), ALL (every tag; at most one per repo), " +
							"or VARIANT (tags of the variant named by `variant_type`; at most one per repo and variant).",
						Required: true,
						Validators: []validator.String{
							stringvalidator.OneOf("EXACT", "ALL", "VARIANT"),
						},
					},
					"tags": schema.ListAttribute{
						Description: "Exact tag names to match. Required for kind=EXACT; must be unset otherwise.",
						Optional:    true,
						ElementType: types.StringType,
					},
					"variant_type": schema.StringAttribute{
						Description: "The variant to match. Required for kind=VARIANT; must be unset otherwise. " +
							"DEV matches tags ending in \"-dev\".",
						Optional: true,
						Validators: []validator.String{
							stringvalidator.OneOf("DEV"),
						},
					},
				},
			},
		},
	}
}

// ValidateConfig enforces the per-kind tag selector rules at plan time so
// misconfigurations fail before hitting the API. The server enforces the
// same rules authoritatively.
func (r *imageOverlayBindingResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config imageOverlayBindingResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if config.TagSelector.IsNull() || config.TagSelector.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("tag_selector"),
			"missing tag selector", "A tag_selector block is required.")
		return
	}

	var sel tagSelectorModel
	resp.Diagnostics.Append(config.TagSelector.As(ctx, &sel, basetypes.ObjectAsOptions{})...)
	if resp.Diagnostics.HasError() {
		return
	}
	if sel.Kind.IsUnknown() || sel.Tags.IsUnknown() || sel.VariantType.IsUnknown() {
		// Deferred values are validated once known (at apply).
		return
	}

	hasTags := !sel.Tags.IsNull() && len(sel.Tags.Elements()) > 0
	hasVariant := !sel.VariantType.IsNull()

	switch sel.Kind.ValueString() {
	case "EXACT":
		if !hasTags {
			resp.Diagnostics.AddAttributeError(path.Root("tag_selector").AtName("tags"),
				"invalid tag selector", "kind=EXACT requires at least one entry in tags.")
		}
		if hasVariant {
			resp.Diagnostics.AddAttributeError(path.Root("tag_selector").AtName("variant_type"),
				"invalid tag selector", "variant_type must be unset when kind=EXACT.")
		}
	case "ALL":
		if hasTags {
			resp.Diagnostics.AddAttributeError(path.Root("tag_selector").AtName("tags"),
				"invalid tag selector", "tags must be unset when kind=ALL.")
		}
		if hasVariant {
			resp.Diagnostics.AddAttributeError(path.Root("tag_selector").AtName("variant_type"),
				"invalid tag selector", "variant_type must be unset when kind=ALL.")
		}
	case "VARIANT":
		if !hasVariant {
			resp.Diagnostics.AddAttributeError(path.Root("tag_selector").AtName("variant_type"),
				"invalid tag selector", "kind=VARIANT requires variant_type.")
		}
		if hasTags {
			resp.Diagnostics.AddAttributeError(path.Root("tag_selector").AtName("tags"),
				"invalid tag selector", "tags must be unset when kind=VARIANT.")
		}
	}
}

// ImportState imports resources by ID into the current Terraform state.
func (r *imageOverlayBindingResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// Create creates the resource and sets the initial Terraform state.
func (r *imageOverlayBindingResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// Read the plan data into the resource model.
	var plan imageOverlayBindingResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("create image overlay binding request: repo_id=%s, overlay_id=%s", plan.RepoID, plan.OverlayID))

	sel, diags := tagSelectorProto(ctx, plan.TagSelector)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Retry on PermissionDenied to handle eventual consistency when the
	// parent repo was just created in the same apply.
	binding, err := retryOnPermissionDenied(ctx, func() (*regv2.OverlayBinding, error) {
		return r.prov.clientV2.Registry().OverlayBindingsService().CreateOverlayBinding(ctx, &regv2.CreateOverlayBindingRequest{
			Parent:      plan.RepoID.ValueString(),
			Overlay:     plan.OverlayID.ValueString(),
			TagSelector: sel,
		})
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to create image overlay binding"))
		return
	}

	// Save binding details in the state.
	plan.ID = types.StringValue(binding.GetUid())
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state with the latest data.
func (r *imageOverlayBindingResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Read the current state into the resource model.
	var state imageOverlayBindingResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("read image overlay binding request: %s", state.ID))

	binding, err := r.prov.clientV2.Registry().OverlayBindingsService().GetOverlayBinding(ctx, &regv2.GetOverlayBindingRequest{
		Uid: state.ID.ValueString(),
	})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			// Binding was deleted outside TF, remove from state.
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to get image overlay binding"))
		return
	}

	state.ID = types.StringValue(binding.GetUid())
	// The binding's UID is a UIDP under the repo it attaches to.
	repoID := binding.GetRepo()
	if repoID == "" {
		repoID = uidp.Parent(binding.GetUid())
	}
	state.RepoID = types.StringValue(repoID)
	state.OverlayID = types.StringValue(binding.GetOverlay().GetUid())

	sel, diags := tagSelectorObject(ctx, binding.GetTagSelector())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.TagSelector = sel

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *imageOverlayBindingResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	// All attributes require replacement, so this is never invoked.
	resp.Diagnostics.AddError("update unsupported", "Updating an image overlay binding is not supported; bindings are replaced.")
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *imageOverlayBindingResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Read the current state into the resource model.
	var state imageOverlayBindingResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("delete image overlay binding request: %s", state.ID))

	id := state.ID.ValueString()
	if _, err := r.prov.clientV2.Registry().OverlayBindingsService().DeleteOverlayBinding(ctx, &regv2.DeleteOverlayBindingRequest{
		Uid: id,
	}); err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, fmt.Sprintf("failed to delete image overlay binding %q", id)))
	}
}

// tagSelectorProto converts the tag_selector object into its proto form.
func tagSelectorProto(ctx context.Context, obj types.Object) (*regv2.TagSelector, diag.Diagnostics) {
	var sel tagSelectorModel
	diags := obj.As(ctx, &sel, basetypes.ObjectAsOptions{})
	if diags.HasError() {
		return nil, diags
	}

	out := &regv2.TagSelector{
		Kind: regv2.TagSelector_Kind(regv2.TagSelector_Kind_value["KIND_"+sel.Kind.ValueString()]),
	}
	if !sel.Tags.IsNull() {
		tags := make([]string, 0, len(sel.Tags.Elements()))
		diags.Append(sel.Tags.ElementsAs(ctx, &tags, false /* allowUnhandled */)...)
		if diags.HasError() {
			return nil, diags
		}
		out.Tags = tags
	}
	if !sel.VariantType.IsNull() {
		out.VariantType = regv2.TagSelector_VariantType(regv2.TagSelector_VariantType_value["VARIANT_TYPE_"+sel.VariantType.ValueString()])
	}
	return out, diags
}

// tagSelectorObject converts a proto TagSelector into the tag_selector
// object, using null for the payload fields the kind leaves unset so
// refresh doesn't report drift against configs that omit them.
func tagSelectorObject(ctx context.Context, sel *regv2.TagSelector) (types.Object, diag.Diagnostics) {
	m := tagSelectorModel{
		Kind:        types.StringValue(strings.TrimPrefix(sel.GetKind().String(), "KIND_")),
		Tags:        types.ListNull(types.StringType),
		VariantType: types.StringNull(),
	}
	if len(sel.GetTags()) > 0 {
		tags, diags := types.ListValueFrom(ctx, types.StringType, sel.GetTags())
		if diags.HasError() {
			return types.ObjectNull(tagSelectorAttrTypes()), diags
		}
		m.Tags = tags
	}
	if sel.GetVariantType() != regv2.TagSelector_VARIANT_TYPE_UNSPECIFIED {
		m.VariantType = types.StringValue(strings.TrimPrefix(sel.GetVariantType().String(), "VARIANT_TYPE_"))
	}
	return types.ObjectValueFrom(ctx, tagSelectorAttrTypes(), m)
}

func tagSelectorAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"kind":         types.StringType,
		"tags":         types.ListType{ElemType: types.StringType},
		"variant_type": types.StringType,
	}
}
