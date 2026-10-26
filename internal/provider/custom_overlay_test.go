/*
Copyright 2026 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"google.golang.org/protobuf/proto"

	regv2 "chainguard.dev/sdk/proto/chainguard/platform/registry/v2beta1"
	registry "chainguard.dev/sdk/proto/platform/registry/v1"
)

func strList(elems ...string) types.List {
	vals := make([]attr.Value, 0, len(elems))
	for _, e := range elems {
		vals = append(vals, types.StringValue(e))
	}
	return types.ListValueMust(types.StringType, vals)
}

func strMap(kv map[string]string) types.Map {
	vals := make(map[string]attr.Value, len(kv))
	for k, v := range kv {
		vals[k] = types.StringValue(v)
	}
	return types.MapValueMust(types.StringType, vals)
}

func Test_validOverlayEnvKey(t *testing.T) {
	// Reserved prefix per the registry API: environment variable keys may
	// not begin with "CHAINGUARD_".
	if err := validOverlayEnvKey("CHAINGUARD_FOO"); err == nil {
		t.Error("expected error for reserved prefix CHAINGUARD_")
	}
	if err := validOverlayEnvKey("HTTP_PROXY"); err != nil {
		t.Errorf("unexpected error for HTTP_PROXY: %v", err)
	}
	// Not reserved: prefix match is exact, including the underscore.
	if err := validOverlayEnvKey("CHAINGUARDIAN"); err != nil {
		t.Errorf("unexpected error for CHAINGUARDIAN: %v", err)
	}
}

func Test_validOverlayAnnotationKey(t *testing.T) {
	// Reserved prefixes per the registry API: "dev.chainguard." and
	// "org.opencontainers.".
	for _, key := range []string{"dev.chainguard.foo", "org.opencontainers.image.title"} {
		if err := validOverlayAnnotationKey(key); err == nil {
			t.Errorf("expected error for reserved key %q", key)
		}
	}
	for _, key := range []string{"com.example.team", "dev.chainguardian.foo", "org.opencontainersx.y"} {
		if err := validOverlayAnnotationKey(key); err != nil {
			t.Errorf("unexpected error for %q: %v", key, err)
		}
	}
}

func Test_overlayErrorDiagnostic(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantHint string
	}{{
		// The repo is not a custom-assembly image (no apko_overlay on
		// its sync_config): explain entitlement instead of leaking a
		// bare PermissionDenied.
		name:     "not a custom image",
		err:      status.Error(codes.PermissionDenied, "custom_overlay can only be set for custom images"),
		wantHint: "custom assembly",
	}, {
		name:     "certificates not entitled",
		err:      status.Error(codes.InvalidArgument, "using custom_overlay.certificates is not allowed"),
		wantHint: "certificates",
	}, {
		name:     "runtime repositories not entitled",
		err:      status.Error(codes.PermissionDenied, "using custom_overlay.contents.runtime_repositories is not allowed"),
		wantHint: "runtime_repositories",
	}, {
		name:     "overlay without sync_config",
		err:      status.Error(codes.InvalidArgument, "custom_overlay requires a sync_config"),
		wantHint: "sync_config",
	}, {
		// Unrelated errors pass through errorToDiagnostic untouched.
		name:     "unrelated error",
		err:      status.Error(codes.NotFound, "no repo instance found"),
		wantHint: "",
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := overlayErrorDiagnostic(tt.err, "failed to update image repo")
			if tt.wantHint == "" {
				want := errorToDiagnostic(tt.err, "failed to update image repo")
				if !d.Equal(want) {
					t.Errorf("expected passthrough diagnostic, got %v", d)
				}
				return
			}
			if !strings.Contains(d.Detail(), tt.wantHint) {
				t.Errorf("detail %q does not mention %q", d.Detail(), tt.wantHint)
			}
			if !strings.Contains(d.Detail(), status.Convert(tt.err).Message()) {
				t.Errorf("detail %q does not preserve the server message", d.Detail())
			}
		})
	}
}

func Test_normalizeCustomOverlay(t *testing.T) {
	tests := []struct {
		name string
		in   *registry.CustomOverlay
		want *registry.CustomOverlay
	}{{
		name: "nil is nil",
		in:   nil,
		want: nil,
	}, {
		// A cleared overlay reads back from the API as an empty,
		// non-nil message (protojson stores "{}"), which must be
		// indistinguishable from no overlay at all.
		name: "empty message is nil",
		in:   &registry.CustomOverlay{},
		want: nil,
	}, {
		name: "all sections present but empty is nil",
		in: &registry.CustomOverlay{
			Contents:     &registry.ImageContents{},
			Environment:  map[string]string{},
			Annotations:  map[string]string{},
			Accounts:     &registry.ApkoConfig_Accounts{},
			Certificates: &registry.CustomOverlay_Certificates{},
		},
		want: nil,
	}, {
		name: "packages survive, empty siblings pruned",
		in: &registry.CustomOverlay{
			Contents: &registry.ImageContents{
				Packages: []string{"curl", "jq"},
			},
			Environment:  map[string]string{},
			Accounts:     &registry.ApkoConfig_Accounts{},
			Certificates: &registry.CustomOverlay_Certificates{},
		},
		want: &registry.CustomOverlay{
			Contents: &registry.ImageContents{
				Packages: []string{"curl", "jq"},
			},
		},
	}, {
		name: "environment alone survives",
		in: &registry.CustomOverlay{
			Environment: map[string]string{"FOO": "bar"},
		},
		want: &registry.CustomOverlay{
			Environment: map[string]string{"FOO": "bar"},
		},
	}, {
		name: "run_as alone keeps accounts",
		in: &registry.CustomOverlay{
			Accounts: &registry.ApkoConfig_Accounts{RunAs: "65532"},
		},
		want: &registry.CustomOverlay{
			Accounts: &registry.ApkoConfig_Accounts{RunAs: "65532"},
		},
	}, {
		name: "certificate providers alone survive",
		in: &registry.CustomOverlay{
			Certificates: &registry.CustomOverlay_Certificates{
				Providers: []string{"my-corp-certs"},
			},
		},
		want: &registry.CustomOverlay{
			Certificates: &registry.CustomOverlay_Certificates{
				Providers: []string{"my-corp-certs"},
			},
		},
	}, {
		name: "runtime repositories alone keep contents",
		in: &registry.CustomOverlay{
			Contents: &registry.ImageContents{
				RuntimeRepositories: []string{"https://apk.example.com/repo"},
			},
		},
		want: &registry.CustomOverlay{
			Contents: &registry.ImageContents{
				RuntimeRepositories: []string{"https://apk.example.com/repo"},
			},
		},
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeCustomOverlay(tt.in)
			if !proto.Equal(got, tt.want) {
				t.Errorf("normalizeCustomOverlay() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_customOverlayToProto(t *testing.T) {
	gid := uint32(1001)

	tests := []struct {
		name string
		in   *customOverlayModel
		want *registry.CustomOverlay
	}{{
		name: "nil model is nil proto",
		in:   nil,
		want: nil,
	}, {
		// `custom_overlay {}` with nothing inside means the same as no
		// block at all: clear the overlay.
		name: "empty block is nil proto",
		in:   &customOverlayModel{},
		want: nil,
	}, {
		name: "full overlay surface",
		in: &customOverlayModel{
			Contents: &overlayContentsModel{
				Packages:            strList("curl", "jq"),
				RuntimeRepositories: strList("https://apk.example.com/repo"),
			},
			Environment: strMap(map[string]string{"HTTP_PROXY": "http://proxy:3128"}),
			Annotations: strMap(map[string]string{"com.example.team": "platform"}),
			Accounts: &overlayAccountsModel{
				RunAs: types.StringValue("65532"),
				Users: []overlayUserModel{{
					Username:  types.StringValue("app"),
					UID:       types.Int64Value(1000),
					GID:       types.Int64Value(1001),
					GroupName: types.StringValue("app"),
					Shell:     types.StringValue("/bin/sh"),
					HomeDir:   types.StringValue("/home/app"),
				}},
				Groups: []overlayGroupModel{{
					Groupname: types.StringValue("app"),
					GID:       types.Int64Value(1001),
					Members:   strList("app"),
				}},
			},
			Certificates: &overlayCertificatesModel{
				Additional: []overlayCertificateEntryModel{{
					Name:    types.StringValue("corp-ca.pem"),
					Content: types.StringValue("-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"),
				}},
				Providers: strList("my-corp-certs"),
			},
		},
		want: &registry.CustomOverlay{
			Contents: &registry.ImageContents{
				Packages:            []string{"curl", "jq"},
				RuntimeRepositories: []string{"https://apk.example.com/repo"},
			},
			Environment: map[string]string{"HTTP_PROXY": "http://proxy:3128"},
			Annotations: map[string]string{"com.example.team": "platform"},
			Accounts: &registry.ApkoConfig_Accounts{
				RunAs: "65532",
				Users: []*registry.ApkoConfig_Accounts_User{{
					UserName:  "app",
					Uid:       1000,
					Gid:       &gid,
					GroupName: "app",
					Shell:     "/bin/sh",
					HomeDir:   "/home/app",
				}},
				Groups: []*registry.ApkoConfig_Accounts_Group{{
					GroupName: "app",
					Gid:       1001,
					Members:   []string{"app"},
				}},
			},
			Certificates: &registry.CustomOverlay_Certificates{
				Additional: []*registry.CustomOverlay_Certificates_AdditionalEntry{{
					Name:    "corp-ca.pem",
					Content: "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n",
				}},
				Providers: []string{"my-corp-certs"},
			},
		},
	}, {
		name: "packages only",
		in: &customOverlayModel{
			Contents: &overlayContentsModel{
				Packages: strList("curl"),
			},
		},
		want: &registry.CustomOverlay{
			Contents: &registry.ImageContents{Packages: []string{"curl"}},
		},
	}, {
		// uid/gid omitted from config: uid falls back to the proto zero
		// value, the optional gid stays unset.
		name: "user with null uid and gid",
		in: &customOverlayModel{
			Accounts: &overlayAccountsModel{
				Users: []overlayUserModel{{
					Username: types.StringValue("app"),
				}},
			},
		},
		want: &registry.CustomOverlay{
			Accounts: &registry.ApkoConfig_Accounts{
				Users: []*registry.ApkoConfig_Accounts_User{{
					UserName: "app",
				}},
			},
		},
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, diags := customOverlayToProto(context.Background(), tt.in)
			if diags.HasError() {
				t.Fatalf("customOverlayToProto() diagnostics: %v", diags)
			}
			if !proto.Equal(got, tt.want) {
				t.Errorf("customOverlayToProto() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_customOverlayV1ToModel(t *testing.T) {
	ctx := context.Background()
	gid := uint32(1001)

	tests := []struct {
		name string
		in   *registry.CustomOverlay
	}{{
		name: "nil is nil model",
		in:   nil,
	}, {
		name: "empty message is nil model",
		in:   &registry.CustomOverlay{},
	}, {
		name: "full overlay surface",
		in: &registry.CustomOverlay{
			Contents: &registry.ImageContents{
				Packages:            []string{"curl", "jq"},
				RuntimeRepositories: []string{"https://apk.example.com/repo"},
			},
			Environment: map[string]string{"HTTP_PROXY": "http://proxy:3128"},
			Annotations: map[string]string{"com.example.team": "platform"},
			Accounts: &registry.ApkoConfig_Accounts{
				RunAs: "65532",
				Users: []*registry.ApkoConfig_Accounts_User{{
					UserName:  "app",
					Uid:       1000,
					Gid:       &gid,
					GroupName: "app",
					Shell:     "/bin/sh",
					HomeDir:   "/home/app",
				}},
				Groups: []*registry.ApkoConfig_Accounts_Group{{
					GroupName: "app",
					Gid:       1001,
					Members:   []string{"app"},
				}},
			},
			Certificates: &registry.CustomOverlay_Certificates{
				Additional: []*registry.CustomOverlay_Certificates_AdditionalEntry{{
					Name:    "corp-ca.pem",
					Content: "cert-pem",
				}},
				Providers: []string{"my-corp-certs"},
			},
		},
	}, {
		name: "packages only",
		in: &registry.CustomOverlay{
			Contents: &registry.ImageContents{Packages: []string{"curl"}},
		},
	}, {
		name: "user without uid or gid",
		in: &registry.CustomOverlay{
			Accounts: &registry.ApkoConfig_Accounts{
				Users: []*registry.ApkoConfig_Accounts_User{{UserName: "app"}},
			},
		},
	}}

	// Converting proto -> model -> proto must reproduce the normalized
	// input; the model is a faithful, lossless view of the overlay.
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model, diags := customOverlayV1ToModel(ctx, tt.in)
			if diags.HasError() {
				t.Fatalf("customOverlayV1ToModel() diagnostics: %v", diags)
			}
			if normalizeCustomOverlay(tt.in) == nil {
				if model != nil {
					t.Fatalf("customOverlayV1ToModel() = %v, want nil", model)
				}
				return
			}
			back, diags := customOverlayToProto(ctx, model)
			if diags.HasError() {
				t.Fatalf("customOverlayToProto() diagnostics: %v", diags)
			}
			if want := normalizeCustomOverlay(tt.in); !proto.Equal(back, want) {
				t.Errorf("round trip = %v, want %v", back, want)
			}
		})
	}

	t.Run("v2 overlay maps to the same model as its v1 twin", func(t *testing.T) {
		gid := uint32(1001)
		v2 := &regv2.CustomOverlay{
			Contents: &regv2.CustomOverlay_ImageContents{
				Packages:            []string{"curl"},
				RuntimeRepositories: []string{"https://apk.example.com/repo"},
			},
			Environment: map[string]string{"HTTP_PROXY": "http://proxy:3128"},
			Annotations: map[string]string{"com.example.team": "platform"},
			Accounts: &regv2.CustomOverlay_Accounts{
				RunAs: "65532",
				Users: []*regv2.CustomOverlay_Accounts_User{{
					Username:  "app",
					Uid:       1000,
					Gid:       1001,
					GroupName: "app",
					Shell:     "/bin/sh",
					HomeDir:   "/home/app",
				}},
				Groups: []*regv2.CustomOverlay_Accounts_Group{{
					Groupname: "app",
					Gid:       1001,
					Members:   []string{"app"},
				}},
			},
			Certificates: &regv2.CustomOverlay_Certificates{
				Additional: []*regv2.CustomOverlay_Certificates_AdditionalEntry{{
					Name:    "corp-ca.pem",
					Content: "cert-pem",
				}},
				Providers: []string{"my-corp-certs"},
			},
		}
		wantV1 := &registry.CustomOverlay{
			Contents: &registry.ImageContents{
				Packages:            []string{"curl"},
				RuntimeRepositories: []string{"https://apk.example.com/repo"},
			},
			Environment: map[string]string{"HTTP_PROXY": "http://proxy:3128"},
			Annotations: map[string]string{"com.example.team": "platform"},
			Accounts: &registry.ApkoConfig_Accounts{
				RunAs: "65532",
				Users: []*registry.ApkoConfig_Accounts_User{{
					UserName:  "app",
					Uid:       1000,
					Gid:       &gid,
					GroupName: "app",
					Shell:     "/bin/sh",
					HomeDir:   "/home/app",
				}},
				Groups: []*registry.ApkoConfig_Accounts_Group{{
					GroupName: "app",
					Gid:       1001,
					Members:   []string{"app"},
				}},
			},
			Certificates: &registry.CustomOverlay_Certificates{
				Additional: []*registry.CustomOverlay_Certificates_AdditionalEntry{{
					Name:    "corp-ca.pem",
					Content: "cert-pem",
				}},
				Providers: []string{"my-corp-certs"},
			},
		}

		model, diags := customOverlayV2ToModel(ctx, v2)
		if diags.HasError() {
			t.Fatalf("customOverlayV2ToModel() diagnostics: %v", diags)
		}
		got, diags := customOverlayToProto(ctx, model)
		if diags.HasError() {
			t.Fatalf("customOverlayToProto() diagnostics: %v", diags)
		}
		if !proto.Equal(got, wantV1) {
			t.Errorf("v2 -> model -> v1 = %v, want %v", got, wantV1)
		}
	})

	t.Run("empty v2 overlay is nil model", func(t *testing.T) {
		for _, in := range []*regv2.CustomOverlay{nil, {}, {Contents: &regv2.CustomOverlay_ImageContents{}}} {
			model, diags := customOverlayV2ToModel(ctx, in)
			if diags.HasError() {
				t.Fatalf("customOverlayV2ToModel() diagnostics: %v", diags)
			}
			if model != nil {
				t.Errorf("customOverlayV2ToModel(%v) = %v, want nil", in, model)
			}
		}
	})

	t.Run("zero uid maps to null", func(t *testing.T) {
		model, diags := customOverlayV1ToModel(ctx, &registry.CustomOverlay{
			Accounts: &registry.ApkoConfig_Accounts{
				Users: []*registry.ApkoConfig_Accounts_User{{UserName: "app"}},
			},
		})
		if diags.HasError() {
			t.Fatalf("customOverlayV1ToModel() diagnostics: %v", diags)
		}
		u := model.Accounts.Users[0]
		if !u.UID.IsNull() || !u.GID.IsNull() {
			t.Errorf("unset uid/gid should be null, got uid=%v gid=%v", u.UID, u.GID)
		}
		if !u.Shell.IsNull() {
			t.Errorf("unset shell should be null, got %v", u.Shell)
		}
	})
}
