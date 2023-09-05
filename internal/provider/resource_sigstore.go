/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/objectvalidator"
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

	"chainguard.dev/sdk/pkg/uidp"
	iam "chainguard.dev/sdk/proto/platform/iam/v1"
	"github.com/chainguard-dev/terraform-provider-chainguard/internal/validators"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &sigstoreResource{}
	_ resource.ResourceWithConfigure   = &sigstoreResource{}
	_ resource.ResourceWithImportState = &sigstoreResource{}
)

// NewSigstoreResource is a helper function to simplify the provider implementation.
func NewSigstoreResource() resource.Resource {
	return &sigstoreResource{}
}

// sigstoreResource is the resource implementation.
type sigstoreResource struct {
	managedResource
}

type sigstoreResourceModel struct {
	ID          types.String `tfsdk:"id"`
	ParentID    types.String `tfsdk:"parent_id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	KMSCA       types.Object `tfsdk:"kms_ca"`
	GoogleCA    types.Object `tfsdk:"google_ca"`
	Hostname    types.String `tfsdk:"hostname"`
}

type kmsCAModel struct {
	KeyRef    types.String `tfsdk:"key_ref"`
	CertChain types.String `tfsdk:"cert_chain"`
}

type googleCAModel struct {
	Ref types.String `tfsdk:"ref"`
}

func (r *sigstoreResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.configure(ctx, req, resp)
}

// Metadata returns the resource type name.
func (r *sigstoreResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sigstore"
}

// Schema defines the schema for the resource.
func (r *sigstoreResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Sigstore instance resource.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "The id of the sigstore instance.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"parent_id": schema.StringAttribute{
				Description:   "Parent IAM group of Sigstore instance.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{validators.UIDP(false /* allowRootSentinel */)},
			},
			"name": schema.StringAttribute{
				Description: "Name of Sigstore instance",
				Required:    true,
			},
			"description": schema.StringAttribute{
				Description: "Description of Sigstore instance",
				Optional:    true,
			},
			"hostname": schema.StringAttribute{
				Description: "Unique hostname of Sigstore instance",
				Computed:    true,
			},
		},
		Blocks: map[string]schema.Block{
			"kms_ca": schema.SingleNestedBlock{
				Description: "KMS backed Certificate Authority",
				Attributes: map[string]schema.Attribute{
					"key_ref": schema.StringAttribute{
						Description:   "reference to the signing key used for this CA most likely a KMS key prefixed with gcpkms://, awskms://, azurekms:// etc and the relevant resource name",
						Optional:      true, // This attribute is required, but only if the block is defined. See Validators.
						PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
					},
					"cert_chain": schema.StringAttribute{
						Description:   "root certificate and (optional) chain in PEM-encoded format",
						Optional:      true, // This attribute is required, but only if the block is defined. See Validators.
						PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
					},
				},
				Validators: []validator.Object{
					objectvalidator.ExactlyOneOf(
						path.MatchRoot("kms_ca"),
						path.MatchRoot("google_ca"),
					),
					// This validator ensures that if this block is defined, both attributes are also defined.
					// `Required: true` couldn't be used on the attributes as this causes the undefined block to throw an error
					// about the missing "required" attribute.
					objectvalidator.AlsoRequires(
						path.Root("kms_ca").AtName("key_ref").Expression(),
						path.Root("kms_ca").AtName("cert_chain").Expression(),
					),
				},
			},
			"google_ca": schema.SingleNestedBlock{
				Description: "Google Cloud Private CA backed Certificate Authority",
				Attributes: map[string]schema.Attribute{
					"ref": schema.StringAttribute{
						Description:   "reference to the Google CA service in the format projects/<project>/locations/<location>/<name>",
						Optional:      true, // This attribute is required, but only if the block is defined. See Validators.
						PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
					},
				},
				Validators: []validator.Object{
					objectvalidator.ExactlyOneOf(
						path.MatchRoot("kms_ca"),
						path.MatchRoot("google_ca"),
					),
					// See comment above in "kms_ca" block.
					objectvalidator.AlsoRequires(
						path.Root("google_ca").AtName("ref").Expression(),
					),
				},
			},
		},
	}
}

// ImportState imports resources by ID into the current Terraform state.
func (r *sigstoreResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// Create creates the resource and sets the initial Terraform state.
func (r *sigstoreResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// Read the plan data into the resource model.
	var plan sigstoreResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("create sigstore request: parent_id=%s, name=%s", plan.ParentID, plan.Name))

	var ca iam.CertificateAuthority
	if !plan.KMSCA.IsNull() {
		var kms kmsCAModel
		resp.Diagnostics.Append(plan.KMSCA.As(ctx, &kms, basetypes.ObjectAsOptions{})...)
		if resp.Diagnostics.HasError() {
			return
		}

		ca.Ca = &iam.CertificateAuthority_KmsCa{
			KmsCa: &iam.KMSCA{
				KeyRef:    kms.KeyRef.ValueString(),
				CertChain: kms.CertChain.ValueString(),
			},
		}
	} else if !plan.GoogleCA.IsNull() {
		var google googleCAModel
		resp.Diagnostics.Append(plan.GoogleCA.As(ctx, &google, basetypes.ObjectAsOptions{})...)
		if resp.Diagnostics.HasError() {
			return
		}

		ca.Ca = &iam.CertificateAuthority_GoogleCa{
			GoogleCa: &iam.GoogleCA{
				Ref: google.Ref.ValueString(),
			},
		}
	}

	sig, err := r.prov.client.IAM().Sigstore().Create(ctx, &iam.CreateSigstoreRequest{
		ParentId: plan.ParentID.ValueString(),
		Sigstore: &iam.Sigstore{
			Name:                 plan.Name.ValueString(),
			Description:          plan.Description.ValueString(),
			CertificateAuthority: &ca,
		},
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to create sigstore"))
		return
	}

	// Save sigstore instance details in the state.
	plan.ID = types.StringValue(sig.Id)
	plan.Hostname = types.StringValue(sig.Hostname)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state with the latest data.
func (r *sigstoreResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Read the current state into the resource model.
	var state sigstoreResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("read sigstore instance request: %s", state.ID))

	id := state.ID.ValueString()
	sigList, err := r.prov.client.IAM().Sigstore().List(ctx, &iam.SigstoreFilter{
		Id: id,
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to list sigstore instances"))
		return
	}

	switch c := len(sigList.GetItems()); {
	case c == 0:
		// Sigstore instance was deleted outside TF, remove from state
		resp.State.RemoveResource(ctx)

	case c == 1:
		sig := sigList.Items[0]
		state.ID = types.StringValue(sig.Id)
		state.Name = types.StringValue(sig.Name)
		state.Description = types.StringValue(sig.Description)
		state.ParentID = types.StringValue(uidp.Parent(sig.Id))
		state.Hostname = types.StringValue(sig.Hostname)

		var diags diag.Diagnostics
		switch ca := sig.CertificateAuthority.Ca.(type) {
		case *iam.CertificateAuthority_KmsCa:
			kms := &kmsCAModel{
				KeyRef:    types.StringValue(ca.KmsCa.KeyRef),
				CertChain: types.StringValue(ca.KmsCa.CertChain),
			}
			state.KMSCA, diags = types.ObjectValueFrom(ctx, state.KMSCA.AttributeTypes(ctx), kms)

		case *iam.CertificateAuthority_GoogleCa:
			google := &googleCAModel{
				Ref: types.StringValue(ca.GoogleCa.Ref),
			}
			state.GoogleCA, diags = types.ObjectValueFrom(ctx, state.GoogleCA.AttributeTypes(ctx), google)

		default:
			diags.AddError("failed to parse api response", fmt.Sprintf("unknown CA type %T", ca))
		}

		resp.Diagnostics.Append(diags...)

		// Set state if no errors were encountered.
		if !resp.Diagnostics.HasError() {
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		}

	default:
		tflog.Error(ctx, fmt.Sprintf("sigstore instance list returned %d instances for id %s", c, id))
		resp.Diagnostics.AddError("failed to list sigstore instances", fmt.Sprintf("more than one sigstore instance found matching id %s", id))
	}
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *sigstoreResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Read the plan into the resource model.
	var data sigstoreResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("update sigstore instance request: %s", data.ID))

	sig, err := r.prov.client.IAM().Sigstore().Update(ctx, &iam.Sigstore{
		Id:          data.ID.ValueString(),
		Name:        data.Name.ValueString(),
		Description: data.Description.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to update sigstore instance"))
		return
	}

	// Update the ID and hostname, since they are not populated in the plan.
	data.ID = types.StringValue(sig.Id)
	data.Hostname = types.StringValue(sig.Hostname)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *sigstoreResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Read the current state into the resource model.
	var state sigstoreResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("delete sigstore instance request: %s", state.ID))

	id := state.ID.ValueString()
	_, err := r.prov.client.IAM().Sigstore().Delete(ctx, &iam.DeleteSigstoreRequest{
		Id: id,
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, fmt.Sprintf("failed to delete sigstore instance %q", id)))
	}
}
