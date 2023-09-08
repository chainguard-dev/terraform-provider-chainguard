/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/objectvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"chainguard.dev/sdk/pkg/uidp"
	iam "chainguard.dev/sdk/proto/platform/iam/v1"
	"github.com/chainguard-dev/terraform-provider-chainguard/internal/validators"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &identityProviderResource{}
	_ resource.ResourceWithConfigure   = &identityProviderResource{}
	_ resource.ResourceWithImportState = &identityProviderResource{}
)

// NewIdentityProviderResource is a helper function to simplify the provider implementation.
func NewIdentityProviderResource() resource.Resource {
	return &identityProviderResource{}
}

// identityProviderResource is the resource implementation.
type identityProviderResource struct {
	managedResource
}

type identityProviderResourceModel struct {
	ID          types.String `tfsdk:"id"`
	ParentID    types.String `tfsdk:"parent_id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	OIDC        types.Object `tfsdk:"oidc"`
}

type oidcResourceModel struct {
	Issuer           types.String `tfsdk:"issuer"`
	ClientID         types.String `tfsdk:"client_id"`
	ClientSecret     types.String `tfsdk:"client_secret"`
	AdditionalScopes types.List   `tfsdk:"additional_scopes"`
}

func (r *identityProviderResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.configure(ctx, req, resp)
}

// Metadata returns the resource type name.
func (r *identityProviderResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_identity_provider"
}

// Schema defines the schema for the resource.
func (r *identityProviderResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "IAM Identity Provider.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "The id of the identity provider.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"parent_id": schema.StringAttribute{
				Description:   "The group containing this identity provider.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{validators.UIDP(false /* allowRootSentinel */)},
			},
			"name": schema.StringAttribute{
				Description: "The name of this identity provider.",
				Required:    true,
			},
			"description": schema.StringAttribute{
				Description: "A longer description of the purpose of this identity provider.",
				Optional:    true,
			},
		},
		Blocks: map[string]schema.Block{
			"oidc": schema.SingleNestedBlock{
				Description: "OIDC configuration of this identity provider",
				Attributes: map[string]schema.Attribute{
					"issuer": schema.StringAttribute{
						Description: "Issuer URL",
						Optional:    true, // This attribute is required, but only if the block is defined. See Validators.
						Validators: []validator.String{
							validators.IsURL(true /* requireHTTPS */),
						},
					},
					"client_id": schema.StringAttribute{
						Description: "Client ID for OIDC identity provider",
						Optional:    true, // This attribute is required, but only if the block is defined. See Validators.
					},
					"client_secret": schema.StringAttribute{
						Description: "Client secret for OIDC identity provider",
						Optional:    true, // This attribute is required, but only if the block is defined. See Validators.
						Sensitive:   true,
					},
					"additional_scopes": schema.ListAttribute{
						Description: "List of scopes to request",
						ElementType: types.StringType,
						Optional:    true, // This attribute is required, but only if the block is defined. See Validators.
					},
				},
				Validators: []validator.Object{
					objectvalidator.ExactlyOneOf(
						// Extend list with other protocols as they come
						path.MatchRoot("oidc"),
					),
					// This validator ensures that if this block is defined, both attributes are also defined.
					// `Required: true` couldn't be used on the attributes as this causes the undefined block to throw an error
					// about the missing "required" attribute.
					objectvalidator.AlsoRequires(
						path.Root("oidc").AtName("issuer").Expression(),
						path.Root("oidc").AtName("client_id").Expression(),
						path.Root("oidc").AtName("client_secret").Expression(),
						path.Root("oidc").AtName("additional_scopes").Expression(),
					),
				},
			},
		},
	}
}

// ImportState imports resources by ID into the current Terraform state.
func (r *identityProviderResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func populateIDP(ctx context.Context, model *identityProviderResourceModel) (*iam.IdentityProvider, error) {
	idp := &iam.IdentityProvider{
		Id:          model.ID.ValueString(),
		Name:        model.Name.ValueString(),
		Description: model.Description.ValueString(),
	}

	if !model.OIDC.IsNull() {
		var oidc oidcResourceModel
		if diags := model.OIDC.As(ctx, &oidc, basetypes.ObjectAsOptions{}); diags.HasError() {
			tflog.Error(ctx, fmt.Sprintf("failed to cast oidc model from state or plan: %s", diags[0].Detail()))
			return nil, errors.New("failed to cast oidc model from plan or state")
		}

		var scopes []string
		if diags := oidc.AdditionalScopes.ElementsAs(ctx, &scopes, false /* allowUnhandled */); diags.HasError() {
			tflog.Error(ctx, fmt.Sprintf("failed to cast additional_scopes from oidc model: %s", diags[0].Detail()))
			return nil, errors.New("failed to cast additional_scopes from oidc model")
		}

		idp.Configuration = &iam.IdentityProvider_Oidc{
			Oidc: &iam.IdentityProvider_OIDC{
				Issuer:           oidc.Issuer.ValueString(),
				ClientId:         oidc.ClientID.ValueString(),
				ClientSecret:     oidc.ClientSecret.ValueString(),
				AdditionalScopes: scopes,
			},
		}
	} else {
		// This shouldn't happen with our validation.
		return nil, errors.New("wanted at least oidc configuration to be set")
	}

	return idp, nil
}

// Create creates the resource and sets the initial Terraform state.
func (r *identityProviderResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// Read the plan data into the resource model.
	var plan identityProviderResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("create identity provider: parent_id=%s, name=%s", plan.ParentID, plan.Name))

	idp, err := populateIDP(ctx, &plan)
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to convert plan to IAM policy"))
		return
	}

	idp, err = r.prov.client.IAM().IdentityProviders().Create(ctx, &iam.CreateIdentityProviderRequest{
		ParentId:         plan.ParentID.ValueString(),
		IdentityProvider: idp,
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to create identity provider"))
		return
	}

	// Save identity provider ID in the state.
	plan.ID = types.StringValue(idp.Id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state with the latest data.
func (r *identityProviderResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Read the current state into the resource model.
	var state identityProviderResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("read identity provider request: %s", state.ID))

	id := state.ID.ValueString()
	idpList, err := r.prov.client.IAM().IdentityProviders().List(ctx, &iam.IdentityProviderFilter{
		Id: id,
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to list identity providers"))
		return
	}

	switch c := len(idpList.GetItems()); {
	case c == 0:
		// IdP was deleted outside TF, remove from state
		resp.State.RemoveResource(ctx)
		return
	case c > 1:
		tflog.Error(ctx, fmt.Sprintf("policy list returned %d policies for id %s", c, id))
		resp.Diagnostics.AddError("failed to list policy", fmt.Sprintf("more than one policy found matching id %s", id))
		return
	}

	idp := idpList.Items[0]
	state.ID = types.StringValue(idp.Id)
	state.Name = types.StringValue(idp.Name)
	state.Description = types.StringValue(idp.Description)
	state.ParentID = types.StringValue(uidp.Parent(idp.Id))

	switch conf := idp.Configuration.(type) {
	case *iam.IdentityProvider_Oidc:
		scopes, diags := types.ListValueFrom(ctx, types.StringType, conf.Oidc.AdditionalScopes)
		if diags.HasError() {
			resp.Diagnostics.Append(diags...)
			return
		}

		oidc := &oidcResourceModel{
			Issuer:           types.StringValue(conf.Oidc.Issuer),
			ClientID:         types.StringValue(conf.Oidc.ClientId),
			ClientSecret:     types.StringValue(conf.Oidc.ClientSecret),
			AdditionalScopes: scopes,
		}
		state.OIDC, diags = types.ObjectValueFrom(ctx, state.OIDC.AttributeTypes(ctx), oidc)
		resp.Diagnostics.Append(diags...)
	default:
		resp.Diagnostics.AddError("failed to save idp response in state", fmt.Sprintf("unknown configuration type: %T", conf))
	}

	if resp.Diagnostics.HasError() {
		return
	}

	// Set state.
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *identityProviderResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Read the plan into the resource model.
	var data identityProviderResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("update identity provider request: %s", data.ID))

	idp, err := populateIDP(ctx, &data)
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to convert plan to IAM policy"))
		return
	}

	if _, err := r.prov.client.IAM().IdentityProviders().Update(ctx, idp); err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to update identity provider"))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *identityProviderResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Read the current state into the resource model.
	var state identityProviderResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("delete identity provider request: %s", state.ID))

	id := state.ID.ValueString()
	_, err := r.prov.client.IAM().IdentityProviders().Delete(ctx, &iam.DeleteIdentityProviderRequest{
		Id: id,
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, fmt.Sprintf("failed to delete identity provider %q", id)))
	}
}
