/*
Copyright 2026 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"testing"

	platform "chainguard.dev/sdk/proto/platform"
)

// TestSetupClient_DoubleCheckSkipsWhenSet verifies that setupClient is a
// no-op when the client is already initialized (the double-check pattern).
func TestSetupClient_DoubleCheckSkipsWhenSet(t *testing.T) {
	fake := &fakeClients{}
	pd := &providerData{
		client: fake, // pre-set
	}

	// setupClient should return nil without changing the client.
	if err := pd.setupClient(t.Context()); err != nil {
		t.Fatalf("setupClient with pre-set client: %v", err)
	}
	if pd.client != fake {
		t.Fatal("client should not have been replaced")
	}
}

// fakeClients satisfies platform.Clients for testing.
type fakeClients struct {
	platform.Clients
}

func (f *fakeClients) Close() error { return nil }
