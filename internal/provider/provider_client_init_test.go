/*
Copyright 2026 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"sync"
	"testing"

	platform "chainguard.dev/sdk/proto/platform"
)

// TestSetClient_ThreadSafe verifies that setClient and the double-check
// in setupClient work correctly under concurrent access.
func TestSetClient_ThreadSafe(t *testing.T) {
	pd := &providerData{}

	// Initially nil.
	if pd.client != nil {
		t.Fatal("client should be nil initially")
	}

	// setClient sets the client under the mutex.
	fake := &fakeClients{}
	pd.setClient(fake)
	if pd.client != fake {
		t.Fatal("client should be set after setClient")
	}

	// setClient can replace the client.
	fake2 := &fakeClients{}
	pd.setClient(fake2)
	if pd.client != fake2 {
		t.Fatal("client should be replaced after second setClient")
	}
}

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

// TestSetClient_ConcurrentAccess verifies that concurrent setClient calls
// don't race.
func TestSetClient_ConcurrentAccess(t *testing.T) {
	pd := &providerData{}

	var wg sync.WaitGroup
	for i := range 10 {
		wg.Go(func() {
			pd.setClient(&fakeClients{id: i})
		})
	}
	wg.Wait()

	// After all goroutines complete, client should be non-nil (last writer wins).
	if pd.client == nil {
		t.Fatal("client should be non-nil after concurrent setClient calls")
	}
}

// fakeClients satisfies platform.Clients for testing.
type fakeClients struct {
	platform.Clients
	id int
}

func (f *fakeClients) Close() error { return nil }
