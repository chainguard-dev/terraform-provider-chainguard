/*
Copyright 2026 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestIsRetryable(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"unknown (raw 5xx)", status.Error(codes.Unknown, "500 Server Error"), true},
		{"unavailable", status.Error(codes.Unavailable, "try later"), true},
		{"internal", status.Error(codes.Internal, "boom"), true},
		{"deadline", status.Error(codes.DeadlineExceeded, "slow"), true},
		{"resource exhausted", status.Error(codes.ResourceExhausted, "429"), true},
		{"aborted", status.Error(codes.Aborted, "conflict"), true},
		{"not found", status.Error(codes.NotFound, "nope"), false},
		{"unauthenticated", status.Error(codes.Unauthenticated, "login"), false},
		{"permission denied", status.Error(codes.PermissionDenied, "no"), false},
		{"invalid argument", status.Error(codes.InvalidArgument, "bad"), false},
		{"non-grpc error", errors.New("plain"), false}, // maps to codes.Unknown via status.Code
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRetryable(tc.err); got != tc.want {
				t.Errorf("isRetryable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestWithRetry(t *testing.T) {
	// Use a zero base delay so the test does not actually sleep.
	t.Run("succeeds on first attempt", func(t *testing.T) {
		calls := 0
		got, err := withRetry(context.Background(), "op", func(context.Context) (int, error) {
			calls++
			return 42, nil
		})
		if err != nil || got != 42 {
			t.Fatalf("got (%d, %v), want (42, nil)", got, err)
		}
		if calls != 1 {
			t.Fatalf("calls = %d, want 1", calls)
		}
	})

	t.Run("retries transient then succeeds", func(t *testing.T) {
		calls := 0
		got, err := withRetryWithDelay(context.Background(), "op", 0, func(context.Context) (int, error) {
			calls++
			if calls < 3 {
				return 0, status.Error(codes.Unavailable, "transient")
			}
			return 7, nil
		})
		if err != nil || got != 7 {
			t.Fatalf("got (%d, %v), want (7, nil)", got, err)
		}
		if calls != 3 {
			t.Fatalf("calls = %d, want 3", calls)
		}
	})

	t.Run("gives up after max attempts", func(t *testing.T) {
		calls := 0
		_, err := withRetryWithDelay(context.Background(), "op", 0, func(context.Context) (int, error) {
			calls++
			return 0, status.Error(codes.Internal, "always fails")
		})
		if status.Code(err) != codes.Internal {
			t.Fatalf("err code = %v, want Internal", status.Code(err))
		}
		if calls != retryMaxAttempts {
			t.Fatalf("calls = %d, want %d", calls, retryMaxAttempts)
		}
	})

	t.Run("does not retry non-retryable error", func(t *testing.T) {
		calls := 0
		_, err := withRetry(context.Background(), "op", func(context.Context) (int, error) {
			calls++
			return 0, status.Error(codes.NotFound, "missing")
		})
		if status.Code(err) != codes.NotFound {
			t.Fatalf("err code = %v, want NotFound", status.Code(err))
		}
		if calls != 1 {
			t.Fatalf("calls = %d, want 1 (no retry)", calls)
		}
	})

	t.Run("honors context cancellation during backoff", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		calls := 0
		// Non-zero delay so cancellation wins the select before the next attempt.
		_, err := withRetryWithDelay(ctx, "op", time.Hour, func(context.Context) (int, error) {
			calls++
			cancel()
			return 0, status.Error(codes.Unavailable, "transient")
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if calls != 1 {
			t.Fatalf("calls = %d, want 1", calls)
		}
	})
}
