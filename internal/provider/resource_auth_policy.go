/*
Copyright 2026 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	iamv2 "chainguard.dev/sdk/proto/chainguard/platform/iam/v2"
	"chainguard.dev/sdk/uidp"
	"github.com/chainguard-dev/terraform-provider-chainguard/internal/validators"
)

// The session length the API accepts: one hour to 90 days, in whole seconds.
const (
	sessionDurationMin = time.Hour
	sessionDurationMax = 90 * 24 * time.Hour
)

// authPolicySessionDurationPath is the update_mask path of session_duration.
// The API refuses a "*" mask on this resource, so every update names it.
const authPolicySessionDurationPath = "session_duration"

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &authPolicyResource{}
	_ resource.ResourceWithConfigure   = &authPolicyResource{}
	_ resource.ResourceWithImportState = &authPolicyResource{}
)

// NewAuthPolicyResource is a helper function to simplify the provider implementation.
func NewAuthPolicyResource() resource.Resource {
	return &authPolicyResource{}
}

// authPolicyResource is the resource implementation.
type authPolicyResource struct {
	managedResource
}

type authPolicyResourceModel struct {
	ID              types.String `tfsdk:"id"`
	ParentID        types.String `tfsdk:"parent_id"`
	SessionDuration types.String `tfsdk:"session_duration"`
}

func (r *authPolicyResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.configure(ctx, req, resp)
}

// Metadata returns the resource type name.
func (r *authPolicyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_auth_policy"
}

const authPolicyDescription = `The authentication policy of a root, verified organization: how long a sign-in to the organization lasts before its members must sign in again.

Every such organization has exactly one policy, so this resource never creates or deletes one. Creating the resource sets the session length; destroying it returns the organization to Chainguard's default length.

The length runs from login. At 30 days or less, a session lasts that long from login, and using it does not extend it. Above 30 days, a session lasts while it is used at least once every 30 days, up to the length. A session whose client does not rotate its refresh tokens lasts at most 20 hours; chainctl rotates them.

A member of several organizations gets the shortest length among them.

A shorter length reaches an existing session at its next token refresh only when a length over 20 hours has already extended that session past 20 hours from login. It does not cut any other existing session: that session keeps the expiry it was issued with, at most 20 hours from login.

The policy's sso_only control is not managed by this resource. SSO-only enforcement has not shipped yet.`

// Schema defines the schema for the resource.
func (r *authPolicyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: authPolicyDescription,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "The UIDP of the organization, the same as parent_id.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"parent_id": schema.StringAttribute{
				Description:   "The UIDP of the root, verified organization this policy belongs to.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{
					validators.UIDP(false /* allowRootSentinel */),
					validators.ValidateStringFuncs(validateRootUIDP),
				},
			},
			"session_duration": schema.StringAttribute{
				Description: "How long a sign-in to the organization lasts, from 1h to 90d, in whole seconds. " +
					"Either a Go duration such as `720h` or a whole number of days such as `30d`. " +
					"A value read back from the API is written in hours (`720h`), or in minutes or seconds when it is not a whole number of hours; " +
					"a configured value that names the same length, such as `30d` for `720h`, is kept as written.",
				Required:   true,
				Validators: []validator.String{validators.ValidateStringFuncs(validateSessionDuration)},
			},
		},
	}
}

// ImportState imports resources by ID into the current Terraform state.
func (r *authPolicyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("parent_id"), req.ID)...)
}

// Create creates the resource and sets the initial Terraform state.
func (r *authPolicyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan authPolicyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("create auth policy: parent_id=%s, session_duration=%s", plan.ParentID, plan.SessionDuration))

	d, err := parseSessionDuration(plan.SessionDuration.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("invalid session_duration", err.Error())
		return
	}

	// Retry on PermissionDenied to handle eventual consistency when the
	// organization was just created in the same apply.
	if _, err := retryOnPermissionDenied(ctx, func() (*iamv2.AuthPolicy, error) {
		return r.updateSessionDuration(ctx, plan.ParentID.ValueString(), durationpb.New(d))
	}); err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to set auth policy session duration"))
		return
	}

	plan.ID = plan.ParentID
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state with the latest data.
func (r *authPolicyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state authPolicyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("read auth policy request: %s", state.ID))

	id := state.ID.ValueString()
	policy, err := r.prov.clientV2GA.IAM().AuthPoliciesService().GetAuthPolicy(ctx, &iamv2.GetAuthPolicyRequest{Uid: id})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			// The organization was deleted outside TF, remove from state.
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to get auth policy"))
		return
	}

	state.ID = types.StringValue(policy.GetUid())
	state.ParentID = types.StringValue(policy.GetUid())
	state.SessionDuration = sessionDurationState(state.SessionDuration, policy.GetSessionDuration())
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *authPolicyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data authPolicyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("update auth policy request: %s", data.ID))

	d, err := parseSessionDuration(data.SessionDuration.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("invalid session_duration", err.Error())
		return
	}
	if _, err := r.updateSessionDuration(ctx, data.ID.ValueString(), durationpb.New(d)); err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to update auth policy session duration"))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete returns the organization's session length to Chainguard's default.
// The policy itself cannot be deleted.
func (r *authPolicyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state authPolicyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("delete auth policy request: %s", state.ID))

	id := state.ID.ValueString()
	if _, err := r.updateSessionDuration(ctx, id, nil); err != nil {
		if status.Code(err) == codes.NotFound {
			return
		}
		resp.Diagnostics.Append(errorToDiagnostic(err, fmt.Sprintf("failed to reset auth policy session duration for %q", id)))
	}
}

// updateSessionDuration writes d as the session length of organization uid.
// A nil d, named in the mask with no value, clears it back to the default.
func (r *authPolicyResource) updateSessionDuration(ctx context.Context, uid string, d *durationpb.Duration) (*iamv2.AuthPolicy, error) {
	return r.prov.clientV2GA.IAM().AuthPoliciesService().UpdateAuthPolicy(ctx, &iamv2.UpdateAuthPolicyRequest{
		AuthPolicy: &iamv2.AuthPolicy{
			Uid:             uid,
			SessionDuration: d,
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{authPolicySessionDurationPath}},
	})
}

// sessionDurationState returns the state value for the stored session length
// got. It keeps current when current names the same length, so a configured
// "30d" does not show a diff against a stored 720h. An unset length (the
// default) is null, which a configured value then shows as drift.
func sessionDurationState(current types.String, got *durationpb.Duration) types.String {
	if got == nil {
		return types.StringNull()
	}
	d := got.AsDuration()
	if c, err := parseSessionDuration(current.ValueString()); err == nil && c == d {
		return current
	}
	return types.StringValue(formatSessionDuration(d))
}

// parseSessionDuration parses s as a Go duration or a whole number of days
// ("30d"), the form chainctl also takes, and checks it against the range the
// API accepts.
func parseSessionDuration(s string) (time.Duration, error) {
	var d time.Duration
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.ParseUint(days, 10, 16)
		if err != nil {
			return 0, fmt.Errorf("session_duration %q is not a whole number of days", s)
		}
		d = time.Duration(n) * 24 * time.Hour
	} else {
		var err error
		if d, err = time.ParseDuration(s); err != nil {
			return 0, fmt.Errorf("session_duration %q is not a duration such as 720h or 30d", s)
		}
	}
	if d%time.Second != 0 {
		return 0, fmt.Errorf("session_duration %q is not a whole number of seconds", s)
	}
	if d < sessionDurationMin || d > sessionDurationMax {
		return 0, fmt.Errorf("session_duration %q is out of range: it must be from 1h to 90d", s)
	}
	return d, nil
}

// formatSessionDuration renders d in the largest of hours, minutes or seconds
// that divides it exactly. time.Duration.String would render 720h as
// "720h0m0s".
func formatSessionDuration(d time.Duration) string {
	switch {
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	default:
		return fmt.Sprintf("%ds", d/time.Second)
	}
}

func validateSessionDuration(s string) error {
	_, err := parseSessionDuration(s)
	return err
}

func validateRootUIDP(s string) error {
	if !uidp.InRoot(strings.TrimSpace(s)) {
		return errors.New("parent_id must be a root organization UIDP: a sub-group has no auth policy of its own")
	}
	return nil
}
