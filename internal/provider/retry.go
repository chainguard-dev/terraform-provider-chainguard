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
//
// codes.Internal is broader than the gRPC retry-policy default set, but Cloud
// Run / the API gateway returns it for transient server-side faults; the cost
// of retrying a deterministic Internal is bounded (all attempts then surface
// the original error). Lookup is idempotent, so this is a safe trade.
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

// backoffDelay returns the wait before the retry following a zero-based attempt
// index: an exponential schedule (baseDelay, 2x, 4x, ...) capped at retryMaxDelay.
func backoffDelay(baseDelay time.Duration, attempt int) time.Duration {
	return min(baseDelay<<attempt, retryMaxDelay)
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

		delay := backoffDelay(baseDelay, attempt)
		tflog.Debug(ctx, "retrying after transient error", map[string]any{
			"operation": operation,
			"attempt":   attempt + 1,
			"code":      status.Code(err).String(),
			"delay":     delay.String(),
		})

		// time.NewTimer (not time.After) so the timer is released promptly when
		// ctx is cancelled mid-backoff rather than lingering until it fires.
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
			// Continue to next attempt.
		case <-ctx.Done():
			timer.Stop()
			return result, ctx.Err()
		}
	}

	// All attempts exhausted on a retryable error: surface it in logs since the
	// per-attempt retries above only log at DEBUG.
	tflog.Warn(ctx, "exhausted retries after transient errors", map[string]any{
		"operation": operation,
		"attempts":  retryMaxAttempts,
		"code":      status.Code(err).String(),
	})
	return result, err
}
