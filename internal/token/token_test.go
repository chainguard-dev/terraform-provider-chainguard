/*
Copyright 2026 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package token

import (
	"context"
	"errors"
	"testing"

	"github.com/sigstore/cosign/v2/pkg/providers"
)

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
