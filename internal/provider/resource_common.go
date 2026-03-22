/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type managedResource struct {
	prov *providerData
}

func (mr *managedResource) configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	pd, ok := req.ProviderData.(*providerData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *providerData, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	// If the client hasn't been configured yet, configure it
	if pd.client == nil {
		tflog.Info(ctx, "initializing Chainguard API client (managed resource)")
		if err := pd.setupClient(ctx); err != nil {
			resp.Diagnostics.Append(errorToDiagnostic(err, "unable to setup client"))
			return
		}
	}

	mr.prov = pd
}

// retryOnPermissionDenied retries fn with exponential backoff when the error
// is codes.PermissionDenied. This handles the eventual consistency window
// after creating a parent group — role bindings may not have propagated yet,
// causing child resource creation to fail with PermissionDenied.
func retryOnPermissionDenied[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	const (
		maxAttempts = 5
		baseDelay   = 2 * time.Second
		maxDelay    = 16 * time.Second
	)

	for attempt := range maxAttempts {
		result, err := fn()
		if err == nil {
			return result, nil
		}
		if status.Code(err) != codes.PermissionDenied {
			return result, err
		}
		if attempt >= maxAttempts-1 {
			return result, err
		}

		delay := min(baseDelay<<attempt, maxDelay)
		tflog.Debug(ctx, "retrying after PermissionDenied (role binding propagation)", map[string]any{
			"attempt": attempt + 1,
			"delay":   delay.String(),
		})

		select {
		case <-time.After(delay):
		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		}
	}

	// Unreachable, but satisfies the compiler.
	var zero T
	return zero, nil
}
