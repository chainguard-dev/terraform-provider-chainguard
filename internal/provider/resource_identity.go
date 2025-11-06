/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/objectvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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
	"golang.org/x/exp/maps"
	"google.golang.org/protobuf/types/known/timestamppb"

	iam "chainguard.dev/sdk/proto/platform/iam/v1"
	"chainguard.dev/sdk/uidp"
	"chainguard.dev/sdk/validation"
	"github.com/chainguard-dev/terraform-provider-chainguard/internal/validators"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &identityResource{}
	_ resource.ResourceWithConfigure   = &identityResource{}
	_ resource.ResourceWithImportState = &identityResource{}
)

// NewIdentityResource is a helper function to simplify the provider implementation.
func NewIdentityResource() resource.Resource {
	return &identityResource{}
}

// identityResource is the resource implementation.
type identityResource struct {
	managedResource
}

type identityResourceModel struct {
	ID               types.String `tfsdk:"id"`
	ParentID         types.String `tfsdk:"parent_id"`
	Name             types.String `tfsdk:"name"`
	Description      types.String `tfsdk:"description"`
	AWSIdentity      types.Object `tfsdk:"aws_identity"`
	ClaimMatch       types.Object `tfsdk:"claim_match"`
	Static           types.Object `tfsdk:"static"`
	ServicePrincipal types.String `tfsdk:"service_principal"`
}

type awsIdentityModel struct {
	Account       types.String `tfsdk:"aws_account"`
	UserID        types.String `tfsdk:"aws_user_id"`
	UserIDPattern types.String `tfsdk:"aws_user_id_pattern"`
	ARN           types.String `tfsdk:"aws_arn"`
	ARNPattern    types.String `tfsdk:"aws_arn_pattern"`
}

type claimMatchModel struct {
	Issuer          types.String `tfsdk:"issuer"`
	IssuerPattern   types.String `tfsdk:"issuer_pattern"`
	Subject         types.String `tfsdk:"subject"`
	SubjectPattern  types.String `tfsdk:"subject_pattern"`
	Claims          types.Map    `tfsdk:"claims"`
	ClaimPatterns   types.Map    `tfsdk:"claim_patterns"`
	Audience        types.String `tfsdk:"audience"`
	AudiencePattern types.String `tfsdk:"audience_pattern"`
}

type staticModel struct {
	Issuer     types.String `tfsdk:"issuer"`
	Subject    types.String `tfsdk:"subject"`
	IssuerKeys types.String `tfsdk:"issuer_keys"`
	Expiration types.String `tfsdk:"expiration"`
}

func (r *identityResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.configure(ctx, req, resp)
}

// Metadata returns the resource type name.
func (r *identityResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_identity"
}

// Schema defines the schema for the resource.
func (r *identityResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	servicePrincipals := maps.Keys(iam.ServicePrincipal_value)

	resp.Schema = schema.Schema{
		Description: "IAM Identity in the Chainguard platform.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "The id of this identity.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"parent_id": schema.StringAttribute{
				Description:   "The id of the group containing this identity.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{validators.UIDP(false /* allowRootSentinel */)},
			},
			"name": schema.StringAttribute{
				Description: "The name of this identity.",
				Required:    true,
				Validators:  []validator.String{validators.Name()},
			},
			"description": schema.StringAttribute{
				Description: "A longer description of the purpose of this identity.",
				Optional:    true,
			},
			"service_principal": schema.StringAttribute{
				Description:   "An identity that may be assumed by a particular Chainguard service.",
				Optional:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{
					stringvalidator.OneOf(servicePrincipals...),
					// Only one relationship type may be defined.
					// This mutex relationship need only be configured once, even if this attribute is not
					// defined by the user.
					stringvalidator.ExactlyOneOf(
						path.MatchRoot("aws_identity"),
						path.MatchRoot("claim_match"),
						path.MatchRoot("static"),
						path.MatchRoot("service_principal"),
					),
				},
			},
		},
		Blocks: map[string]schema.Block{
			"aws_identity": schema.SingleNestedBlock{
				Description: "An identity that may be assumed by an AWS identity satisfying the following contains on its GetCallerIdentity values",
				Validators: []validator.Object{
					// This validator ensures that if this block is defined, aws_account is also defined.
					// `Required: true` couldn't be used on the attributes as this causes the undefined block to throw an error
					// about the missing "required" attribute.
					objectvalidator.AlsoRequires(
						path.Root("aws_identity").AtName("aws_account").Expression(),
					),
				},
				Attributes: map[string]schema.Attribute{
					"aws_account": schema.StringAttribute{
						Description: "AWS Account ID of AWS user",
						Optional:    true, // This attribute is required, but only if the block is defined. See Validators.
						Validators: []validator.String{
							validators.ValidateStringFuncs(validation.ValidateAWSAccount),
						},
					},
					"aws_user_id": schema.StringAttribute{
						Description: "The exact UserId that must appear in GetCallerIdentity to assume this identity.",
						Optional:    true,
						Validators: []validator.String{
							validators.IfParentDefined(
								stringvalidator.ExactlyOneOf(
									path.Root("aws_identity").AtName("aws_user_id").Expression(),
									path.Root("aws_identity").AtName("aws_user_id_pattern").Expression(),
								),
							),
						},
					},
					"aws_user_id_pattern": schema.StringAttribute{
						Description: "A pattern for matching acceptable UserID that must appear in GetCallerIdentity response to assume this identity.",
						Optional:    true,
						Validators: []validator.String{
							validators.ValidRegExp(),
						},
					},
					"aws_arn": schema.StringAttribute{
						Description: "The exact Arn that must appear in GetCallerIdentity to assume this identity.",
						Optional:    true,
						Validators: []validator.String{
							validators.IfParentDefined(
								stringvalidator.ExactlyOneOf(
									path.Root("aws_identity").AtName("aws_arn").Expression(),
									path.Root("aws_identity").AtName("aws_arn_pattern").Expression(),
								),
							),
						},
					},
					"aws_arn_pattern": schema.StringAttribute{
						Description: "A pattern for matching acceptable Arn that must appear in GetCallerIdentity response to assume this identity.",
						Optional:    true,
						Validators: []validator.String{
							validators.ValidRegExp(),
						},
					},
				},
			},
			"claim_match": schema.SingleNestedBlock{
				Description: "An identity that may be assumed when its claims satisfy these constraints.",
				Attributes: map[string]schema.Attribute{
					"issuer": schema.StringAttribute{
						Description: "The exact issuer that must appear in tokens to assume this identity.",
						Optional:    true,
						Validators: []validator.String{
							validators.IsURL(true /* requireHTTPS */),
							validators.IfParentDefined(
								stringvalidator.ExactlyOneOf(
									path.Root("claim_match").AtName("issuer").Expression(),
									path.Root("claim_match").AtName("issuer_pattern").Expression(),
								),
							),
						},
					},
					"issuer_pattern": schema.StringAttribute{
						Description: "A pattern for matching acceptable issuers that appear in tokens to assume this identity.",
						Optional:    true,
						Validators: []validator.String{
							validators.ValidRegExp(),
						},
					},
					"subject": schema.StringAttribute{
						Description: "The exact subject that must appear in tokens to assume this identity.",
						Optional:    true,
						Validators: []validator.String{
							validators.IfParentDefined(
								stringvalidator.ExactlyOneOf(
									path.Root("claim_match").AtName("subject").Expression(),
									path.Root("claim_match").AtName("subject_pattern").Expression(),
								),
							),
						},
					},
					"subject_pattern": schema.StringAttribute{
						Description: "A pattern for matching acceptable subjects that appear in tokens to assume this identity.",
						Optional:    true,
						Validators: []validator.String{
							validators.ValidRegExp(),
						},
					},
					// NB: claims and claim_patterns are neither required, nor mutually-exclusive.
					"claims": schema.MapAttribute{
						Description: "The exact custom claims that appear in tokens to assume this identity.",
						Optional:    true,
						ElementType: types.StringType,
					},
					"claim_patterns": schema.MapAttribute{
						Description: "The custom claim patterns for matching acceptable custom claims that appear in tokens to assume this identity.",
						Optional:    true,
						ElementType: types.StringType,
						Validators: []validator.Map{
							mapvalidator.ValueStringsAre(validators.ValidRegExp()),
						},
					},
					"audience": schema.StringAttribute{
						Description: "The exact audience that must appear in tokens to assume this identity.",
						Optional:    true,
						Validators: []validator.String{
							stringvalidator.ConflictsWith(path.Root("claim_match").AtName("audience_pattern").Expression()),
						},
					},
					"audience_pattern": schema.StringAttribute{
						Description: "A pattern for matching acceptable audiences that appear in tokens to assume this identity.",
						Optional:    true,
						Validators: []validator.String{
							validators.ValidRegExp(),
							stringvalidator.ConflictsWith(path.Root("claim_match").AtName("audience").Expression()),
						},
					},
				},
			},
			"static": schema.SingleNestedBlock{
				Description: "An identity that is verified by OIDC, with pre-registered verification keys.",
				// TODO: remove once bug in Identity.Update between static <-> claim_match is resolved
				PlanModifiers: []planmodifier.Object{objectplanmodifier.RequiresReplace()},
				Validators: []validator.Object{
					// This validator ensures that if this block is defined, all attributes are defined.
					// `Required: true` couldn't be used on the attributes as this causes the undefined block to throw an error
					// about the missing "required" attribute.
					objectvalidator.AlsoRequires(
						path.Root("static").AtName("issuer").Expression(),
						path.Root("static").AtName("subject").Expression(),
						path.Root("static").AtName("issuer_keys").Expression(),
						path.Root("static").AtName("expiration").Expression(),
					),
				},
				Attributes: map[string]schema.Attribute{
					"issuer": schema.StringAttribute{
						Description: "The exact issuer that must appear in tokens to assume this identity.",
						Optional:    true, // This attribute is required, but only if the block is defined. See Validators.
						Validators: []validator.String{
							validators.IsURL(true /* requireHTTPS */),
						},
					},
					"subject": schema.StringAttribute{
						Description: "The exact subject that must appear in tokens to assume this identity.",
						Optional:    true, // This attribute is required, but only if the block is defined. See Validators.
					},
					"issuer_keys": schema.StringAttribute{
						Description: "The JSON web key set (JWKS) of the OIDC issuer that should be used to verify tokens.",
						Optional:    true, // This attribute is required, but only if the block is defined. See Validators.
					},
					"expiration": schema.StringAttribute{
						Description: "The RFC3339 encoded date and time at which this identity will no longer be valid.",
						Optional:    true, // This attribute is required, but only if the block is defined. See Validators.
						Validators: []validator.String{
							validators.ValidateStringFuncs(checkRFC3339),
						},
					},
				},
			},
		},
	}
}

// For testing.
var timeNow = time.Now

// checkRFC3339 implements validators.ValidateStringFunc.
func checkRFC3339(raw string) error {
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return fmt.Errorf("failed to parse %s: %w", raw, err)
	}
	if t.Before(timeNow()) {
		return fmt.Errorf("expiration %q is in the past", raw)
	}
	return nil
}

func populateModel(ctx context.Context, model *identityResourceModel, id *iam.Identity) diag.Diagnostics {
	var allDiags diag.Diagnostics

	if model == nil {
		model = &identityResourceModel{}
	}

	awsTypes := model.AWSIdentity.AttributeTypes(ctx)
	claimMatchTypes := model.ClaimMatch.AttributeTypes(ctx)
	staticTypes := model.Static.AttributeTypes(ctx)

	model.ID = types.StringValue(id.Id)
	model.ParentID = types.StringValue(uidp.Parent(id.Id))
	model.Name = types.StringValue(id.Name)
	if model.Description.IsNull() && id.Description != "" {
		model.Description = types.StringValue(id.Description)
	}

	if lit, ok := id.Relationship.(*iam.Identity_ClaimMatch_); ok {
		var diags diag.Diagnostics

		// Get the current state
		state := &claimMatchModel{}
		allDiags.Append(model.ClaimMatch.As(ctx, &state, basetypes.ObjectAsOptions{})...)

		cm := &claimMatchModel{
			Claims:        types.MapNull(types.StringType),
			ClaimPatterns: types.MapNull(types.StringType),
		}

		// Preserve the current state of claims/claim_patterns
		// This allows users to have either empty maps, or omitted maps.
		//
		// For example, the following identities are equivalent:
		// resource chainguard_identity "test" {
		//   ...
		//   claims = {}
		// }
		//
		// resource chainguard_identity "test" {
		//   ...
		// }
		if state != nil && !state.Claims.IsNull() {
			cm.Claims = state.Claims
		}
		if state != nil && !state.ClaimPatterns.IsNull() {
			cm.ClaimPatterns = state.ClaimPatterns
		}

		// Populate claims and claim_patterns, if present.
		if len(lit.ClaimMatch.GetClaims()) > 0 {
			cm.Claims, diags = types.MapValueFrom(ctx, types.StringType, lit.ClaimMatch.GetClaims())
			allDiags.Append(diags...)
		}
		if len(lit.ClaimMatch.GetClaimPatterns()) > 0 {
			cm.ClaimPatterns, diags = types.MapValueFrom(ctx, types.StringType, lit.ClaimMatch.GetClaimPatterns())
			allDiags.Append(diags...)
		}

		switch lit.ClaimMatch.Iss.(type) {
		case *iam.Identity_ClaimMatch_Issuer:
			cm.Issuer = types.StringValue(lit.ClaimMatch.GetIssuer())
		case *iam.Identity_ClaimMatch_IssuerPattern:
			cm.IssuerPattern = types.StringValue(lit.ClaimMatch.GetIssuerPattern())
		default:
			allDiags.AddError("failed to assign issuer", fmt.Sprintf("unsupported issuer type: %T", lit.ClaimMatch.Iss))
		}

		switch lit.ClaimMatch.Sub.(type) {
		case *iam.Identity_ClaimMatch_Subject:
			cm.Subject = types.StringValue(lit.ClaimMatch.GetSubject())
		case *iam.Identity_ClaimMatch_SubjectPattern:
			cm.SubjectPattern = types.StringValue(lit.ClaimMatch.GetSubjectPattern())
		default:
			allDiags.AddError("failed to assign subject", fmt.Sprintf("unsupported subject type: %T", lit.ClaimMatch.Sub))
		}

		switch lit.ClaimMatch.Aud.(type) {
		case *iam.Identity_ClaimMatch_Audience:
			cm.Audience = types.StringValue(lit.ClaimMatch.GetAudience())
		case *iam.Identity_ClaimMatch_AudiencePattern:
			cm.AudiencePattern = types.StringValue(lit.ClaimMatch.GetAudiencePattern())
		default:
			// This isn't a required field.
		}

		model.ClaimMatch, diags = types.ObjectValueFrom(ctx, claimMatchTypes, cm)
		allDiags.Append(diags...)
	} else {
		model.ClaimMatch = types.ObjectNull(claimMatchTypes)
	}

	if aws, ok := id.Relationship.(*iam.Identity_AwsIdentity); ok {
		awsModel := &awsIdentityModel{
			Account: types.StringValue(aws.AwsIdentity.AwsAccount),
		}

		switch aws.AwsIdentity.AwsUserId.(type) {
		case *iam.Identity_AWSIdentity_UserId:
			awsModel.UserID = types.StringValue(aws.AwsIdentity.GetUserId())
		case *iam.Identity_AWSIdentity_UserIdPattern:
			awsModel.UserIDPattern = types.StringValue(aws.AwsIdentity.GetUserIdPattern())
		default:
			allDiags.AddError("failed to assign AWS user ID", fmt.Sprintf("unsupported user ID type: %T", aws.AwsIdentity.AwsUserId))
		}

		switch aws.AwsIdentity.AwsArn.(type) {
		case *iam.Identity_AWSIdentity_Arn:
			awsModel.ARN = types.StringValue(aws.AwsIdentity.GetArn())
		case *iam.Identity_AWSIdentity_ArnPattern:
			awsModel.ARNPattern = types.StringValue(aws.AwsIdentity.GetArnPattern())
		default:
			allDiags.AddError("failed to assign AWS ARN", fmt.Sprintf("unsupported ARN type: %T", aws.AwsIdentity.AwsArn))
		}

		var diags diag.Diagnostics
		model.AWSIdentity, diags = types.ObjectValueFrom(ctx, awsTypes, awsModel)
		allDiags.Append(diags...)
	} else {
		model.AWSIdentity = types.ObjectNull(awsTypes)
	}

	if st, ok := id.Relationship.(*iam.Identity_Static); ok {
		static := &staticModel{
			Issuer:     types.StringValue(st.Static.Issuer),
			Subject:    types.StringValue(st.Static.Subject),
			IssuerKeys: types.StringValue(st.Static.IssuerKeys),
			Expiration: types.StringValue(st.Static.Expiration.AsTime().Format(time.RFC3339)),
		}

		var diags diag.Diagnostics
		model.Static, diags = types.ObjectValueFrom(ctx, staticTypes, static)
		allDiags.Append(diags...)
	} else {
		model.Static = types.ObjectNull(staticTypes)
	}

	if sp, ok := id.Relationship.(*iam.Identity_ServicePrincipal); ok {
		v := iam.ServicePrincipal_name[int32(sp.ServicePrincipal)]
		model.ServicePrincipal = types.StringValue(v)
	} else {
		model.ServicePrincipal = types.StringNull()
	}

	return allDiags
}

func populateIdentity(ctx context.Context, m identityResourceModel) (*iam.Identity, error) {
	id := &iam.Identity{
		Id:          m.ID.ValueString(),
		Name:        m.Name.ValueString(),
		Description: m.Description.ValueString(),
	}

	if !m.ClaimMatch.IsNull() {
		var cmModel claimMatchModel
		if diags := m.ClaimMatch.As(ctx, &cmModel, basetypes.ObjectAsOptions{}); diags.HasError() {
			tflog.Error(ctx, "failed to cast claim match model from state or plan", map[string]interface{}{"model": m, "error": diags[0].Detail()})
			return nil, errors.New("failed to cast claim match model from plan")
		}

		// Claims or ClaimPatterns
		var claims map[string]string
		if !cmModel.Claims.IsNull() {
			claims = make(map[string]string, len(cmModel.Claims.Elements()))
			cmModel.Claims.ElementsAs(ctx, &claims, false /* allowUnhandled */)
		}

		var claimPatterns map[string]string
		if !cmModel.ClaimPatterns.IsNull() {
			claimPatterns = make(map[string]string, len(cmModel.ClaimPatterns.Elements()))
			cmModel.ClaimPatterns.ElementsAs(ctx, &claimPatterns, false /* allowUnhandled */)
		}

		cm := &iam.Identity_ClaimMatch{
			Claims:        claims,
			ClaimPatterns: claimPatterns,
		}

		// Issuer or IssuerPattern; only one is not null due to validators
		if !cmModel.Issuer.IsNull() {
			cm.Iss = &iam.Identity_ClaimMatch_Issuer{
				Issuer: cmModel.Issuer.ValueString(),
			}
		}
		if !cmModel.IssuerPattern.IsNull() {
			cm.Iss = &iam.Identity_ClaimMatch_IssuerPattern{
				IssuerPattern: cmModel.IssuerPattern.ValueString(),
			}
		}

		// Subject or SubjectPattern; only one is not null due to validators
		if !cmModel.Subject.IsNull() {
			cm.Sub = &iam.Identity_ClaimMatch_Subject{
				Subject: cmModel.Subject.ValueString(),
			}
		}
		if !cmModel.SubjectPattern.IsNull() {
			cm.Sub = &iam.Identity_ClaimMatch_SubjectPattern{
				SubjectPattern: cmModel.SubjectPattern.ValueString(),
			}
		}

		// Audience or AudiencePattern; at most one is not null due to validators (both may be null)
		if !cmModel.Audience.IsNull() {
			cm.Aud = &iam.Identity_ClaimMatch_Audience{
				Audience: cmModel.Audience.ValueString(),
			}
		}
		if !cmModel.AudiencePattern.IsNull() {
			cm.Aud = &iam.Identity_ClaimMatch_AudiencePattern{
				AudiencePattern: cmModel.AudiencePattern.ValueString(),
			}
		}

		id.Relationship = &iam.Identity_ClaimMatch_{
			ClaimMatch: cm,
		}
	} else if !m.AWSIdentity.IsNull() {
		var awsModel awsIdentityModel
		if diags := m.AWSIdentity.As(ctx, &awsModel, basetypes.ObjectAsOptions{}); diags.HasError() {
			tflog.Error(ctx, "failed to cast aws model from state or plan", map[string]interface{}{"model": m, "error": diags[0].Detail()})
			return nil, errors.New("failed to cast aws model from state or plan")
		}

		aws := &iam.Identity_AWSIdentity{
			AwsAccount: awsModel.Account.ValueString(),
		}

		// UserID or UserIDPattern; only one is not null due to validators
		if !awsModel.UserID.IsNull() {
			aws.AwsUserId = &iam.Identity_AWSIdentity_UserId{
				UserId: awsModel.UserID.ValueString(),
			}
		}
		if !awsModel.UserIDPattern.IsNull() {
			aws.AwsUserId = &iam.Identity_AWSIdentity_UserIdPattern{
				UserIdPattern: awsModel.UserIDPattern.ValueString(),
			}
		}

		// ARN or ARNPattern; only one is not null due to validators
		if !awsModel.ARN.IsNull() {
			aws.AwsArn = &iam.Identity_AWSIdentity_Arn{
				Arn: awsModel.ARN.ValueString(),
			}
		}
		if !awsModel.ARNPattern.IsNull() {
			aws.AwsArn = &iam.Identity_AWSIdentity_ArnPattern{
				ArnPattern: awsModel.ARNPattern.ValueString(),
			}
		}

		id.Relationship = &iam.Identity_AwsIdentity{
			AwsIdentity: aws,
		}
	} else if !m.Static.IsNull() {
		var exp *timestamppb.Timestamp
		var stModel staticModel
		if diags := m.Static.As(ctx, &stModel, basetypes.ObjectAsOptions{}); diags.HasError() {
			tflog.Error(ctx, "failed to cast static model from state or plan", map[string]interface{}{"model": m, "error": diags[0].Detail()})
			return nil, errors.New("failed to cast aws model from state or plan")
		}

		ts, err := time.Parse(time.RFC3339, stModel.Expiration.ValueString())
		if err != nil {
			// This shouldn't happen with our validation.
			return nil, err
		}
		exp = timestamppb.New(ts)

		id.Relationship = &iam.Identity_Static{
			Static: &iam.Identity_StaticKeys{
				Issuer:     stModel.Issuer.ValueString(),
				Subject:    stModel.Subject.ValueString(),
				IssuerKeys: stModel.IssuerKeys.ValueString(),
				Expiration: exp,
			},
		}
	} else if !m.ServicePrincipal.IsNull() {
		id.Relationship = &iam.Identity_ServicePrincipal{
			ServicePrincipal: iam.ServicePrincipal(iam.ServicePrincipal_value[m.ServicePrincipal.ValueString()]),
		}
	} else {
		// This shouldn't happen with our validation.
		return nil, errors.New("wanted one of aws_identity, claim_match, static, service_principal")
	}

	return id, nil
}

// ImportState imports resources by ID into the current Terraform state.
func (r *identityResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// Create creates the resource and sets the initial Terraform state.
func (r *identityResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// Read the plan data into the resource model.
	var plan identityResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("create identity request: name=%s, parent_id=%s", plan.Name, plan.ParentID))

	identity, err := populateIdentity(ctx, plan)
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to populate identity from plan"))
		return
	}

	// Create the identity.
	ident, err := r.prov.client.IAM().Identities().Create(ctx, &iam.CreateIdentityRequest{
		ParentId: plan.ParentID.ValueString(),
		Identity: identity,
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to create identity"))
		return
	}

	// If any errors were encountered, exit before updating the state.
	if resp.Diagnostics.Append(populateModel(ctx, &plan, ident)...); resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state with the latest data.
func (r *identityResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Read the current state into the resource model.
	var state identityResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("read identity request: %s", state.ID))

	// Query for the identity to update state
	identID := state.ID.ValueString()
	identityList, err := r.prov.client.IAM().Identities().List(ctx, &iam.IdentityFilter{
		Id: identID,
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to list identities"))
		return
	}

	switch c := len(identityList.GetItems()); {
	case c == 0:
		// Identity doesn't exist or was deleted outside TF
		resp.State.RemoveResource(ctx)
		return
	case c > 1:
		tflog.Error(ctx, fmt.Sprintf("identities list returned %d identities for id %q", c, identID))
		resp.Diagnostics.AddError("internal error", fmt.Sprintf("fatal data corruption: id %s matched more than one identity", identID))
		return
	}

	ident := identityList.Items[0]

	// If any errors were encountered, exit before updating the state.
	if resp.Diagnostics.Append(populateModel(ctx, &state, ident)...); resp.Diagnostics.HasError() {
		return
	}

	// Set state
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *identityResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Read the plan into the resource model.
	var plan identityResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("update identity request: %s", plan.ID))

	ident, err := populateIdentity(ctx, plan)
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to populate identity from plan"))
		return
	}

	updated, err := r.prov.client.IAM().Identities().Update(ctx, ident)
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, fmt.Sprintf("failed to update identity %q", plan.ID.ValueString())))
		return
	}

	resp.Diagnostics.Append(populateModel(ctx, &plan, updated)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Set state
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *identityResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Read the current state into the resource model.
	var state identityResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("delete identity request: %s", state.ID))

	id := state.ID.ValueString()
	_, err := r.prov.client.IAM().Identities().Delete(ctx, &iam.DeleteIdentityRequest{
		Id: id,
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, fmt.Sprintf("failed to delete identity %q", id)))
		return
	}
}
