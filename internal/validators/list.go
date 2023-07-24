/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package validators

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ validator.List = &EachString{}
	_ validator.List = &ListLength{}
)

type EachString struct {
	Validators []validator.String
}

func (v EachString) Description(_ context.Context) string {
	return "Execute one or more string validators on a list of strings."
}

func (v EachString) MarkdownDescription(ctx context.Context) string {
	// TODO(colin): look into this further
	return v.Description(ctx)
}

func (v EachString) ValidateList(ctx context.Context, req validator.ListRequest, resp *validator.ListResponse) {
	if len(v.Validators) == 0 {
		return
	}

	// TODO: find a better way to go from []attr.Value to types.List of types.String
	sl := make([]string, 0, len(req.ConfigValue.Elements()))
	diags := req.ConfigValue.ElementsAs(ctx, &sl, false /* allowUnhandled */)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	for _, s := range sl {
		for _, valid := range v.Validators {
			sreq := validator.StringRequest{
				Path:           req.Path,
				PathExpression: req.PathExpression,
				Config:         req.Config,
				ConfigValue:    types.StringValue(s),
			}
			sresp := &validator.StringResponse{}
			valid.ValidateString(ctx, sreq, sresp)
			resp.Diagnostics.Append(sresp.Diagnostics...)
		}
	}
}

type ListLength struct {
	Min int
	Max int
}

func (v ListLength) Description(_ context.Context) string {
	return "Validate the length of a list. At least one of Min, Max must be specified."
}

func (v ListLength) MarkdownDescription(ctx context.Context) string {
	// TODO(colin): look into this further
	return v.Description(ctx)
}

func (v ListLength) ValidateList(_ context.Context, req validator.ListRequest, resp *validator.ListResponse) {
	// Verify the validator configuration
	if v.Min < 0 || v.Max < 0 {
		resp.Diagnostics.AddError("invalid validator configuration",
			fmt.Sprintf("ListLength.Min (%d) and ListLength.Max (%d) must both be non-negative.", v.Min, v.Max))
	}
	if v.Min == 0 && v.Max == 0 {
		resp.Diagnostics.AddError("invalid validator configuration",
			fmt.Sprintf("At least one of ListLength.Min (%d) and ListLength.Max (%d) must be non-zero.", v.Min, v.Max))
	}
	if v.Max > 0 && v.Max < v.Min {
		resp.Diagnostics.AddError("invalid validator configuration",
			fmt.Sprintf("Max (%d) cannot be less than Min (%d) when both are set.", v.Max, v.Min))
	}
	if resp.Diagnostics.HasError() {
		return
	}

	l := len(req.ConfigValue.Elements())
	if l < v.Min {
		resp.Diagnostics.AddError("failed list length validation",
			fmt.Sprintf("length=%d < min=%d", l, v.Min))
	}
	if v.Max > 0 && l > v.Max {
		resp.Diagnostics.AddError("failed list length validation",
			fmt.Sprintf("length=%d > max=%d", l, v.Max))
	}
}
