/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package token

// LoginConfig configures options for fetching and refreshing Chainguard a token.
type LoginConfig struct {
	// Auth0Connection is the social login to use with Auth0.
	// Must be one of: github, gitlab, google-oauth2
	Auth0Connection string

	// Audience is the audience of the Chainguard token.
	Audience string

	// Disabled determines if this package should attempt to refresh missing
	// and expired tokens automatically.
	Disabled bool

	// IdentityID is the exact UIDP of a Chainguard identity to assume.
	IdentityID string

	// IdentityProvider is the exact UIDP of a custom identity provider
	// to use for authentication. If empty, Auth0 is assumed.
	IdentityProvider string

	// IdentityToken is a path to an OIDC token, or literal identity token.
	IdentityToken string

	// Issuer is the URL of the Chainguard token issuer.
	Issuer string

	// OrgName is the verified organization name that defines a custom
	// identity provider to use for authentication.
	OrgName string

	// UserAgent is the user-agent to set during token exchange.
	UserAgent string

	// UseRefreshTokens indicates if refresh tokens should be created
	// and exchanged for access tokens.
	UseRefreshTokens bool
}
