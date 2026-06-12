/*
Copyright 2026 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"chainguard.dev/sdk/proto/platform"
	iam "chainguard.dev/sdk/proto/platform/iam/v1"
)

// --- minimal stateful client fakes -----------------------------------------
//
// MockPlatformClients is concrete down to the Identities client, so it can't
// return different responses across successive Lookup calls. These embed the
// SDK interfaces (nil) and override only the methods Read exercises, so the
// Lookup sequence is fully controllable.

type lookupResult struct {
	id  *iam.Identity
	err error
}

type sequenceIdentities struct {
	iam.IdentitiesClient
	results []lookupResult
	calls   int
}

func (s *sequenceIdentities) Lookup(_ context.Context, _ *iam.LookupRequest, _ ...grpc.CallOption) (*iam.Identity, error) {
	i := s.calls
	if i >= len(s.results) {
		i = len(s.results) - 1 // repeat the last result once exhausted
	}
	s.calls++
	return s.results[i].id, s.results[i].err
}

type retryFakeIAM struct {
	iam.Clients
	identities iam.IdentitiesClient
}

func (f retryFakeIAM) Identities() iam.IdentitiesClient { return f.identities }

type retryFakeClients struct {
	platform.Clients
	iamClients iam.Clients
}

func (f retryFakeClients) IAM() iam.Clients { return f.iamClients }

// readIdentity drives identityDataSource.Read with the given client and a
// fixed valid config, returning the populated state model, the diagnostics,
// and the number of Lookup calls made.
func readIdentity(t *testing.T, ids *sequenceIdentities) (identityDataSourceModel, datasource.ReadResponse) {
	t.Helper()
	ctx := context.Background()

	d := &identityDataSource{}
	d.prov = &providerData{
		client: retryFakeClients{iamClients: retryFakeIAM{identities: ids}},
	}

	var sresp datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &sresp)
	sch := sresp.Schema

	objType := sch.Type().TerraformType(ctx)
	raw := tftypes.NewValue(objType, map[string]tftypes.Value{
		"id":      tftypes.NewValue(tftypes.String, nil),
		"issuer":  tftypes.NewValue(tftypes.String, "https://accounts.google.com"),
		"subject": tftypes.NewValue(tftypes.String, "test-subject"),
	})

	req := datasource.ReadRequest{Config: tfsdk.Config{Schema: sch, Raw: raw}}
	resp := datasource.ReadResponse{State: tfsdk.State{Schema: sch}}
	d.Read(ctx, req, &resp)

	var out identityDataSourceModel
	resp.State.Get(ctx, &out)
	return out, resp
}

func TestIdentityDataSourceRead_Retry(t *testing.T) {
	// No real sleeps between retries.
	retryBaseDelay = 0
	t.Cleanup(func() { retryBaseDelay = 2 * time.Second })

	t.Run("retries transient 5xx then succeeds", func(t *testing.T) {
		ids := &sequenceIdentities{results: []lookupResult{
			{err: status.Error(codes.Unknown, "<html>500 Server Error</html>")},
			{id: &iam.Identity{Id: "foo/identity-123"}},
		}}
		out, resp := readIdentity(t, ids)

		if resp.Diagnostics.HasError() {
			t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
		}
		if out.ID.ValueString() != "foo/identity-123" {
			t.Fatalf("id = %q, want foo/identity-123", out.ID.ValueString())
		}
		if ids.calls != 2 {
			t.Fatalf("Lookup calls = %d, want 2 (one retry)", ids.calls)
		}
	})

	t.Run("does not retry NotFound", func(t *testing.T) {
		ids := &sequenceIdentities{results: []lookupResult{
			{err: status.Error(codes.NotFound, "no such identity")},
		}}
		_, resp := readIdentity(t, ids)

		if !resp.Diagnostics.HasError() {
			t.Fatal("expected an error diagnostic for NotFound")
		}
		if got := resp.Diagnostics.Errors()[0].Summary(); !strings.Contains(got, "not found") {
			t.Fatalf("diagnostic summary = %q, want it to mention 'not found'", got)
		}
		if ids.calls != 1 {
			t.Fatalf("Lookup calls = %d, want 1 (NotFound must not be retried)", ids.calls)
		}
	})

	t.Run("exhausts retries on persistent transient error", func(t *testing.T) {
		ids := &sequenceIdentities{results: []lookupResult{
			{err: status.Error(codes.Unavailable, "still down")},
		}}
		_, resp := readIdentity(t, ids)

		if !resp.Diagnostics.HasError() {
			t.Fatal("expected an error diagnostic after exhausting retries")
		}
		if ids.calls != retryMaxAttempts {
			t.Fatalf("Lookup calls = %d, want %d", ids.calls, retryMaxAttempts)
		}
	})
}
