/*
Copyright 2026 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRetryOnPermissionDenied_ImmediateSuccess(t *testing.T) {
	calls := 0
	result, err := retryOnPermissionDenied(t.Context(), func() (string, error) {
		calls++
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Errorf("got %q, want %q", result, "ok")
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestRetryOnPermissionDenied_SuccessAfterRetries(t *testing.T) {
	calls := 0
	result, err := retryOnPermissionDenied(t.Context(), func() (string, error) {
		calls++
		if calls < 3 {
			return "", status.Error(codes.PermissionDenied, "not yet")
		}
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Errorf("got %q, want %q", result, "ok")
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestRetryOnPermissionDenied_NonRetryableError(t *testing.T) {
	calls := 0
	_, err := retryOnPermissionDenied(t.Context(), func() (string, error) {
		calls++
		return "", status.Error(codes.NotFound, "gone")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("expected 1 call (no retry), got %d", calls)
	}
}

func TestRetryOnPermissionDenied_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := retryOnPermissionDenied(ctx, func() (string, error) {
		return "", status.Error(codes.PermissionDenied, "denied")
	})
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}
