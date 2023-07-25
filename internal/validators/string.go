/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package validators

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"

	"chainguard.dev/api/pkg/uidp"
	"chainguard.dev/api/pkg/validation"
	"chainguard.dev/api/proto/capabilities"
)

var (
	_ validator.String = &Capability{}
	_ validator.String = &Enum{}
	_ validator.String = &Name{}
	_ validator.String = &NonEmpty{}
	_ validator.String = &UIDP{}
)

// Capability validates the string value is a valid role capability.
type Capability struct{}

func (v Capability) Description(_ context.Context) string {
	return "Check a given name is a valid Chainguard role capability."
}

func (v Capability) MarkdownDescription(ctx context.Context) string {
	// TODO(colin): look into this further
	return v.Description(ctx)
}

func (v Capability) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	// Attributes may be optional, and thus null, which should not fail validation.
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	sc := req.ConfigValue.ValueString()
	c, err := capabilities.Parse(sc)
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("failed to parse capability %q", sc), err.Error())
	}
	if c == capabilities.Capability_UNKNOWN {
		resp.Diagnostics.AddError(fmt.Sprintf("failed to parse capability %q", sc),
			"unknown capability: "+sc)
	}
}

// Enum validates the string is a valid enum name.
type Enum struct {
	Valid map[string]int32
}

func (v Enum) Description(_ context.Context) string {
	return "Check a string is a valid enum name."
}

func (v Enum) MarkdownDescription(ctx context.Context) string {
	// TODO(colin): look into this further
	return v.Description(ctx)
}

func (v Enum) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	// TODO(colin): case insensitve?
	val := req.ConfigValue.ValueString()
	if _, ok := v.Valid[val]; !ok {
		resp.Diagnostics.AddError("failed enum validation",
			fmt.Sprintf("value %q is not a valid enum value", val))
	}
}

// Name validates the string value is a valid Chainguard name.
type Name struct{}

func (v Name) Description(_ context.Context) string {
	return "Check a given name is a valid Chainguard resource name."
}

func (v Name) MarkdownDescription(ctx context.Context) string {
	// TODO(colin): look into this further
	return v.Description(ctx)
}

func (v Name) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	// Attributes may be optional, and thus null, which should not fail validation.
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	name := req.ConfigValue.ValueString()
	if err := validation.ValidateName(name); err != nil {
		resp.Diagnostics.AddError("failed name validation",
			fmt.Sprintf("%s is not a valid Chainguard resource name: %s", name, err.Error()))
	}
}

// NonEmpty validates the string value is non-empty.
type NonEmpty struct{}

func (v NonEmpty) Description(_ context.Context) string {
	return "Check a given string is a non-empty."
}

func (v NonEmpty) MarkdownDescription(ctx context.Context) string {
	// TODO(colin): look into this further
	return v.Description(ctx)
}

func (v NonEmpty) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.ValueString() == "" {
		resp.Diagnostics.AddError("failed non-empty validation",
			fmt.Sprintf("%s cannot be an empty string", req.Path.String()))
	}
}

// OneOf validates the string value is a member of a given set of valid strings.
type OneOf struct {
	Valid []string
}

func (v OneOf) Description(_ context.Context) string {
	return "Check that the given string is a member of a given set of valid strings."
}

func (v OneOf) MarkdownDescription(ctx context.Context) string {
	// TODO(colin): look into this further
	return v.Description(ctx)
}

func (v OneOf) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	s := strings.TrimSpace(req.ConfigValue.ValueString())
	for _, valid := range v.Valid {
		// Found it!
		if s == valid {
			return
		}
	}
	// Didn't find it :(
	resp.Diagnostics.AddError("failed one-of validation",
		fmt.Sprintf("%s is not a member of valid choices: %v", s, v.Valid))
}

// UIDP validates the string value is a valid Chainguard UIDP.
type UIDP struct {
	AllowRoot bool
}

func (v UIDP) Description(_ context.Context) string {
	return "Check that the given string is a valid Chainguard UIDP."
}

func (v UIDP) MarkdownDescription(ctx context.Context) string {
	// TODO(colin): look into this further
	return v.Description(ctx)
}

func (v UIDP) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	// Attributes may be optional, and thus null, which should not fail validation.
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	id := strings.TrimSpace(req.ConfigValue.ValueString())
	if !uidp.Valid(id) && !(v.AllowRoot && id == "/") {
		switch {
		case v.AllowRoot:
			resp.Diagnostics.AddError("failed uidp validation",
				fmt.Sprintf("%s is not a valid UIDP (or the '/' sentinel)", id))
		default:
			resp.Diagnostics.AddError("failed uidp validation",
				fmt.Sprintf("%s is not a valid UIDP", id))
		}
	}
}
