/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"chainguard.dev/sdk/pkg/validation"
	iam "chainguard.dev/sdk/proto/platform/iam/v1"
	"github.com/chainguard-dev/terraform-provider-chainguard/internal/validators"
	"github.com/hashicorp/terraform-plugin-framework-validators/objectvalidator"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &accountAssociationsResource{}
	_ resource.ResourceWithConfigure   = &accountAssociationsResource{}
	_ resource.ResourceWithImportState = &accountAssociationsResource{}
)

// NewAccountAssociationsResource is a helper function to simplify the provider implementation.
func NewAccountAssociationsResource() resource.Resource {
	return &accountAssociationsResource{}
}

// accountAssociationsResource is the resource implementation.
type accountAssociationsResource struct {
	managedResource
}

type accountAssociationsResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Group       types.String `tfsdk:"group"`
	Amazon      types.Object `tfsdk:"amazon"`
	Google      types.Object `tfsdk:"google"`
	Chainguard  types.Object `tfsdk:"chainguard"`
}

type amazonAccountModel struct {
	Account types.String `tfsdk:"account"`
}

type chainguardAccountModel struct {
	ServiceBindings types.Map `tfsdk:"service_bindings"`
}

type googleAccountModel struct {
	ProjectID     types.String `tfsdk:"project_id"`
	ProjectNumber types.String `tfsdk:"project_number"`
}

func (r *accountAssociationsResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.configure(ctx, req, resp)
}

// Metadata returns the resource type name.
func (r *accountAssociationsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_account_associations"
}

// Schema defines the schema for the resource.
func (r *accountAssociationsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "IAM group invite on the Chainguard platform.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "The id of the account association.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Description: "Name of the account association.",
				Required:    true,
				Validators:  []validator.String{validators.Name()},
			},
			"description": schema.StringAttribute{
				Description: "Description of the account association.",
				Optional:    true,
			},
			"group": schema.StringAttribute{
				Description:   "The UIDP of the IAM group to associate to cloud accounts.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{validators.UIDP(false /* allowRootSentinel */)},
			},
		},
		Blocks: map[string]schema.Block{
			"amazon": schema.SingleNestedBlock{
				Description: "Amazon account configuration",
				Validators: []validator.Object{
					objectvalidator.AlsoRequires(
						path.Root("amazon").AtName("account").Expression(),
					),

					// At least one cloud provider block must be defined.
					// However, they are not mutually exclusive.
					objectvalidator.AtLeastOneOf(
						path.MatchRoot("amazon"),
						path.MatchRoot("google"),
						path.MatchRoot("chainguard"),
					),
				},
				Attributes: map[string]schema.Attribute{
					"account": schema.StringAttribute{
						Description: "AWS account ID",
						Optional:    true, // This attribute is required, but only if the block is defined. See Validators.
						Validators: []validator.String{
							validators.RunFuncs(validation.ValidateAWSAccount),
						},
					},
				},
			},
			"google": schema.SingleNestedBlock{
				Description: "Google Cloud Platform account association configuration",
				Validators: []validator.Object{
					objectvalidator.AlsoRequires(
						path.Root("google").AtName("project_id").Expression(),
						path.Root("google").AtName("project_number").Expression(),
					),
				},
				Attributes: map[string]schema.Attribute{
					"project_id": schema.StringAttribute{
						Description: "GCP project id",
						Optional:    true,
					},
					"project_number": schema.StringAttribute{
						Description: "GCP project number",
						Optional:    true,
					},
				},
			},
			"chainguard": schema.SingleNestedBlock{
				Description: "Association of Chainguard services to the service principals they should assume when talking to Chainguard APIs.",
				Validators: []validator.Object{
					objectvalidator.AlsoRequires(
						path.Root("chainguard").AtName("service_bindings").Expression(),
					),
				},
				Attributes: map[string]schema.Attribute{
					"service_bindings": schema.MapAttribute{
						Description: "A map of service bindings where the key is the service name and the value is the Id of the service principal identity.",
						ElementType: types.StringType,
						Optional:    true, // This attribute is required, but only if the block is defined. See block level Validators.
						Validators: []validator.Map{
							mapvalidator.ValueStringsAre(validators.UIDP(false /* allowRootSentinel */)),
						},
					},
				},
			},
		},
	}
}

// ImportState imports resources by ID into the current Terraform state.
func (r *accountAssociationsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func populateAccountAssociation(ctx context.Context, m accountAssociationsResourceModel) (*iam.AccountAssociations, diag.Diagnostics) {
	assoc := &iam.AccountAssociations{
		Name:        m.Name.ValueString(),
		Description: m.Description.ValueString(),
		Group:       m.Group.ValueString(),
	}

	var diags diag.Diagnostics
	if !m.Amazon.IsNull() {
		var am amazonAccountModel
		if diags = m.Amazon.As(ctx, &am, basetypes.ObjectAsOptions{}); diags.HasError() {
			return nil, diags
		}

		assoc.Amazon = &iam.AccountAssociations_Amazon{
			Account: am.Account.ValueString(),
		}
	}

	if !m.Chainguard.IsNull() {
		var cm chainguardAccountModel
		if diags = m.Chainguard.As(ctx, &cm, basetypes.ObjectAsOptions{}); diags.HasError() {
			return nil, diags
		}

		sb := make(map[string]string, len(cm.ServiceBindings.Elements()))
		if diags = cm.ServiceBindings.ElementsAs(ctx, &sb, false /* allowUnhandled */); diags.HasError() {
			return nil, diags
		}

		assoc.Chainguard = &iam.AccountAssociations_Chainguard{
			ServiceBindings: sb,
		}
	}

	if !m.Google.IsNull() {
		var gm googleAccountModel
		if diags = m.Google.As(ctx, &gm, basetypes.ObjectAsOptions{}); diags.HasError() {
			return nil, diags
		}

		assoc.Google = &iam.AccountAssociations_Google{
			ProjectId:     gm.ProjectID.ValueString(),
			ProjectNumber: gm.ProjectNumber.ValueString(),
		}
	}

	return assoc, nil
}

// Create creates the resource and sets the initial Terraform state.
func (r *accountAssociationsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// Read the plan data into the resource model.
	var plan accountAssociationsResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("create account association request: group=%s, amazon=%t, google=%t, chainguard=%t", plan.Group, !plan.Google.IsNull(), !plan.Amazon.IsNull(), !plan.Chainguard.IsNull()))

	assoc, diags := populateAccountAssociation(ctx, plan)
	if diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	created, err := r.prov.client.IAM().AccountAssociations().Create(ctx, assoc)
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to create account association"))
		return
	}

	// Save account association details in the state.
	// Account associations have no "id". They are one per group so we use the
	// group as id.
	plan.ID = types.StringValue(created.Group)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state with the latest data.
func (r *accountAssociationsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Read the current state into the resource model.
	var state accountAssociationsResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Account associations don't have a UIDP since only one is allowed per group.
	// The state ID == group UIDP for this account association.
	id := state.ID.ValueString()
	tflog.Info(ctx, fmt.Sprintf("read account association request for group: %s", id))

	assocList, err := r.prov.client.IAM().AccountAssociations().List(ctx, &iam.AccountAssociationsFilter{
		Group: id,
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to list account associations"))
		return
	}

	switch c := len(assocList.GetItems()); {
	case c == 0:
		// Account association was deleted outside TF, remove from state
		resp.State.RemoveResource(ctx)
		return
	case c > 1:
		resp.Diagnostics.AddError("failed to list account associations", fmt.Sprintf("more than one account association found matching group id %s", id))
		return
	}

	assoc := assocList.Items[0]

	// Only update the state model if there are differences returned from the API
	// to prevent Terraform reporting extraneous drift.
	if state.Name.ValueString() != assoc.Name {
		state.Name = types.StringValue(assoc.Name)
	}
	if state.Description.ValueString() != assoc.Description {
		state.Description = types.StringValue(assoc.Description)
	}
	if state.Group.ValueString() != assoc.Group {
		state.Group = types.StringValue(assoc.Group)
	}

	var diags diag.Diagnostics
	if assoc.Amazon != nil {
		var am amazonAccountModel
		update := true
		if !state.Amazon.IsNull() {
			if diags = state.Amazon.As(ctx, &am, basetypes.ObjectAsOptions{}); diags.HasError() {
				resp.Diagnostics.Append(diags...)
				return
			}
			update = am.Account.ValueString() != assoc.Amazon.Account
		}

		if update {
			am.Account = types.StringValue(assoc.Amazon.Account)
			state.Amazon, diags = types.ObjectValueFrom(ctx, state.Amazon.AttributeTypes(ctx), am)
			resp.Diagnostics.Append(diags...)
		}
	}

	if assoc.Chainguard != nil {
		var cm chainguardAccountModel
		update := true
		if !state.Chainguard.IsNull() {
			if diags = state.Chainguard.As(ctx, &cm, basetypes.ObjectAsOptions{}); diags.HasError() {
				resp.Diagnostics.Append(diags...)
				return
			}

			// Assume they're equal, until proven otherwise.
			update = false
			for k, sv := range cm.ServiceBindings.Elements() {
				if v, ok := assoc.Chainguard.ServiceBindings[k]; !ok || v != sv.String() {
					update = true
					break
				}
			}
		}

		if update {
			cm.ServiceBindings, diags = types.MapValueFrom(ctx, types.StringType, assoc.Chainguard.ServiceBindings)
			resp.Diagnostics.Append(diags...)

			state.Chainguard, diags = types.ObjectValueFrom(ctx, state.Chainguard.AttributeTypes(ctx), cm)
			resp.Diagnostics.Append(diags...)
		}
	}

	if assoc.Google != nil {
		var gm googleAccountModel
		update := true
		if !state.Google.IsNull() {
			if diags = state.Google.As(ctx, &gm, basetypes.ObjectAsOptions{}); diags.HasError() {
				resp.Diagnostics.Append(diags...)
				return
			}
			update = (gm.ProjectID.ValueString() != assoc.Google.ProjectId) || (gm.ProjectNumber.ValueString() != assoc.Google.ProjectNumber)
		}

		if update {
			gm.ProjectID = types.StringValue(assoc.Google.ProjectId)
			gm.ProjectNumber = types.StringValue(assoc.Google.ProjectNumber)
			state.Google, diags = types.ObjectValueFrom(ctx, state.Google.AttributeTypes(ctx), gm)
			resp.Diagnostics.Append(diags...)
		}
	}

	if resp.Diagnostics.HasError() {
		return
	}

	// Set state
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *accountAssociationsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Read the plan into the resource model.
	var data accountAssociationsResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("update account association request: group=%s, amazon=%t, google=%t, chainguard=%t", data.Group, !data.Google.IsNull(), !data.Amazon.IsNull(), !data.Chainguard.IsNull()))

	assoc, diags := populateAccountAssociation(ctx, data)
	if diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	_, err := r.prov.client.IAM().AccountAssociations().Update(ctx, assoc)
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to update account associations"))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *accountAssociationsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Read the current state into the resource model.
	var state accountAssociationsResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.Group.ValueString()
	tflog.Info(ctx, fmt.Sprintf("delete account associations request for group: %s", id))

	_, err := r.prov.client.IAM().AccountAssociations().Delete(ctx, &iam.DeleteAccountAssociationsRequest{
		Group: id,
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, fmt.Sprintf("failed to account associations for group %q", id)))
	}
}
