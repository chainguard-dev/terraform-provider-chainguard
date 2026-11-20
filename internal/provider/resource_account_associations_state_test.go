/*
Copyright 2026 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"math/rand/v2"
	"testing"

	iamv2 "chainguard.dev/sdk/proto/chainguard/platform/iam/v2beta1"
	iamv2test "chainguard.dev/sdk/proto/chainguard/platform/iam/v2beta1/test"
	"chainguard.dev/sdk/proto/chainguard/platform/test"
	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

type accountAssociationTestClient struct {
	iamv2test.MockAccountAssociationsServiceClient
	updates int
}

func (c *accountAssociationTestClient) UpdateAccountAssociation(ctx context.Context, req *iamv2.UpdateAccountAssociationRequest, opts ...grpc.CallOption) (*iamv2.AccountAssociation, error) {
	c.updates++
	return c.MockAccountAssociationsServiceClient.UpdateAccountAssociation(ctx, req, opts...)
}

type accountAssociationTestIAM struct {
	iamv2.Clients
	associations iamv2.AccountAssociationsServiceClient
}

func (c accountAssociationTestIAM) AccountAssociationsService() iamv2.AccountAssociationsServiceClient {
	return c.associations
}

func accountAssociationTestState(t *testing.T, r *accountAssociationsResource, values map[string]any) tfsdk.State {
	t.Helper()
	var resp resource.SchemaResponse
	r.Schema(t.Context(), resource.SchemaRequest{}, &resp)
	objectType := resp.Schema.Type().TerraformType(t.Context())
	data := make(map[string]any, len(resp.Schema.Attributes)+len(resp.Schema.Blocks))
	for name := range resp.Schema.Attributes {
		data[name] = nil
	}
	for name := range resp.Schema.Blocks {
		data[name] = nil
	}
	maps.Copy(data, values)
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := (tfprotov6.DynamicValue{JSON: encoded}).Unmarshal(objectType)
	if err != nil {
		t.Fatal(err)
	}
	return tfsdk.State{Schema: resp.Schema, Raw: raw}
}

func accountAssociationTestResource(t *testing.T, client iamv2test.MockAccountAssociationsServiceClient) *accountAssociationsResource {
	t.Helper()
	client.T = t
	observed := &accountAssociationTestClient{MockAccountAssociationsServiceClient: client}
	t.Cleanup(func() {
		if got, want := observed.updates, len(client.OnUpdateAccountAssociation); got != want {
			t.Errorf("UpdateAccountAssociation calls: got = %d, want = %d", got, want)
		}
	})
	return &accountAssociationsResource{
		managedResource: managedResource{
			prov: &providerData{
				clientV2: &mockV2PlatformClients{
					iamClients: accountAssociationTestIAM{associations: observed},
				},
			},
		},
	}
}

func accountAssociationTestGithubPlan(t *testing.T, plan *tfsdk.Plan, state tfsdk.State) {
	t.Helper()
	for _, field := range []string{"host", "app_id"} {
		attributePath := path.Root("github").AtName(field)
		attribute, diags := plan.Schema.AttributeAtPath(t.Context(), attributePath)
		if diags.HasError() {
			t.Fatal(diags)
		}
		switch attribute := attribute.(type) {
		case schema.StringAttribute:
			var previous types.String
			if diags := state.GetAttribute(t.Context(), attributePath, &previous); diags.HasError() {
				t.Fatal(diags)
			}
			if diags := plan.SetAttribute(t.Context(), attributePath, types.StringUnknown()); diags.HasError() {
				t.Fatal(diags)
			}
			resp := planmodifier.StringResponse{PlanValue: types.StringUnknown()}
			for _, modifier := range attribute.PlanModifiers {
				modifier.PlanModifyString(t.Context(), planmodifier.StringRequest{
					Path: attributePath, Plan: *plan, State: state,
					ConfigValue: types.StringNull(), PlanValue: resp.PlanValue, StateValue: previous,
				}, &resp)
			}
			if resp.Diagnostics.HasError() {
				t.Fatal(resp.Diagnostics)
			}
			if diags := plan.SetAttribute(t.Context(), attributePath, resp.PlanValue); diags.HasError() {
				t.Fatal(diags)
			}
		case schema.Int64Attribute:
			var previous types.Int64
			if diags := state.GetAttribute(t.Context(), attributePath, &previous); diags.HasError() {
				t.Fatal(diags)
			}
			if diags := plan.SetAttribute(t.Context(), attributePath, types.Int64Unknown()); diags.HasError() {
				t.Fatal(diags)
			}
			resp := planmodifier.Int64Response{PlanValue: types.Int64Unknown()}
			for _, modifier := range attribute.PlanModifiers {
				modifier.PlanModifyInt64(t.Context(), planmodifier.Int64Request{
					Path: attributePath, Plan: *plan, State: state,
					ConfigValue: types.Int64Null(), PlanValue: resp.PlanValue, StateValue: previous,
				}, &resp)
			}
			if resp.Diagnostics.HasError() {
				t.Fatal(resp.Diagnostics)
			}
			if diags := plan.SetAttribute(t.Context(), attributePath, resp.PlanValue); diags.HasError() {
				t.Fatal(diags)
			}
		default:
			t.Fatalf("attribute type: got = %T, want = string or int64", attribute)
		}
	}
}

func TestAccountAssociationsReadServiceBindings(t *testing.T) {
	identity := fmt.Sprintf("identity-%d", rand.Uint64())
	for _, tt := range []struct {
		name          string
		state, remote map[string]string
	}{{
		name:   "added binding",
		state:  map[string]string{"INGESTER": identity},
		remote: map[string]string{"INGESTER": identity, "COSIGNED": identity + "-cosigned"},
	}, {
		name:   "added to empty map",
		state:  map[string]string{},
		remote: map[string]string{"INGESTER": identity},
	}, {
		name:   "changed binding",
		state:  map[string]string{"INGESTER": identity},
		remote: map[string]string{"INGESTER": identity + "-changed"},
	}, {
		name:   "removed binding",
		state:  map[string]string{"INGESTER": identity, "COSIGNED": identity + "-cosigned"},
		remote: map[string]string{"INGESTER": identity},
	}, {
		name:   "unchanged bindings",
		state:  map[string]string{"INGESTER": identity},
		remote: map[string]string{"INGESTER": identity},
	}} {
		t.Run(tt.name, func(t *testing.T) {
			r := accountAssociationTestResource(t, iamv2test.MockAccountAssociationsServiceClient{
				OnGetAccountAssociation: []test.On[*iamv2.GetAccountAssociationRequest, *iamv2.AccountAssociation]{{
					Given: &iamv2.GetAccountAssociationRequest{Uid: "group"},
					Result: &iamv2.AccountAssociation{
						Uid: "group", Name: "association",
						Chainguard: &iamv2.AccountAssociation_Chainguard{ServiceBindings: tt.remote},
					},
				}},
			})
			state := accountAssociationTestState(t, r, map[string]any{
				"id": "group", "group": "group", "name": "association",
				"chainguard": map[string]any{"service_bindings": tt.state},
			})
			resp := resource.ReadResponse{State: state}
			r.Read(t.Context(), resource.ReadRequest{State: state}, &resp)
			if resp.Diagnostics.HasError() {
				t.Fatalf("Read diagnostics: got = %v, want = none", resp.Diagnostics)
			}
			var bindings types.Map
			if diags := resp.State.GetAttribute(t.Context(), path.Root("chainguard").AtName("service_bindings"), &bindings); diags.HasError() {
				t.Fatal(diags)
			}
			var got map[string]string
			if diags := bindings.ElementsAs(t.Context(), &got, false); diags.HasError() {
				t.Fatal(diags)
			}
			if diff := cmp.Diff(tt.remote, got); diff != "" {
				t.Errorf("service bindings (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestAccountAssociationsUpdateMask(t *testing.T) {
	identity := fmt.Sprintf("identity-%d", rand.Uint64())
	google := map[string]any{"project_id": "project", "project_number": "123456"}
	github := map[string]any{"host": "", "app_id": 123, "installation_id": 456, "name": "org"}
	installations := []map[string]any{{"app_id": 123, "installation_id": 456, "name": "org"}}
	githubAssociation := &iamv2.AccountAssociation_GitHub{
		AppInstallations: map[int64]*iamv2.AccountAssociation_GitHubAppInstallations{
			123: {Installations: []*iamv2.AccountAssociation_GitHubInstallation{{InstallationId: 456, Name: "org"}}},
		},
	}
	for _, tt := range []struct {
		name            string
		before, after   map[string]any
		unknownComputed bool
		mask            []string
		want            *iamv2.AccountAssociation
	}{{
		name:  "google addition preserves service bindings",
		after: map[string]any{"google": google},
		mask:  []string{"google"},
		want:  &iamv2.AccountAssociation{Google: &iamv2.AccountAssociation_Google{ProjectId: "project", ProjectNumber: "123456"}},
	}, {
		name:   "google change preserves service bindings",
		before: map[string]any{"google": map[string]any{"project_id": "old-project", "project_number": "654321"}},
		after:  map[string]any{"google": google},
		mask:   []string{"google"},
		want:   &iamv2.AccountAssociation{Google: &iamv2.AccountAssociation_Google{ProjectId: "project", ProjectNumber: "123456"}},
	}, {
		name:   "google removal",
		before: map[string]any{"google": google},
		mask:   []string{"google"},
		want:   &iamv2.AccountAssociation{},
	}, {
		name:  "name and description change",
		after: map[string]any{"name": "renamed", "description": "description"},
		mask:  []string{"name", "description"},
		want:  &iamv2.AccountAssociation{Name: "renamed", Description: "description"},
	}, {
		name:   "description removal",
		before: map[string]any{"description": "description"},
		mask:   []string{"description"},
		want:   &iamv2.AccountAssociation{},
	}, {
		name:   "amazon removal and azure addition",
		before: map[string]any{"amazon": map[string]any{"account": "123456789012"}},
		after:  map[string]any{"azure": map[string]any{"tenant_id": "tenant", "client_ids": map[string]string{"service": "client"}}},
		mask:   []string{"amazon", "azure"},
		want:   &iamv2.AccountAssociation{Azure: &iamv2.AccountAssociation_Azure{TenantId: "tenant", ClientIds: map[string]string{"service": "client"}}},
	}, {
		name:  "service bindings change",
		after: map[string]any{"chainguard": map[string]any{"service_bindings": map[string]string{"COSIGNED": identity + "-cosigned"}}},
		mask:  []string{"chainguard"},
		want:  &iamv2.AccountAssociation{Chainguard: &iamv2.AccountAssociation_Chainguard{ServiceBindings: map[string]string{"COSIGNED": identity + "-cosigned"}}},
	}, {
		name:  "service bindings removal",
		after: map[string]any{"chainguard": nil, "google": google},
		mask:  []string{"google", "chainguard"},
		want:  &iamv2.AccountAssociation{Google: &iamv2.AccountAssociation_Google{ProjectId: "project", ProjectNumber: "123456"}},
	}, {
		name:  "deprecated github addition",
		after: map[string]any{"github": github},
		mask:  []string{"github"},
		want:  &iamv2.AccountAssociation{Github: githubAssociation},
	}, {
		name:   "deprecated github removal",
		before: map[string]any{"github": github},
		mask:   []string{"github"},
		want:   &iamv2.AccountAssociation{},
	}, {
		name:  "github installations addition",
		after: map[string]any{"github_installation": installations},
		mask:  []string{"github"},
		want:  &iamv2.AccountAssociation{Github: githubAssociation},
	}, {
		name:   "google addition preserves deprecated github",
		before: map[string]any{"github": github},
		after:  map[string]any{"google": google, "github": github},
		mask:   []string{"google"},
		want: &iamv2.AccountAssociation{
			Google: &iamv2.AccountAssociation_Google{ProjectId: "project", ProjectNumber: "123456"},
			Github: githubAssociation,
		},
	}, {
		name:            "unknown github computed fields do not mask github on google addition",
		before:          map[string]any{"github": github},
		after:           map[string]any{"google": google, "github": github},
		unknownComputed: true,
		mask:            []string{"google"},
		want: &iamv2.AccountAssociation{
			Google: &iamv2.AccountAssociation_Google{ProjectId: "project", ProjectNumber: "123456"},
			Github: githubAssociation,
		},
	}, {
		name:            "unknown github computed fields preserve an installation change",
		before:          map[string]any{"github": map[string]any{"host": "", "app_id": 123, "installation_id": 789, "name": "other-org"}},
		after:           map[string]any{"github": github},
		unknownComputed: true,
		mask:            []string{"github"},
		want:            &iamv2.AccountAssociation{Github: githubAssociation},
	}, {
		name:            "unknown github computed fields alone do not call update",
		before:          map[string]any{"github": github},
		after:           map[string]any{"github": github},
		unknownComputed: true,
		want:            &iamv2.AccountAssociation{Github: githubAssociation},
	}, {
		name:   "github installations removal",
		before: map[string]any{"github_installation": installations},
		mask:   []string{"github"},
		want:   &iamv2.AccountAssociation{},
	}, {
		name:   "github installations change",
		before: map[string]any{"github_installation": []map[string]any{{"app_id": 123, "installation_id": 789, "name": "other-org"}}},
		after:  map[string]any{"github_installation": installations},
		mask:   []string{"github"},
		want:   &iamv2.AccountAssociation{Github: githubAssociation},
	}, {
		name:   "github representation migration masks github once",
		before: map[string]any{"github": github},
		after:  map[string]any{"github_installation": installations},
		mask:   []string{"github"},
		want:   &iamv2.AccountAssociation{Github: githubAssociation},
	}, {
		name: "unchanged plan does not call update",
		want: &iamv2.AccountAssociation{},
	}} {
		t.Run(tt.name, func(t *testing.T) {
			before := map[string]any{
				"id": "group", "group": "group", "name": "association",
				"chainguard": map[string]any{"service_bindings": map[string]string{"INGESTER": identity}},
			}
			after := make(map[string]any, len(before)+len(tt.after))
			maps.Copy(after, before)
			maps.Copy(before, tt.before)
			maps.Copy(after, tt.after)
			tt.want.Uid = "group"
			if tt.want.Name == "" {
				tt.want.Name = "association"
			}
			if _, changed := tt.after["chainguard"]; !changed {
				tt.want.Chainguard = &iamv2.AccountAssociation_Chainguard{ServiceBindings: map[string]string{"INGESTER": identity}}
			}
			client := iamv2test.MockAccountAssociationsServiceClient{}
			if len(tt.mask) > 0 {
				client.OnUpdateAccountAssociation = []test.On[*iamv2.UpdateAccountAssociationRequest, *iamv2.AccountAssociation]{{
					Given: &iamv2.UpdateAccountAssociationRequest{
						AccountAssociation: tt.want,
						UpdateMask:         &fieldmaskpb.FieldMask{Paths: tt.mask},
					},
					Result: tt.want,
				}}
			}
			r := accountAssociationTestResource(t, client)
			state := accountAssociationTestState(t, r, before)
			plan := tfsdk.Plan(accountAssociationTestState(t, r, after))
			wantState := plan
			if tt.unknownComputed {
				accountAssociationTestGithubPlan(t, &plan, state)
			}
			resp := resource.UpdateResponse{State: state}
			r.Update(t.Context(), resource.UpdateRequest{
				Plan: plan, State: state,
			}, &resp)
			if resp.Diagnostics.HasError() {
				t.Fatalf("Update diagnostics: got = %v, want = none", resp.Diagnostics)
			}
			if !resp.State.Raw.Equal(wantState.Raw) {
				t.Errorf("updated state: got = %s, want = %s", resp.State.Raw, wantState.Raw)
			}
		})
	}
}
