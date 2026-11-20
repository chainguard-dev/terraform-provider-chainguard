/*
Copyright 2026 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"google.golang.org/grpc/status"

	regv2 "chainguard.dev/sdk/proto/chainguard/platform/registry/v2beta1"
	registry "chainguard.dev/sdk/proto/platform/registry/v1"
	"github.com/chainguard-dev/terraform-provider-chainguard/internal/validators"
)

// customOverlayResourceBlock returns the custom_overlay block schema for the
// chainguard_image_repo resource. All changes apply in place; the platform
// rebuilds affected images asynchronously after an update is accepted.
func customOverlayResourceBlock() rschema.Block {
	return rschema.SingleNestedBlock{
		Description: "Custom assembly overlay applied to images in this repo during rebuilds. " +
			"Requires custom assembly to be enabled for the repo. Removing this block clears the overlay. " +
			"Changes are accepted immediately and images are rebuilt asynchronously (typically within minutes).",
		Attributes: map[string]rschema.Attribute{
			"environment": rschema.MapAttribute{
				Description: "Environment variables to set in the image. Keys with the reserved prefix CHAINGUARD_ are rejected.",
				Optional:    true,
				ElementType: types.StringType,
				Validators: []validator.Map{
					mapvalidator.KeysAre(validators.ValidateStringFuncs(validOverlayEnvKey)),
				},
			},
			"annotations": rschema.MapAttribute{
				Description: "OCI annotations to add to the image. Keys with the reserved prefixes dev.chainguard. and org.opencontainers. are rejected.",
				Optional:    true,
				ElementType: types.StringType,
				Validators: []validator.Map{
					mapvalidator.KeysAre(validators.ValidateStringFuncs(validOverlayAnnotationKey)),
				},
			},
		},
		Blocks: map[string]rschema.Block{
			"contents": rschema.SingleNestedBlock{
				Description: "Additional image contents.",
				Attributes: map[string]rschema.Attribute{
					"packages": rschema.ListAttribute{
						Description: "Packages to append to the image.",
						Optional:    true,
						ElementType: types.StringType,
					},
					"runtime_repositories": rschema.ListAttribute{
						Description: "Custom APK repository URLs to write to /etc/apk/repositories in the assembled image. When set, these replace the default virtualapk.cgr.dev repositories. Requires an additional entitlement.",
						Optional:    true,
						ElementType: types.StringType,
					},
				},
			},
			"accounts": rschema.SingleNestedBlock{
				Description: "Account customizations applied during rebuilds.",
				Attributes: map[string]rschema.Attribute{
					"run_as": rschema.StringAttribute{
						Description: "User (name or UID) the image runs as.",
						Optional:    true,
					},
				},
				Blocks: map[string]rschema.Block{
					"user": rschema.ListNestedBlock{
						Description: "User accounts to add to the image.",
						NestedObject: rschema.NestedBlockObject{
							Attributes: map[string]rschema.Attribute{
								"username": rschema.StringAttribute{
									Description: "Username for the account.",
									Required:    true,
								},
								"uid": rschema.Int64Attribute{
									Description: "User ID for the account.",
									Optional:    true,
									Validators:  []validator.Int64{int64validator.Between(0, math.MaxUint32)},
								},
								"gid": rschema.Int64Attribute{
									Description: "Primary group ID for the account.",
									Optional:    true,
									Validators:  []validator.Int64{int64validator.Between(0, math.MaxUint32)},
								},
								"group_name": rschema.StringAttribute{
									Description: "Primary group name for the account.",
									Optional:    true,
								},
								"shell": rschema.StringAttribute{
									Description: "Login shell for the account.",
									Optional:    true,
								},
								"home_dir": rschema.StringAttribute{
									Description: "Home directory for the account.",
									Optional:    true,
								},
							},
						},
					},
					"group": rschema.ListNestedBlock{
						Description: "Group accounts to add to the image.",
						NestedObject: rschema.NestedBlockObject{
							Attributes: map[string]rschema.Attribute{
								"groupname": rschema.StringAttribute{
									Description: "Name of the group.",
									Required:    true,
								},
								"gid": rschema.Int64Attribute{
									Description: "Group ID for the account.",
									Optional:    true,
									Validators:  []validator.Int64{int64validator.Between(0, math.MaxUint32)},
								},
								"members": rschema.ListAttribute{
									Description: "Members of the group.",
									Optional:    true,
									ElementType: types.StringType,
								},
							},
						},
					},
				},
			},
			"certificates": rschema.SingleNestedBlock{
				Description: "Custom certificates to include in the image. Requires an additional entitlement.",
				Attributes: map[string]rschema.Attribute{
					"providers": rschema.ListAttribute{
						Description: "Certificate providers via packages.",
						Optional:    true,
						ElementType: types.StringType,
					},
				},
				Blocks: map[string]rschema.Block{
					"additional": rschema.ListNestedBlock{
						Description: "Additional certificates to include.",
						NestedObject: rschema.NestedBlockObject{
							Attributes: map[string]rschema.Attribute{
								"name": rschema.StringAttribute{
									Description: "Name of the certificate file.",
									Required:    true,
								},
								"content": rschema.StringAttribute{
									Description: "PEM-encoded certificate content.",
									Required:    true,
								},
							},
						},
					},
				},
			},
		},
	}
}

// customOverlayDataSourceAttribute returns the computed custom_overlay
// attribute for the image repo data sources. Attribute names mirror the
// resource block so both marshal into customOverlayModel.
func customOverlayDataSourceAttribute() dschema.Attribute {
	return dschema.SingleNestedAttribute{
		Description: "Custom assembly overlay applied to images in this repo during rebuilds.",
		Computed:    true,
		Attributes: map[string]dschema.Attribute{
			"contents": dschema.SingleNestedAttribute{
				Description: "Additional image contents.",
				Computed:    true,
				Attributes: map[string]dschema.Attribute{
					"packages": dschema.ListAttribute{
						Description: "Packages to append to the image.",
						Computed:    true,
						ElementType: types.StringType,
					},
					"runtime_repositories": dschema.ListAttribute{
						Description: "Custom APK repository URLs used in the assembled image.",
						Computed:    true,
						ElementType: types.StringType,
					},
				},
			},
			"environment": dschema.MapAttribute{
				Description: "Environment variables set in the image.",
				Computed:    true,
				ElementType: types.StringType,
			},
			"annotations": dschema.MapAttribute{
				Description: "OCI annotations added to the image.",
				Computed:    true,
				ElementType: types.StringType,
			},
			"accounts": dschema.SingleNestedAttribute{
				Description: "Account customizations applied during rebuilds.",
				Computed:    true,
				Attributes: map[string]dschema.Attribute{
					"run_as": dschema.StringAttribute{
						Description: "User (name or UID) the image runs as.",
						Computed:    true,
					},
					"user": dschema.ListNestedAttribute{
						Description: "User accounts added to the image.",
						Computed:    true,
						NestedObject: dschema.NestedAttributeObject{
							Attributes: map[string]dschema.Attribute{
								"username":   dschema.StringAttribute{Description: "Username for the account.", Computed: true},
								"uid":        dschema.Int64Attribute{Description: "User ID for the account.", Computed: true},
								"gid":        dschema.Int64Attribute{Description: "Primary group ID for the account.", Computed: true},
								"group_name": dschema.StringAttribute{Description: "Primary group name for the account.", Computed: true},
								"shell":      dschema.StringAttribute{Description: "Login shell for the account.", Computed: true},
								"home_dir":   dschema.StringAttribute{Description: "Home directory for the account.", Computed: true},
							},
						},
					},
					"group": dschema.ListNestedAttribute{
						Description: "Group accounts added to the image.",
						Computed:    true,
						NestedObject: dschema.NestedAttributeObject{
							Attributes: map[string]dschema.Attribute{
								"groupname": dschema.StringAttribute{Description: "Name of the group.", Computed: true},
								"gid":       dschema.Int64Attribute{Description: "Group ID for the account.", Computed: true},
								"members": dschema.ListAttribute{
									Description: "Members of the group.",
									Computed:    true,
									ElementType: types.StringType,
								},
							},
						},
					},
				},
			},
			"certificates": dschema.SingleNestedAttribute{
				Description: "Custom certificates included in the image.",
				Computed:    true,
				Attributes: map[string]dschema.Attribute{
					"additional": dschema.ListNestedAttribute{
						Description: "Additional certificates included.",
						Computed:    true,
						NestedObject: dschema.NestedAttributeObject{
							Attributes: map[string]dschema.Attribute{
								"name":    dschema.StringAttribute{Description: "Name of the certificate file.", Computed: true},
								"content": dschema.StringAttribute{Description: "PEM-encoded certificate content.", Computed: true},
							},
						},
					},
					"providers": dschema.ListAttribute{
						Description: "Certificate providers via packages.",
						Computed:    true,
						ElementType: types.StringType,
					},
				},
			},
		},
	}
}

// validOverlayEnvKey implements validators.ValidateStringFunc, mirroring the
// registry API's reserved-prefix check for overlay environment variables.
func validOverlayEnvKey(s string) error {
	if strings.HasPrefix(s, "CHAINGUARD_") {
		return fmt.Errorf("environment variable %q uses reserved prefix %q", s, "CHAINGUARD_")
	}
	return nil
}

// validOverlayAnnotationKey implements validators.ValidateStringFunc,
// mirroring the registry API's reserved-prefix checks for overlay annotations.
func validOverlayAnnotationKey(s string) error {
	for _, prefix := range []string{"dev.chainguard.", "org.opencontainers."} {
		if strings.HasPrefix(s, prefix) {
			return fmt.Errorf("annotation key %q uses reserved prefix %q", s, prefix)
		}
	}
	return nil
}

// overlayErrorHints maps known registry error messages about custom_overlay
// to actionable guidance. Entitlement failures are only discoverable through
// these apply-time errors — they cannot be validated at plan time.
var overlayErrorHints = map[string]string{
	"custom_overlay can only be set for custom images": "This repo does not have custom assembly enabled " +
		"(its sync_config has no apko_overlay). Custom overlays can only be set on custom-assembly-enabled repos; " +
		"contact your Chainguard account team to enable custom assembly for your organization.",
	"using custom_overlay.certificates is not allowed": "Your organization is not entitled to use custom " +
		"certificates in image builds. Contact your Chainguard account team to enable this feature, or remove " +
		"the certificates block from custom_overlay.",
	"using custom_overlay.contents.runtime_repositories is not allowed": "Your organization is not entitled to " +
		"use custom runtime repositories in image builds. Contact your Chainguard account team to enable this " +
		"feature, or remove contents.runtime_repositories from custom_overlay.",
	"custom_overlay requires a sync_config": "A custom_overlay can only be set on a repo that syncs from a " +
		"Chainguard catalog source. Add a sync_config block with a source, or remove the custom_overlay block.",
}

// overlayErrorDiagnostic wraps registry errors about custom_overlay with
// actionable guidance, and defers to errorToDiagnostic for everything else.
func overlayErrorDiagnostic(err error, summary string) diag.Diagnostic {
	msg := status.Convert(err).Message()
	for needle, hint := range overlayErrorHints {
		if strings.Contains(msg, needle) {
			return diag.NewErrorDiagnostic(summary, fmt.Sprintf("%s: %s\n\n%s", status.Code(err), msg, hint))
		}
	}
	return errorToDiagnostic(err, summary)
}

// customOverlayModel is the terraform view of registry.CustomOverlay, shared
// by the chainguard_image_repo resource and the image repo data sources.
type customOverlayModel struct {
	Contents     *overlayContentsModel     `tfsdk:"contents"`
	Environment  types.Map                 `tfsdk:"environment"`
	Annotations  types.Map                 `tfsdk:"annotations"`
	Accounts     *overlayAccountsModel     `tfsdk:"accounts"`
	Certificates *overlayCertificatesModel `tfsdk:"certificates"`
}

type overlayContentsModel struct {
	Packages            types.List `tfsdk:"packages"`
	RuntimeRepositories types.List `tfsdk:"runtime_repositories"`
}

type overlayAccountsModel struct {
	RunAs  types.String        `tfsdk:"run_as"`
	Users  []overlayUserModel  `tfsdk:"user"`
	Groups []overlayGroupModel `tfsdk:"group"`
}

type overlayUserModel struct {
	Username  types.String `tfsdk:"username"`
	UID       types.Int64  `tfsdk:"uid"`
	GID       types.Int64  `tfsdk:"gid"`
	GroupName types.String `tfsdk:"group_name"`
	Shell     types.String `tfsdk:"shell"`
	HomeDir   types.String `tfsdk:"home_dir"`
}

type overlayGroupModel struct {
	Groupname types.String `tfsdk:"groupname"`
	GID       types.Int64  `tfsdk:"gid"`
	Members   types.List   `tfsdk:"members"`
}

type overlayCertificatesModel struct {
	Additional []overlayCertificateEntryModel `tfsdk:"additional"`
	Providers  types.List                     `tfsdk:"providers"`
}

type overlayCertificateEntryModel struct {
	Name    types.String `tfsdk:"name"`
	Content types.String `tfsdk:"content"`
}

// stringSlice converts a types.List of strings to a []string, treating
// null/unknown lists as empty.
func stringSlice(ctx context.Context, l types.List, diags *diag.Diagnostics) []string {
	if l.IsNull() || l.IsUnknown() {
		return nil
	}
	out := make([]string, 0, len(l.Elements()))
	diags.Append(l.ElementsAs(ctx, &out, false /* allowUnhandled */)...)
	return out
}

// stringMap converts a types.Map of strings to a map[string]string, treating
// null/unknown maps as empty.
func stringMap(ctx context.Context, m types.Map, diags *diag.Diagnostics) map[string]string {
	if m.IsNull() || m.IsUnknown() {
		return nil
	}
	out := make(map[string]string, len(m.Elements()))
	diags.Append(m.ElementsAs(ctx, &out, false /* allowUnhandled */)...)
	return out
}

// customOverlayToProto converts the terraform model to the v1 proto the
// registry API mutates with, normalized so that an empty block means "no
// overlay" (nil).
func customOverlayToProto(ctx context.Context, m *customOverlayModel) (*registry.CustomOverlay, diag.Diagnostics) {
	var diags diag.Diagnostics
	if m == nil {
		return nil, diags
	}

	o := &registry.CustomOverlay{
		Environment: stringMap(ctx, m.Environment, &diags),
		Annotations: stringMap(ctx, m.Annotations, &diags),
	}

	if m.Contents != nil {
		o.Contents = &registry.ImageContents{
			Packages:            stringSlice(ctx, m.Contents.Packages, &diags),
			RuntimeRepositories: stringSlice(ctx, m.Contents.RuntimeRepositories, &diags),
		}
	}

	if m.Accounts != nil {
		accounts := &registry.ApkoConfig_Accounts{
			RunAs: m.Accounts.RunAs.ValueString(),
		}
		for _, u := range m.Accounts.Users {
			user := &registry.ApkoConfig_Accounts_User{
				UserName:  u.Username.ValueString(),
				Uid:       uint32(u.UID.ValueInt64()),
				GroupName: u.GroupName.ValueString(),
				Shell:     u.Shell.ValueString(),
				HomeDir:   u.HomeDir.ValueString(),
			}
			if !u.GID.IsNull() && !u.GID.IsUnknown() {
				gid := uint32(u.GID.ValueInt64())
				user.Gid = &gid
			}
			accounts.Users = append(accounts.Users, user)
		}
		for _, g := range m.Accounts.Groups {
			accounts.Groups = append(accounts.Groups, &registry.ApkoConfig_Accounts_Group{
				GroupName: g.Groupname.ValueString(),
				Gid:       uint32(g.GID.ValueInt64()),
				Members:   stringSlice(ctx, g.Members, &diags),
			})
		}
		o.Accounts = accounts
	}

	if m.Certificates != nil {
		certs := &registry.CustomOverlay_Certificates{
			Providers: stringSlice(ctx, m.Certificates.Providers, &diags),
		}
		for _, e := range m.Certificates.Additional {
			certs.Additional = append(certs.Additional, &registry.CustomOverlay_Certificates_AdditionalEntry{
				Name:    e.Name.ValueString(),
				Content: e.Content.ValueString(),
			})
		}
		o.Certificates = certs
	}

	return normalizeCustomOverlay(o), diags
}

// nullableString maps the proto zero value to a null terraform string so
// fields the user omitted don't read back as "".
func nullableString(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

// nullableStringList maps an empty slice to a null list.
func nullableStringList(ctx context.Context, s []string, diags *diag.Diagnostics) types.List {
	if len(s) == 0 {
		return types.ListNull(types.StringType)
	}
	l, d := types.ListValueFrom(ctx, types.StringType, s)
	diags.Append(d...)
	return l
}

// nullableStringMap maps an empty map to a null map.
func nullableStringMap(ctx context.Context, m map[string]string, diags *diag.Diagnostics) types.Map {
	if len(m) == 0 {
		return types.MapNull(types.StringType)
	}
	v, d := types.MapValueFrom(ctx, types.StringType, m)
	diags.Append(d...)
	return v
}

// customOverlayV1ToModel converts the v1 proto to the terraform model,
// normalized: an empty overlay yields a nil model (no block).
func customOverlayV1ToModel(ctx context.Context, o *registry.CustomOverlay) (*customOverlayModel, diag.Diagnostics) {
	var diags diag.Diagnostics
	o = normalizeCustomOverlay(o)
	if o == nil {
		return nil, diags
	}

	m := &customOverlayModel{
		Environment: nullableStringMap(ctx, o.GetEnvironment(), &diags),
		Annotations: nullableStringMap(ctx, o.GetAnnotations(), &diags),
	}

	if c := o.GetContents(); c != nil {
		m.Contents = &overlayContentsModel{
			Packages:            nullableStringList(ctx, c.GetPackages(), &diags),
			RuntimeRepositories: nullableStringList(ctx, c.GetRuntimeRepositories(), &diags),
		}
	}

	if a := o.GetAccounts(); a != nil {
		accounts := &overlayAccountsModel{
			RunAs: nullableString(a.GetRunAs()),
		}
		for _, u := range a.GetUsers() {
			user := overlayUserModel{
				Username:  types.StringValue(u.GetUserName()),
				UID:       types.Int64Null(),
				GID:       types.Int64Null(),
				GroupName: nullableString(u.GetGroupName()),
				Shell:     nullableString(u.GetShell()),
				HomeDir:   nullableString(u.GetHomeDir()),
			}
			if u.GetUid() != 0 {
				user.UID = types.Int64Value(int64(u.GetUid()))
			}
			if u.Gid != nil {
				user.GID = types.Int64Value(int64(u.GetGid()))
			}
			accounts.Users = append(accounts.Users, user)
		}
		for _, g := range a.GetGroups() {
			group := overlayGroupModel{
				Groupname: types.StringValue(g.GetGroupName()),
				GID:       types.Int64Null(),
				Members:   nullableStringList(ctx, g.GetMembers(), &diags),
			}
			if g.GetGid() != 0 {
				group.GID = types.Int64Value(int64(g.GetGid()))
			}
			accounts.Groups = append(accounts.Groups, group)
		}
		m.Accounts = accounts
	}

	if c := o.GetCertificates(); c != nil {
		certs := &overlayCertificatesModel{
			Providers: nullableStringList(ctx, c.GetProviders(), &diags),
		}
		for _, e := range c.GetAdditional() {
			certs.Additional = append(certs.Additional, overlayCertificateEntryModel{
				Name:    types.StringValue(e.GetName()),
				Content: types.StringValue(e.GetContent()),
			})
		}
		m.Certificates = certs
	}

	return m, diags
}

// customOverlayV2ToModel converts the v2beta1 proto (the generation the data
// sources read with) to the terraform model by translating it into its v1
// twin first, so all normalization lives in one place. The messages are
// structurally identical apart from field naming and the v1 user gid being
// optional — v2's zero gid is treated as unset.
func customOverlayV2ToModel(ctx context.Context, o *regv2.CustomOverlay) (*customOverlayModel, diag.Diagnostics) {
	if o == nil {
		return customOverlayV1ToModel(ctx, nil)
	}

	v1 := &registry.CustomOverlay{
		Environment: o.GetEnvironment(),
		Annotations: o.GetAnnotations(),
	}
	if c := o.GetContents(); c != nil {
		v1.Contents = &registry.ImageContents{
			Packages:            c.GetPackages(),
			RuntimeRepositories: c.GetRuntimeRepositories(),
		}
	}
	if a := o.GetAccounts(); a != nil {
		accounts := &registry.ApkoConfig_Accounts{RunAs: a.GetRunAs()}
		for _, u := range a.GetUsers() {
			user := &registry.ApkoConfig_Accounts_User{
				UserName:  u.GetUsername(),
				Uid:       uint32(u.GetUid()),
				GroupName: u.GetGroupName(),
				Shell:     u.GetShell(),
				HomeDir:   u.GetHomeDir(),
			}
			if u.GetGid() != 0 {
				gid := uint32(u.GetGid())
				user.Gid = &gid
			}
			accounts.Users = append(accounts.Users, user)
		}
		for _, g := range a.GetGroups() {
			accounts.Groups = append(accounts.Groups, &registry.ApkoConfig_Accounts_Group{
				GroupName: g.GetGroupname(),
				Gid:       uint32(g.GetGid()),
				Members:   g.GetMembers(),
			})
		}
		v1.Accounts = accounts
	}
	if c := o.GetCertificates(); c != nil {
		certs := &registry.CustomOverlay_Certificates{Providers: c.GetProviders()}
		for _, e := range c.GetAdditional() {
			certs.Additional = append(certs.Additional, &registry.CustomOverlay_Certificates_AdditionalEntry{
				Name:    e.GetName(),
				Content: e.GetContent(),
			})
		}
		v1.Certificates = certs
	}

	return customOverlayV1ToModel(ctx, v1)
}

// normalizeCustomOverlay returns a semantically-equivalent overlay with empty
// sub-sections pruned, and nil when nothing remains. The API reads a cleared
// overlay back as an empty non-nil message (it is stored as protojson "{}"),
// so nil, the empty message, and all-sections-empty must all compare equal or
// every clear produces a perpetual diff.
func normalizeCustomOverlay(o *registry.CustomOverlay) *registry.CustomOverlay {
	if o == nil {
		return nil
	}
	out := &registry.CustomOverlay{}
	if c := o.GetContents(); len(c.GetPackages()) > 0 || len(c.GetRuntimeRepositories()) > 0 {
		out.Contents = &registry.ImageContents{
			Packages:            c.GetPackages(),
			RuntimeRepositories: c.GetRuntimeRepositories(),
		}
	}
	if len(o.GetEnvironment()) > 0 {
		out.Environment = o.GetEnvironment()
	}
	if len(o.GetAnnotations()) > 0 {
		out.Annotations = o.GetAnnotations()
	}
	if a := o.GetAccounts(); a.GetRunAs() != "" || len(a.GetUsers()) > 0 || len(a.GetGroups()) > 0 {
		out.Accounts = a
	}
	if c := o.GetCertificates(); len(c.GetAdditional()) > 0 || len(c.GetProviders()) > 0 {
		out.Certificates = c
	}
	if out.Contents == nil && out.Environment == nil && out.Annotations == nil &&
		out.Accounts == nil && out.Certificates == nil {
		return nil
	}
	return out
}
