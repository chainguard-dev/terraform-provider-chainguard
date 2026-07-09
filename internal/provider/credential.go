/*
Copyright 2026 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"

	"google.golang.org/grpc/credentials"

	"github.com/chainguard-dev/terraform-provider-chainguard/internal/token"
)

// refreshingCredential fetches the token per-RPC via token.Get (cached, or
// refreshed when expired), so long applies never send a stale token.
type refreshingCredential struct {
	loginConfig token.LoginConfig
}

var _ credentials.PerRPCCredentials = (*refreshingCredential)(nil)

func (c *refreshingCredential) GetRequestMetadata(ctx context.Context, _ ...string) (map[string]string, error) {
	tok, err := token.Get(ctx, c.loginConfig, false /* forceRefresh */)
	if err != nil {
		return nil, err
	}
	return map[string]string{"Authorization": "Bearer " + string(tok)}, nil
}

// RequireTransportSecurity matches the prior auth.NewFromToken(..., false) behavior.
func (c *refreshingCredential) RequireTransportSecurity() bool { return false }
