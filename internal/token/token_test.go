/*
Copyright 2026 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package token

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"chainguard.dev/sdk/auth"
	sdktoken "chainguard.dev/sdk/auth/token"
	"github.com/sigstore/cosign/v2/pkg/providers"
	// Registers the github-actions ambient provider so providers.Enabled works
	// in this package's acc test binary (see TestExchangeAmbientRefreshTargetsSessionIdentity).
	_ "github.com/sigstore/cosign/v2/pkg/providers/github"
)

// Valid UIDP-shaped identities for tests (40-hex group + 16-hex identity),
// matching what the issuer sets as a real token's sub.
const (
	uidpDerived = "1111111111111111111111111111111111111111/1111111111111111"
	uidpOther   = "2222222222222222222222222222222222222222/2222222222222222"
	uidpPin     = "3333333333333333333333333333333333333333/3333333333333333"
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
		"pin wins without reading cache":   {identityID: uidpPin, saveRaw: fakeAccessToken(uidpOther), want: uidpPin},
		"derive from current token sub":    {saveRaw: fakeAccessToken(uidpDerived), want: uidpDerived},
		"no token stays untargeted":        {want: ""},
		"malformed token stays untargeted": {saveRaw: "not-a-jwt", want: ""},
		"empty sub stays untargeted":       {saveRaw: fakeAccessToken(""), want: ""},
		"non-UIDP sub stays untargeted":    {saveRaw: fakeAccessToken("group/derived"), want: ""},
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
		"derived identity succeeds":          {saveSub: uidpDerived, wantCalls: []string{uidpDerived}, wantTok: "ok"},
		"derived failure retries untargeted": {saveSub: uidpDerived, failOn: []string{uidpDerived}, wantCalls: []string{uidpDerived, ""}, wantTok: "ok"},
		"pin failure does not fall back":     {identityID: uidpPin, failOn: []string{uidpPin}, wantCalls: []string{uidpPin}, wantErr: true},
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

// TestExchangeAmbientRefreshTargetsSessionIdentity is an acceptance-gated
// integration test against a live issuer: with NO identity pinned, a forced
// refresh must re-assume the identity the current session already holds. This
// is what lets callers drop a hardcoded identity_id and still refresh in an
// environment where a repo maps to multiple identities.
func TestExchangeAmbientRefreshTargetsSessionIdentity(t *testing.T) {
	for _, v := range []string{"TF_ACC", "TF_ACC_AMBIENT", "TF_ACC_AUDIENCE", "TF_ACC_ISSUER"} {
		if os.Getenv(v) == "" {
			t.Skipf("%s not set; skipping ambient-refresh integration test", v)
		}
	}
	ctx := context.Background()
	// No IdentityID: this is the pin-free path under test.
	cfg := LoginConfig{
		Audience:  os.Getenv("TF_ACC_AUDIENCE"),
		Issuer:    os.Getenv("TF_ACC_ISSUER"),
		UserAgent: "terraform-provider-chainguard/acctest-refresh",
	}

	// Learn the session identity from the token setup-chainctl already cached.
	if _, err := Get(ctx, cfg, false); err != nil {
		t.Fatalf("initial token.Get: %v", err)
	}
	raw, err := sdktoken.Load(sdktoken.KindAccess, cfg.Audience, withAlias(cfg))
	if err != nil {
		t.Fatalf("load cached token: %v", err)
	}
	_, sessionID, err := auth.ExtractIssuerAndSubject(string(raw))
	if err != nil || sessionID == "" {
		t.Fatalf("read session identity: err=%v sub=%q", err, sessionID)
	}

	// Route the next refresh through the ambient OIDC exchange (as Configure would).
	idToken, err := ResolveIdentityToken(ctx, cfg.Issuer, "")
	if err != nil || idToken == "" {
		t.Skipf("no ambient OIDC token available: err=%v", err)
	}
	cfg.IdentityToken = idToken

	// Make the cached token look expired so the next Get refreshes now.
	expireCachedAccessToken(t, cfg)

	// Record the identity each refresh exchange targets (delegate to the real one).
	var calls []string
	orig := stsExchange
	t.Cleanup(func() { stsExchange = orig })
	stsExchange = func(c context.Context, lc LoginConfig, idt, identityID string) (string, error) {
		calls = append(calls, identityID)
		return orig(c, lc, idt, identityID)
	}

	if _, err := Get(ctx, cfg, false); err != nil {
		t.Fatalf("forced refresh token.Get: %v", err)
	}
	if len(calls) == 0 || calls[0] != sessionID {
		t.Fatalf("refresh targeted %v, want session identity %q first (unpinned refresh must re-assume the session identity)", calls, sessionID)
	}
}

// expireCachedAccessToken rewrites the cached access token's exp claim to the
// past, preserving the rest of the payload, so the provider treats it as expired.
func expireCachedAccessToken(t *testing.T, cfg LoginConfig) {
	t.Helper()
	raw, err := sdktoken.Load(sdktoken.KindAccess, cfg.Audience, withAlias(cfg))
	if err != nil {
		t.Fatalf("load token to expire: %v", err)
	}
	parts := strings.Split(string(raw), ".")
	if len(parts) < 2 {
		t.Fatalf("cached token is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode token payload: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("unmarshal claims: %v", err)
	}
	claims["exp"] = time.Now().Add(-time.Hour).Unix()
	nb, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	parts[1] = base64.RawURLEncoding.EncodeToString(nb)
	if err := sdktoken.Save([]byte(strings.Join(parts, ".")), sdktoken.KindAccess, cfg.Audience, withAlias(cfg)); err != nil {
		t.Fatalf("save expired token: %v", err)
	}
}

// fakeRefreshToken builds a refresh token in the shape the SDK's family store
// accepts: base64-encoded JSON claims. nonce distinguishes successive tokens.
func fakeRefreshToken(t *testing.T, issuer, audience, nonce string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"iss":   issuer,
		"aud":   audience,
		"sub":   uidpDerived,
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(time.Hour).Unix(),
		"nonce": nonce,
	})
	if err != nil {
		t.Fatalf("marshal refresh claims: %v", err)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// isolateTokenCache points the SDK token cache at a fresh directory.
// os.UserCacheDir reads XDG_CACHE_HOME on Linux and HOME on macOS.
func isolateTokenCache(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	t.Setenv("HOME", dir)
}

// saveChainctlFamily stores tok the way chainctl stores a rotation-capable
// login: as a family with no per-audience copy.
func saveChainctlFamily(t *testing.T, cfg LoginConfig, tok string) {
	t.Helper()
	if err := sdktoken.SaveFamily([]byte(tok), []string{cfg.Audience}, sdktoken.WithIssuer(cfg.Issuer)); err != nil {
		t.Fatalf("SaveFamily: %v", err)
	}
	// sdktoken.Delete would also remove the audience from the family.
	path, err := sdktoken.Path(sdktoken.KindRefresh, cfg.Audience)
	if err != nil {
		t.Fatalf("per-audience path: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("delete per-audience copy: %v", err)
	}
	if _, err := sdktoken.Load(sdktoken.KindRefresh, cfg.Audience); err == nil {
		t.Fatal("per-audience copy still present")
	}
}

func TestRefreshChainguardTokenFromFamily(t *testing.T) {
	isolateTokenCache(t)
	cfg := LoginConfig{
		Audience:         "https://console-api.example.dev",
		Issuer:           "https://issuer.example.dev",
		UseRefreshTokens: true,
		// Set so a failed refresh falls back to the stubbed exchange below
		// rather than a browser login.
		IdentityToken: "configured-oidc-token",
	}
	parent := fakeRefreshToken(t, cfg.Issuer, cfg.Audience, "parent")
	successor := fakeRefreshToken(t, cfg.Issuer, cfg.Audience, "successor")
	access := fakeAccessToken(uidpDerived)
	saveChainctlFamily(t, cfg, parent)

	origRefresh, origExchange := stsRefresh, stsExchange
	t.Cleanup(func() { stsRefresh, stsExchange = origRefresh, origExchange })
	stsExchange = func(context.Context, LoginConfig, string, string) (string, error) {
		t.Error("refresh fell back to the identity token exchange")
		return "", errors.New("unexpected fallback")
	}
	var presented []string
	stsRefresh = func(_ context.Context, _ LoginConfig, tok string) (string, string, error) {
		presented = append(presented, tok)
		return access, successor, nil
	}

	if err := refreshChainguardToken(t.Context(), cfg, true); err != nil {
		t.Fatalf("refreshChainguardToken: %v", err)
	}
	if want := []string{parent}; !slices.Equal(presented, want) {
		t.Errorf("presented: got = %v, want = %v", presented, want)
	}
	got, err := sdktoken.Load(sdktoken.KindAccess, cfg.Audience, withAlias(cfg))
	if err != nil {
		t.Fatalf("load access token: %v", err)
	}
	if string(got) != access {
		t.Errorf("access token: got = %q, want = %q", got, access)
	}

	// The successor is persisted to the family: the next refresh presents it.
	if err := refreshChainguardToken(t.Context(), cfg, true); err != nil {
		t.Fatalf("second refreshChainguardToken: %v", err)
	}
	if want := []string{parent, successor}; !slices.Equal(presented, want) {
		t.Errorf("presented: got = %v, want = %v", presented, want)
	}
	if _, err := sdktoken.Load(sdktoken.KindRefresh, cfg.Audience); err == nil {
		t.Error("refresh wrote a per-audience copy of the family's token")
	}
}

func TestRefreshChainguardTokenFamilyFailureFallsBack(t *testing.T) {
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", "")
	t.Setenv("TF_CHAINGUARD_IDENTITY_TOKEN", "")
	testProvider.enabled = false

	tests := []struct {
		name       string
		saveFamily bool
		refreshErr error
	}{{
		name: "no refresh token in the store",
	}, {
		name:       "exchange refused",
		saveFamily: true,
		refreshErr: errors.New("refresh refused"),
	}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isolateTokenCache(t)
			cfg := LoginConfig{
				Audience:         "https://console-api.example.dev",
				Issuer:           "https://issuer.example.dev",
				IdentityToken:    "configured-oidc-token",
				UseRefreshTokens: true,
			}
			if tc.saveFamily {
				saveChainctlFamily(t, cfg, fakeRefreshToken(t, cfg.Issuer, cfg.Audience, "parent"))
			}

			origRefresh, origExchange := stsRefresh, stsExchange
			t.Cleanup(func() { stsRefresh, stsExchange = origRefresh, origExchange })
			stsRefresh = func(context.Context, LoginConfig, string) (string, string, error) {
				return "", "", tc.refreshErr
			}
			access := fakeAccessToken(uidpOther)
			var exchanged []string
			stsExchange = func(_ context.Context, _ LoginConfig, idToken, _ string) (string, error) {
				exchanged = append(exchanged, idToken)
				return access, nil
			}

			if err := refreshChainguardToken(t.Context(), cfg, true); err != nil {
				t.Fatalf("refreshChainguardToken: %v", err)
			}
			if want := []string{cfg.IdentityToken}; !slices.Equal(exchanged, want) {
				t.Errorf("identity token exchanges: got = %v, want = %v", exchanged, want)
			}
			got, err := sdktoken.Load(sdktoken.KindAccess, cfg.Audience, withAlias(cfg))
			if err != nil {
				t.Fatalf("load access token: %v", err)
			}
			if string(got) != access {
				t.Errorf("access token: got = %q, want = %q", got, access)
			}
		})
	}
}
