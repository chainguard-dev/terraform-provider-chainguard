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

func TestEachStringValidateList(t *testing.T) {
	// Validators chosen to ensure inputs could match [0,1,2] validators.
	validators := []validator.String{
		OneOf{Valid: []string{"fb694596eb1678321f94eec283e1e0be690f655c", "fb694596eb1678321f94eec283e1e0be690f655c/7542b4e1600377ce", "not-valid-uidp"}},
		UIDP{},
	}

	tests := map[string]struct {
		input        []string
		wantErrCount int
	}{
		"valid uidp IN list (1)": {
			input:        []string{"fb694596eb1678321f94eec283e1e0be690f655c"},
			wantErrCount: 0,
		},
		"valid uidp IN list (2)": {
			input:        []string{"fb694596eb1678321f94eec283e1e0be690f655c", "fb694596eb1678321f94eec283e1e0be690f655c/7542b4e1600377ce"},
			wantErrCount: 0,
		},
		"valid uidp NOT IN list": {
			input:        []string{"bd459166392625d3ba12561f8912e22a022eb8ed"},
			wantErrCount: 1,
		},
		"invalid uidp IN list": {
			input:        []string{"not-valid-uidp"},
			wantErrCount: 1,
		},
		"invalid uidp NOT IN list": {
			input:        []string{"still-not-valid-uidp"},
			wantErrCount: 2,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			in, _ := types.ListValueFrom(ctx, types.StringType, test.input)
			req := validator.ListRequest{
				ConfigValue: in,
			}
			resp := &validator.ListResponse{}
			each := EachString{
				Validators: validators,
			}

			each.ValidateList(context.Background(), req, resp)

			if resp.Diagnostics.ErrorsCount() != test.wantErrCount {
				t.Fatalf("EachString.ValidateList() mismatch error count, want=%d got=%d",
					test.wantErrCount, resp.Diagnostics.ErrorsCount())
			}
		})
	}
}

func TestListLengthValidateList(t *testing.T) {
	tests := map[string]struct {
		min     int
		max     int
		input   []string
		wantErr bool
	}{
		"min, list larger": {
			min:     2,
			input:   []string{"a", "b", "c"},
			wantErr: false,
		},
		"min, list equal": {
			min:     2,
			input:   []string{"a", "b"},
			wantErr: false,
		},
		"min, list shorter": {
			min:     2,
			input:   []string{"a"},
			wantErr: true,
		},
		"max, list larger": {
			max:     2,
			input:   []string{"a", "b", "c"},
			wantErr: true,
		},
		"max, list equal": {
			max:     2,
			input:   []string{"a", "b"},
			wantErr: false,
		},
		"max, list shorter": {
			max:     2,
			input:   []string{"a"},
			wantErr: false,
		},
		"both, list larger": {
			min:     1,
			max:     2,
			input:   []string{"a", "b", "c"},
			wantErr: true,
		},
		"both, list within": {
			min:     1,
			max:     2,
			input:   []string{"a", "b"},
			wantErr: false,
		},
		"both, list shorter": {
			min:     2,
			max:     2,
			input:   []string{"a"},
			wantErr: true,
		},
		"negative min": {
			min:     -1,
			input:   []string{"a"},
			wantErr: true,
		},
		"negative max": {
			max:     -1,
			input:   []string{"a"},
			wantErr: true,
		},
		"negative min and max": {
			min:     -1,
			max:     -1,
			input:   []string{"a"},
			wantErr: true,
		},
		"neither set": {
			input:   []string{"a"},
			wantErr: true,
		},
		"max < min": {
			min:     3,
			max:     2,
			input:   []string{"a"},
			wantErr: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			in, _ := types.ListValueFrom(ctx, types.StringType, test.input)
			req := validator.ListRequest{
				ConfigValue: in,
			}
			resp := &validator.ListResponse{}
			ll := ListLength{
				Min: test.min,
				Max: test.max,
			}

			ll.ValidateList(context.Background(), req, resp)

			if resp.Diagnostics.HasError() != test.wantErr {
				t.Fatalf("ListLength.ValidateList() mismatch, want=%t got=%t",
					test.wantErr, resp.Diagnostics.HasError())
			}
		})
	}
}
