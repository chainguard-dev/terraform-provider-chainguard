/*
Copyright 2026 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-log/tflog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	retryMaxAttempts = 5
	retryBaseDelay   = 2 * time.Second
	retryMaxDelay    = 16 * time.Second
)

// isRetryable reports whether err is a transient, server-side gRPC failure
// where retrying an idempotent call may succeed.
//
// codes.Unknown is included deliberately: a raw HTTP 5xx page returned by an
// upstream load balancer (rather than a well-formed gRPC response) surfaces to
// the client as codes.Unknown, which is exactly how transient 500s from the
// identities API present.
func isRetryable(err error) bool {
	// Require a genuine gRPC status: status.Code maps any non-status error to
	// codes.Unknown, which would make every plain error look retryable.
	s, ok := status.FromError(err)
	if !ok {
		return false
	}
	switch s.Code() {
	case codes.Unavailable,
		codes.Internal,
		codes.Unknown,
		codes.DeadlineExceeded,
		codes.ResourceExhausted,
		codes.Aborted:
		return true
	default:
		return false
	}
}

// withRetry invokes fn, retrying with exponential backoff (2s, 4s, 8s, 16s)
// while it returns a transient gRPC error (see isRetryable). It returns the
// first successful result, or the last error once attempts are exhausted. If
// ctx is cancelled during a backoff, the context error is returned.
//
// fn must be idempotent; only read-only / lookup calls should use this.
func withRetry[T any](ctx context.Context, operation string, fn func(context.Context) (T, error)) (T, error) {
	return withRetryWithDelay(ctx, operation, retryBaseDelay, fn)
}

// withRetryWithDelay is withRetry with an injectable base delay so tests can
// exercise the retry loop without sleeping.
func withRetryWithDelay[T any](ctx context.Context, operation string, baseDelay time.Duration, fn func(context.Context) (T, error)) (T, error) {
	var (
		result T
		err    error
	)
	for attempt := range retryMaxAttempts {
		result, err = fn(ctx)
		if err == nil || !isRetryable(err) {
			return result, err
		}

		// Don't sleep after the final attempt.
		if attempt == retryMaxAttempts-1 {
			break
		}

		delay := min(baseDelay<<attempt, retryMaxDelay)
		tflog.Debug(ctx, "retrying after transient error", map[string]any{
			"operation": operation,
			"attempt":   attempt + 1,
			"code":      status.Code(err).String(),
			"delay":     delay.String(),
		})

		select {
		case <-time.After(delay):
			// Continue to next attempt.
		case <-ctx.Done():
			return result, ctx.Err()
		}
	}
	return result, err
}
