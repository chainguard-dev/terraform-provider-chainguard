/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package token

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/hashicorp/terraform-plugin-log/tflog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"chainguard.dev/sdk/auth/login"
	sdktoken "chainguard.dev/sdk/auth/token"
	"chainguard.dev/sdk/sts"
)

const (
	// auth0ClientID is the oauth2 clientID to indicate to the issuer to
	// authenticate with Auth0.
	auth0ClientID = "auth0"

	// tokenLifeBuffer is the amount of remaining life required to use
	// a token before attempting to generate a new one.
	tokenLifeBuffer = time.Minute
)

var lock sync.RWMutex

// Get retrieves a Chainguard token, refreshing it if expired/non-existent or forceRefresh == true.
// If automatic authentication is disabled, returns an unauthenticated error.
func Get(ctx context.Context, cfg LoginConfig, forceRefresh bool) ([]byte, error) {
	// Get the remaining life of the current token.
	lock.RLock()
	life := sdktoken.RemainingLife(sdktoken.KindAccess, cfg.Audience, tokenLifeBuffer)
	lock.RUnlock()

	// If token is expired or not found, or we're forcing a refresh, login and save a new one.
	if life <= 0 || forceRefresh {
		err := refreshChainguardToken(ctx, cfg, life)
		if err != nil {
			return nil, err
		}
	}

	lock.RLock()
	defer lock.RUnlock()
	return sdktoken.Load(sdktoken.KindAccess, cfg.Audience)
}

// refreshChainguardToken attempts to get a new Chainguard token either through user browser flow,
// or by exchanging a given OIDC token, unless auto-login is disabled.
func refreshChainguardToken(ctx context.Context, cfg LoginConfig, life time.Duration) error {
	// Bail if auto-login is disabled.
	if cfg.Disabled {
		tflog.Info(ctx, "automatic authentication disabled")
		return status.Error(codes.Unauthenticated, "automatic auth disabled")
	}

	// Obtain a write lock since we may be updating the token
	lock.Lock()
	defer lock.Unlock()

	// Check that the token wasn't refreshed by another thread
	if sdktoken.RemainingLife(sdktoken.KindAccess, cfg.Audience, tokenLifeBuffer) > life {
		return nil
	}

	tflog.Info(ctx, "refreshing Chainguard token", map[string]interface{}{
		"UseRefreshTokens": cfg.UseRefreshTokens,
	})
	var (
		accessToken, refreshToken string
		err                       error
	)

	// If configured to use refresh tokens, attempt to exchange it for a new access token.
	if cfg.UseRefreshTokens {
		accessToken, refreshToken, err = exchangeRefreshToken(ctx, cfg)
		if err == nil && accessToken != "" && refreshToken != "" {
			return saveTokens(accessToken, refreshToken, cfg.Audience)
		}
		// If refresh token exchange failed, fall through to login flow
		tflog.Warn(ctx, fmt.Sprintf("failed to exchange refresh token: %s", err.Error()))
	}

	if cfg.IdentityToken != "" {
		accessToken, err = exchangeToken(ctx, cfg.IdentityToken, cfg)
	} else {
		accessToken, refreshToken, err = getChainguardToken(ctx, cfg)
	}
	if err != nil {
		return fmt.Errorf("failed to get Chainguard token: %w", err)
	}

	return saveTokens(accessToken, refreshToken, cfg.Audience)
}

func saveTokens(accessToken, refreshToken, audience string) error {
	if err := sdktoken.Save([]byte(accessToken), sdktoken.KindAccess, audience); err != nil {
		return fmt.Errorf("failed to save Chainguard token: %w", err)
	}
	if refreshToken != "" {
		if err := sdktoken.Save([]byte(refreshToken), sdktoken.KindRefresh, audience); err != nil {
			return fmt.Errorf("failed to save refresh token: %w", err)
		}
	}
	return nil
}

// getChainguardToken gets a Chainguard token by launching a browser and having the user authenticate
// through the configured OIDC identity provider.
func getChainguardToken(ctx context.Context, cfg LoginConfig) (accessToken string, refreshToken string, err error) {
	tflog.Info(ctx, "launching browser login flow")
	loginCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	opts := []login.Option{
		login.WithIssuer(cfg.Issuer),
		login.WithAudience([]string{cfg.Audience}),
		login.WithClientID(auth0ClientID),
		login.WithIdentity(cfg.IdentityID),
		login.WithIdentityProvider(cfg.IdentityProvider),
		login.WithAuth0Connection(cfg.Auth0Connection),
		login.WithOrgName(cfg.OrgName),
	}
	if cfg.UseRefreshTokens {
		opts = append(opts, login.WithCreateRefreshToken())
	}
	return login.Login(loginCtx, opts...)
}

func exchangeRefreshToken(ctx context.Context, cfg LoginConfig) (cgToken string, refreshToken string, err error) {
	tflog.Info(ctx, "exchanging refresh token for access token")
	refreshTokenBytes, err := sdktoken.Load(sdktoken.KindRefresh, cfg.Audience)
	if err != nil {
		return "", "", fmt.Errorf("failed to load refresh token: %w", err)
	}

	e := sts.New(cfg.Issuer, cfg.Audience, sts.WithUserAgent(cfg.UserAgent))
	return e.Refresh(ctx, string(refreshTokenBytes))
}

// exchangeToken gets a Chainguard token by exchanging the given OIDC token or path to a token.
// No user interaction is required. Refresh tokens are not supported in this login flow.
func exchangeToken(ctx context.Context, idToken string, cfg LoginConfig) (string, error) {
	tflog.Info(ctx, "exchanging oidc token for chainguard token")

	// Test to see if identity token is a path or not.
	if _, err := os.Stat(idToken); err == nil {
		// Token was specified, and it is a path. Read the token in from that file.
		b, err := os.ReadFile(idToken)
		if err != nil {
			return "", err
		}
		idToken = string(b)
	}

	opts := []sts.ExchangerOption{
		sts.WithUserAgent(cfg.UserAgent),
		// If IdentityID is empty this is a noop during exchange.
		sts.WithIdentity(cfg.IdentityID),
	}

	tok, err := sts.ExchangePair(ctx, cfg.Issuer, cfg.Audience, idToken, opts...)
	if err != nil {
		return "", err
	}

	return tok.AccessToken, nil
}
