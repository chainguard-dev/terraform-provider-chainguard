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
)

var lock sync.Mutex

// Get retrieves a Chainguard token, refreshing it if expired/non-existent or forceRefresh == true.
// If automatic authentication is disabled, returns an unauthenticated error.
func Get(ctx context.Context, cfg LoginConfig, forceRefresh bool) ([]byte, error) {
	// Lock access to the token so no other process attempts to read or refresh
	// while refreshing the token.
	lock.Lock()
	defer lock.Unlock()

	// If token is expired or not found, login and save a new one.
	if sdktoken.RemainingLife(cfg.Audience, time.Minute) <= 0 || forceRefresh {
		err := refreshChainguardToken(ctx, cfg)
		if err != nil {
			return nil, err
		}
	}

	return sdktoken.Load(cfg.Audience)
}

// refreshChainguardToken attempts to get a new Chainguard token either through user browser flow,
// or by exchanging a given OIDC token, unless auto-login is disabled.
func refreshChainguardToken(ctx context.Context, cfg LoginConfig) error {
	// Bail if auto-login is disabled.
	if cfg.Disabled {
		tflog.Info(ctx, "automatic authentication disabled")
		return status.Error(codes.Unauthenticated, "automatic auth disabled")
	}

	tflog.Info(ctx, "refreshing Chainguard token")
	var (
		err     error
		cgToken string
	)

	if cfg.IdentityToken != "" {
		cgToken, err = exchangeToken(ctx, cfg.IdentityToken, cfg)
	} else {
		cgToken, err = getChainguardToken(ctx, cfg)
	}
	if err != nil {
		return fmt.Errorf("failed to get Chainguard token: %w", err)
	}

	if err = sdktoken.Save([]byte(cgToken), cfg.Audience); err != nil {
		return fmt.Errorf("failed to save Chainguard token: %w", err)
	}
	return nil
}

// getChainguardToken gets a Chainguard token by launching a browser and having the user authenticate
// through the configured OIDC identity provider.
func getChainguardToken(ctx context.Context, cfg LoginConfig) (string, error) {
	tflog.Info(ctx, "launching browser login flow")
	loginCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	// For each option, prefer the environment var, if set.
	return login.Login(loginCtx,
		login.WithIssuer(cfg.Issuer),
		login.WithAudience([]string{cfg.Audience}),
		login.WithClientID(auth0ClientID),
		login.WithIdentity(cfg.IdentityID),
		login.WithIdentityProvider(cfg.IdentityProvider),
		login.WithAuth0Connection(cfg.Auth0Connection),
		login.WithOrgName(cfg.OrgName),
	)
}

// exchangeToken gets a Chainguard token by exchanging the given OIDC token or path to a token.
// No user interaction is required.
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
	return sts.Exchange(ctx, cfg.Issuer, cfg.Audience, idToken, opts...)
}
