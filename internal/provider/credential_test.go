/*
Copyright 2026 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	sdktoken "chainguard.dev/sdk/auth/token"

	"github.com/chainguard-dev/terraform-provider-chainguard/internal/token"
)

// tokenWithExp builds an unsigned JWT carrying the given sub and exp (unix seconds).
// token.Get / RemainingLife only parse these claims (no signature verification), which
// is what lets this reproducer be fully local and deterministic.
func tokenWithExp(sub string, exp int64) string {
	payload := base64.RawURLEncoding.EncodeToString(fmt.Appendf(nil, `{"sub":%q,"exp":%d}`, sub, exp))
	return "hdr." + payload + ".sig"
}

// TestRefreshingCredentialVsStatic is the minimal reproducer of the release-build
// failure: a client credential captured once at setup keeps emitting a token frozen
// at construction time, so once that token's ~TTL elapses the server rejects it with
// Unauthenticated. The refreshingCredential re-reads via token.Get on every RPC and
// therefore emits whatever the (refreshed) cache currently holds.
//
// We model a 1-minute-TTL token: both credentials emit it initially; then we rotate
// the cache to a fresh token (as token.Get's refresh would do once the first expired)
// and show the static credential is stuck on the stale token while the refreshing one
// picks up the new one. No live issuer or long build required.
func TestRefreshingCredentialVsStatic(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	ctx := context.Background()
	const audience = "https://console.example.test"

	// token.Get refreshes when a cached token has <60s (tokenLifeBuffer) of life left,
	// so a "still-usable" short-lived token must sit just above that floor. We use ~2 min
	// to model a freshly-minted short-TTL token that token.Get will hand back as-is.
	now := time.Now().Unix()
	oneMinuteToken := tokenWithExp("group/session", now+120)  // short TTL, still > 60s buffer
	refreshedToken := tokenWithExp("group/session", now+3600) // what the cache holds after a refresh

	if err := sdktoken.Save([]byte(oneMinuteToken), sdktoken.KindAccess, audience); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	// The buggy path: a credential frozen with the token captured at client construction.
	staticAuthHeader := "Bearer " + oneMinuteToken
	// The fix: a credential that re-fetches from the cache (token.Get) on every RPC.
	refreshing := &refreshingCredential{loginConfig: token.LoginConfig{Audience: audience}}

	// Sanity: before the token rolls over, both emit the same header.
	md, err := refreshing.GetRequestMetadata(ctx)
	if err != nil {
		t.Fatalf("refreshing.GetRequestMetadata: %v", err)
	}
	if md["Authorization"] != staticAuthHeader {
		t.Fatalf("pre-rotate: refreshing emitted %q, want %q", md["Authorization"], staticAuthHeader)
	}

	// The ~1-min token expires and the cache is refreshed to a new token (token.Get would
	// do this on the next call). The static credential cannot see this — it holds a string.
	if err := sdktoken.Save([]byte(refreshedToken), sdktoken.KindAccess, audience); err != nil {
		t.Fatalf("rotate cache: %v", err)
	}

	// FIX verified: the refreshing credential emits the current (refreshed) token.
	md, err = refreshing.GetRequestMetadata(ctx)
	if err != nil {
		t.Fatalf("refreshing.GetRequestMetadata after rotate: %v", err)
	}
	if got, want := md["Authorization"], "Bearer "+refreshedToken; got != want {
		t.Errorf("post-rotate: refreshing emitted %q, want %q (should track the refreshed cache, not the frozen token)", got, want)
	}
	if md["Authorization"] == staticAuthHeader {
		t.Errorf("refreshing credential is stuck on the stale token — fix ineffective")
	}
}
