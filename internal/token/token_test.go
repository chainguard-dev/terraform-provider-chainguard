/*
Copyright 2026 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package token

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"testing"

	sdktoken "chainguard.dev/sdk/auth/token"
	"github.com/sigstore/cosign/v2/pkg/providers"
)

// fakeAccessToken builds an unsigned JWT with the given sub claim.
func fakeAccessToken(sub string) string {
	payload := base64.RawURLEncoding.EncodeToString(fmt.Appendf(nil, `{"sub":%q}`, sub))
	return "header." + payload + ".sig"
}

// fakeProvider is a controllable ambient OIDC provider for tests.
type fakeProvider struct {
	enabled bool
	token   string
	err     error
}

func (f *fakeProvider) Enabled(context.Context) bool { return f.enabled }

func (f *fakeProvider) Provide(context.Context, string) (string, error) {
	return f.token, f.err
}

var testProvider = &fakeProvider{}

func init() {
	providers.Register("terraform-provider-chainguard-test", testProvider)
}

// TestIdentityTokenForExchange guards the regression: on refresh, an ambient
// token must be re-minted, not reused from the (possibly expired) one captured
// at provider configuration time.
func TestIdentityTokenForExchange(t *testing.T) {
	const stale = "stale-captured-token"

	// The real github-actions provider keys off these; clear them so only the
	// fake provider decides whether ambient credentials are enabled.
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", "")

	tests := map[string]struct {
		envToken      string // TF_CHAINGUARD_IDENTITY_TOKEN
		enabled       bool
		minted        string
		mintErr       error
		identityToken string
		want          string
	}{
		"explicit env token wins, not re-minted": {
			envToken:      "explicit-env-token",
			enabled:       true,
			minted:        "fresh-ambient-token",
			identityToken: "explicit-env-token",
			want:          "explicit-env-token",
		},
		"ambient re-mints a fresh token": {
			enabled:       true,
			minted:        "fresh-ambient-token",
			identityToken: stale,
			want:          "fresh-ambient-token", // pre-fix returned stale -> expired-token failure
		},
		"ambient mint failure falls back to configured token": {
			enabled:       true,
			mintErr:       errors.New("oidc endpoint unavailable"),
			identityToken: stale,
			want:          stale,
		},
		"non-ambient returns configured literal/path token unchanged": {
			enabled:       false,
			minted:        "should-not-be-used",
			identityToken: "literal-or-path-token",
			want:          "literal-or-path-token",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Setenv("TF_CHAINGUARD_IDENTITY_TOKEN", tc.envToken)
			testProvider.enabled = tc.enabled
			testProvider.token = tc.minted
			testProvider.err = tc.mintErr

			cfg := LoginConfig{
				IdentityToken: tc.identityToken,
				Issuer:        "https://issuer.example.test",
			}
			if got := identityTokenForExchange(context.Background(), cfg); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestEffectiveExchangeIdentity verifies the refresh target: an explicit pin wins,
// otherwise the identity is derived from the current token's sub (else untargeted).
func TestEffectiveExchangeIdentity(t *testing.T) {
	const audience = "https://console.example.test"

	tests := map[string]struct {
		identityID string // explicit pin
		saveRaw    string // token to write to the cache ("" => none)
		want       string
	}{
		"pin wins without reading cache":   {identityID: "group/pin", saveRaw: fakeAccessToken("group/other"), want: "group/pin"},
		"derive from current token sub":    {saveRaw: fakeAccessToken("group/derived"), want: "group/derived"},
		"no token stays untargeted":        {want: ""},
		"malformed token stays untargeted": {saveRaw: "not-a-jwt", want: ""},
		"empty sub stays untargeted":       {saveRaw: fakeAccessToken(""), want: ""},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			if tc.saveRaw != "" {
				if err := sdktoken.Save([]byte(tc.saveRaw), sdktoken.KindAccess, audience); err != nil {
					t.Fatalf("save token: %v", err)
				}
			}
			cfg := LoginConfig{IdentityID: tc.identityID, Audience: audience}
			if got := effectiveExchangeIdentity(context.Background(), cfg); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestExchangeAmbient verifies targeting and the monotonic fallback: a derived
// identity retries untargeted on failure, while an explicit pin does not.
func TestExchangeAmbient(t *testing.T) {
	const audience = "https://console.example.test"
	// Force identityTokenForExchange down the non-ambient path (returns cfg.IdentityToken).
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", "")
	t.Setenv("TF_CHAINGUARD_IDENTITY_TOKEN", "")
	testProvider.enabled = false

	orig := stsExchange
	t.Cleanup(func() { stsExchange = orig })

	tests := map[string]struct {
		identityID string   // explicit pin
		saveSub    string   // cached token sub ("" => no token)
		failOn     []string // identityIDs the exchange should reject
		wantCalls  []string
		wantTok    string
		wantErr    bool
	}{
		"derived identity succeeds":          {saveSub: "group/derived", wantCalls: []string{"group/derived"}, wantTok: "ok"},
		"derived failure retries untargeted": {saveSub: "group/derived", failOn: []string{"group/derived"}, wantCalls: []string{"group/derived", ""}, wantTok: "ok"},
		"pin failure does not fall back":     {identityID: "group/pin", failOn: []string{"group/pin"}, wantCalls: []string{"group/pin"}, wantErr: true},
		"no token exchanges untargeted":      {wantCalls: []string{""}, wantTok: "ok"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			if tc.saveSub != "" {
				if err := sdktoken.Save([]byte(fakeAccessToken(tc.saveSub)), sdktoken.KindAccess, audience); err != nil {
					t.Fatalf("save token: %v", err)
				}
			}
			var calls []string
			stsExchange = func(_ context.Context, _ LoginConfig, _, identityID string) (string, error) {
				calls = append(calls, identityID)
				if slices.Contains(tc.failOn, identityID) {
					return "", errors.New("exchange failed")
				}
				return "ok", nil
			}

			cfg := LoginConfig{
				IdentityID:    tc.identityID,
				IdentityToken: "oidc-literal",
				Issuer:        "https://issuer.example.test",
				Audience:      audience,
			}
			tok, err := exchangeAmbient(context.Background(), cfg)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v, wantErr=%v", err, tc.wantErr)
			}
			if !tc.wantErr && tok != tc.wantTok {
				t.Errorf("tok=%q, want %q", tok, tc.wantTok)
			}
			if !slices.Equal(calls, tc.wantCalls) {
				t.Errorf("calls=%v, want %v", calls, tc.wantCalls)
			}
		})
	}
}
