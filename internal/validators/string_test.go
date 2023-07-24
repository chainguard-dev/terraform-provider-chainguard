/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package validators

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestEnumValidateString(t *testing.T) {
	tests := map[string]struct {
		valid   map[string]int32
		input   string
		wantErr bool
	}{
		"valid input": {
			valid: map[string]int32{
				"ONE": 1,
				"TWO": 2,
			},
			input:   "ONE",
			wantErr: false,
		},
		"invalid input": {
			valid: map[string]int32{
				"ONE": 1,
				"TWO": 2,
			},
			input:   "one",
			wantErr: true,
		},
		"empty valid map": {
			valid:   map[string]int32{},
			input:   "ONE",
			wantErr: true,
		},
		"nil valid map": {
			valid:   nil,
			input:   "ONE",
			wantErr: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			req := validator.StringRequest{
				ConfigValue: types.StringValue(test.input),
			}
			resp := &validator.StringResponse{}
			enum := Enum{
				Valid: test.valid,
			}

			enum.ValidateString(context.Background(), req, resp)

			if resp.Diagnostics.HasError() != test.wantErr {
				t.Fatalf("Enum.ValidateString() mismatch, want=%t got=%t",
					test.wantErr, resp.Diagnostics.HasError())
			}
		})
	}
}

func TestNameValidateString(t *testing.T) {
	tests := map[string]struct {
		input   string
		wantErr bool
	}{
		"valid name": {
			input:   "good-chainguard-name",
			wantErr: false,
		},
		"invalid input": {
			input:   "BAD NAME!",
			wantErr: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			req := validator.StringRequest{
				ConfigValue: types.StringValue(test.input),
			}
			resp := &validator.StringResponse{}

			Name{}.ValidateString(context.Background(), req, resp)

			if resp.Diagnostics.HasError() != test.wantErr {
				t.Fatalf("Name.ValidateString() mismatch, want=%t got=%t",
					test.wantErr, resp.Diagnostics.HasError())
			}
		})
	}
}

func TestNonEmptyValidateString(t *testing.T) {
	tests := map[string]struct {
		input   string
		wantErr bool
	}{
		"nonempty": {
			input:   "a string with characters",
			wantErr: false,
		},
		"empty": {
			input:   "",
			wantErr: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			req := validator.StringRequest{
				ConfigValue: types.StringValue(test.input),
			}
			resp := &validator.StringResponse{}

			NonEmpty{}.ValidateString(context.Background(), req, resp)

			if resp.Diagnostics.HasError() != test.wantErr {
				t.Fatalf("NonEmpty.ValidateString() mismatch, want=%t got=%t",
					test.wantErr, resp.Diagnostics.HasError())
			}
		})
	}
}

func TestOneOfValidateString(t *testing.T) {
	tests := map[string]struct {
		valid   []string
		input   string
		wantErr bool
	}{
		"valid": {
			valid:   []string{"a", "b", "c"},
			input:   "a",
			wantErr: false,
		},
		"invalid": {
			valid:   []string{"a", "b", "c"},
			input:   "d",
			wantErr: true,
		},
		"empty valid list": {
			valid:   []string{},
			input:   "a",
			wantErr: true,
		},
		"nil valid list": {
			valid:   nil,
			input:   "a",
			wantErr: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			req := validator.StringRequest{
				ConfigValue: types.StringValue(test.input),
			}
			resp := &validator.StringResponse{}
			oneof := OneOf{
				Valid: test.valid,
			}

			oneof.ValidateString(context.Background(), req, resp)

			if resp.Diagnostics.HasError() != test.wantErr {
				t.Fatalf("OneOf.ValidateString() mismatch, want=%t got=%t",
					test.wantErr, resp.Diagnostics.HasError())
			}
		})
	}
}

func TestUIDPValidateString(t *testing.T) {
	tests := map[string]struct {
		allowRoot bool
		input     string
		wantErr   bool
	}{
		"valid UIDP (subresource)": {
			input:   "fb694596eb1678321f94eec283e1e0be690f655c/7542b4e1600377ce",
			wantErr: false,
		},
		"valid UIDP (in root)": {
			input:   "fb694596eb1678321f94eec283e1e0be690f655c",
			wantErr: false,
		},
		"root sentinel (/) with AllowRoot": {
			allowRoot: true,
			input:     "/",
			wantErr:   false,
		},
		"root sentinel (/) without AllowRoot": {
			allowRoot: false,
			input:     "/",
			wantErr:   true,
		},
		"invalid input": {
			input:   "not-a-uidp",
			wantErr: true,
		},
		"invalid input with AllowRoot": {
			allowRoot: true,
			input:     "not-a-uidp",
			wantErr:   true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			req := validator.StringRequest{
				ConfigValue: types.StringValue(test.input),
			}
			resp := &validator.StringResponse{}
			uidp := UIDP{
				AllowRoot: test.allowRoot,
			}

			uidp.ValidateString(context.Background(), req, resp)

			if resp.Diagnostics.HasError() != test.wantErr {
				t.Fatalf("UIDP.ValidateString() mismatch, want=%t got=%t",
					test.wantErr, resp.Diagnostics.HasError())
			}
		})
	}
}
