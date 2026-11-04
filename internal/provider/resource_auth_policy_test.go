/*
Copyright 2026 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	clientsv2ga "chainguard.dev/sdk/proto/chainguard/platform/clients/v2"
	iamv2 "chainguard.dev/sdk/proto/chainguard/platform/iam/v2"
	iamv2test "chainguard.dev/sdk/proto/chainguard/platform/iam/v2/test"
	"chainguard.dev/sdk/proto/chainguard/platform/test"
)

// mockV2GAClients implements clientsv2ga.Clients for unit tests. Only IAM is
// served; every other method panics through the nil embedded interface.
type mockV2GAClients struct {
	clientsv2ga.Clients
	iamClients iamv2.Clients
}

func (m *mockV2GAClients) IAM() iamv2.Clients { return m.iamClients }

func TestParseSessionDuration(t *testing.T) {
	for _, c := range []struct {
		name    string
		in      string
		want    time.Duration
		wantErr bool
	}{
		{name: "hours", in: "720h", want: 720 * time.Hour},
		{name: "days", in: "30d", want: 720 * time.Hour},
		{name: "minimum", in: "1h", want: time.Hour},
		{name: "minimum in minutes", in: "60m", want: time.Hour},
		{name: "maximum in days", in: "90d", want: 90 * 24 * time.Hour},
		{name: "maximum in hours", in: "2160h", want: 2160 * time.Hour},
		{name: "mixed units", in: "1h30m", want: 90 * time.Minute},
		{name: "fractional hours on a whole second", in: "1.5h", want: 90 * time.Minute},
		{name: "below minimum", in: "59m59s", wantErr: true},
		{name: "above maximum", in: "91d", wantErr: true},
		{name: "above maximum by a second", in: "2160h1s", wantErr: true},
		{name: "zero", in: "0s", wantErr: true},
		{name: "zero days", in: "0d", wantErr: true},
		{name: "negative", in: "-720h", wantErr: true},
		{name: "negative days", in: "-30d", wantErr: true},
		{name: "fractional days", in: "1.5d", wantErr: true},
		{name: "bare suffix", in: "d", wantErr: true},
		{name: "sub-second", in: "1h500ms", wantErr: true},
		{name: "empty", in: "", wantErr: true},
		{name: "default is not a duration", in: "default", wantErr: true},
		{name: "garbage", in: "a month", wantErr: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseSessionDuration(c.in)
			if (err != nil) != c.wantErr {
				t.Fatalf("parseSessionDuration(%q) error: got = %v, wantErr = %t", c.in, err, c.wantErr)
			}
			if got != c.want {
				t.Errorf("parseSessionDuration(%q): got = %v, want = %v", c.in, got, c.want)
			}
		})
	}
}

func TestFormatSessionDuration(t *testing.T) {
	for _, c := range []struct {
		in   time.Duration
		want string
	}{
		{in: time.Hour, want: "1h"},
		{in: 720 * time.Hour, want: "720h"},
		{in: 2160 * time.Hour, want: "2160h"},
		{in: 90 * time.Minute, want: "90m"},
		{in: time.Hour + time.Second, want: "3601s"},
	} {
		t.Run(c.want, func(t *testing.T) {
			got := formatSessionDuration(c.in)
			if got != c.want {
				t.Errorf("formatSessionDuration(%v): got = %q, want = %q", c.in, got, c.want)
			}
			// The canonical form must parse back to the same length, or a
			// value read back from the API would never settle.
			back, err := parseSessionDuration(got)
			if err != nil {
				t.Fatalf("parseSessionDuration(%q): %v", got, err)
			}
			if back != c.in {
				t.Errorf("round trip of %v: got = %v", c.in, back)
			}
		})
	}
}

func TestSessionDurationState(t *testing.T) {
	for _, c := range []struct {
		name    string
		current types.String
		got     *durationpb.Duration
		want    types.String
	}{{
		name:    "same length in days is kept as written",
		current: types.StringValue("30d"),
		got:     durationpb.New(720 * time.Hour),
		want:    types.StringValue("30d"),
	}, {
		name:    "same length in hours is kept as written",
		current: types.StringValue("720h"),
		got:     durationpb.New(720 * time.Hour),
		want:    types.StringValue("720h"),
	}, {
		name:    "same length in minutes is kept as written",
		current: types.StringValue("43200m"),
		got:     durationpb.New(720 * time.Hour),
		want:    types.StringValue("43200m"),
	}, {
		name:    "changed outside terraform is canonical",
		current: types.StringValue("30d"),
		got:     durationpb.New(168 * time.Hour),
		want:    types.StringValue("168h"),
	}, {
		name:    "import has no current value",
		current: types.StringNull(),
		got:     durationpb.New(720 * time.Hour),
		want:    types.StringValue("720h"),
	}, {
		name:    "unset is null",
		current: types.StringValue("720h"),
		got:     nil,
		want:    types.StringNull(),
	}, {
		name:    "zero set by chainguard is shown, not hidden",
		current: types.StringValue("720h"),
		got:     durationpb.New(0),
		want:    types.StringValue("0h"),
	}} {
		t.Run(c.name, func(t *testing.T) {
			got := sessionDurationState(c.current, c.got)
			if !got.Equal(c.want) {
				t.Errorf("sessionDurationState(%v, %v): got = %v, want = %v", c.current, c.got, got, c.want)
			}
		})
	}
}

func TestValidateRootUIDP(t *testing.T) {
	for _, c := range []struct {
		in      string
		wantErr bool
	}{
		{in: "ef127a7c0909329f04b43d845cf80eea4247a07b"},
		{in: "ef127a7c0909329f04b43d845cf80eea4247a07b/66305475446bbef6", wantErr: true},
	} {
		t.Run(c.in, func(t *testing.T) {
			if err := validateRootUIDP(c.in); (err != nil) != c.wantErr {
				t.Errorf("validateRootUIDP(%q): got = %v, wantErr = %t", c.in, err, c.wantErr)
			}
		})
	}
}

func TestAuthPolicyResource_updateSessionDuration(t *testing.T) {
	const org = "ef127a7c0909329f04b43d845cf80eea4247a07b"
	mask := &fieldmaskpb.FieldMask{Paths: []string{"session_duration"}}
	for _, c := range []struct {
		name string
		d    *durationpb.Duration
	}{
		{name: "set", d: durationpb.New(720 * time.Hour)},
		// Delete clears the length: the field is named in the mask with no
		// value, which the API reads as Chainguard's default.
		{name: "clear to default", d: nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			want := &iamv2.AuthPolicy{Uid: org, SessionDuration: c.d}
			r := &authPolicyResource{managedResource{prov: &providerData{
				clientV2GA: &mockV2GAClients{iamClients: &iamv2test.MockClients{
					AuthPoliciesServiceClient: iamv2test.MockAuthPoliciesServiceClient{
						T: t,
						OnUpdateAuthPolicy: []test.On[*iamv2.UpdateAuthPolicyRequest, *iamv2.AuthPolicy]{{
							Given:  &iamv2.UpdateAuthPolicyRequest{AuthPolicy: want, UpdateMask: mask},
							Result: want,
						}},
					},
				}},
			}}}
			got, err := r.updateSessionDuration(t.Context(), org, c.d)
			if err != nil {
				t.Fatalf("updateSessionDuration: %v", err)
			}
			if diff := cmp.Diff(want.GetSessionDuration().AsDuration(), got.GetSessionDuration().AsDuration()); diff != "" {
				t.Errorf("session duration (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestAccResourceAuthPolicy(t *testing.T) {
	// Auth policies belong to root, verified organizations.
	// TF_ACC_GROUP_ID may be a child group; skip if so.
	parentID := os.Getenv(EnvAccGroupID)
	if strings.Contains(parentID, "/") {
		t.Skip("TF_ACC_GROUP_ID is not a root group; auth policy tests require a root organization group")
	}
	clients := testAccV2GAClient(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkAuthPolicyDestroy(clients),
		Steps: []resource.TestStep{
			{
				Config: testAccResourceAuthPolicy(parentID, "720h"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(`chainguard_auth_policy.example`, `id`, parentID),
					resource.TestCheckResourceAttr(`chainguard_auth_policy.example`, `parent_id`, parentID),
					resource.TestCheckResourceAttr(`chainguard_auth_policy.example`, `session_duration`, "720h"),
				),
			},
			// ImportState testing.
			{
				ResourceName:      "chainguard_auth_policy.example",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// The same length in days. The test framework fails the step if
			// the plan after apply is not empty, which catches a perpetual
			// diff between "30d" and the stored 720h.
			{
				Config: testAccResourceAuthPolicy(parentID, "30d"),
				Check:  resource.TestCheckResourceAttr(`chainguard_auth_policy.example`, `session_duration`, "30d"),
			},
			{
				Config: testAccResourceAuthPolicy(parentID, "7d"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(`chainguard_auth_policy.example`, `id`, parentID),
					resource.TestCheckResourceAttr(`chainguard_auth_policy.example`, `session_duration`, "7d"),
				),
			},
		},
	})
}

func testAccResourceAuthPolicy(parentID, sessionDuration string) string {
	const tmpl = `
resource "chainguard_auth_policy" "example" {
  parent_id        = %q
  session_duration = %q
}
`
	return fmt.Sprintf(tmpl, parentID, sessionDuration)
}
