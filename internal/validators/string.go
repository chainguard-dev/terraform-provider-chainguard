/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package validators

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"

	"chainguard.dev/api/pkg/uidp"
	"chainguard.dev/api/pkg/validation"
	"chainguard.dev/api/proto/capabilities"
)

var (
	_ validator.String = &capability{}
	_ validator.String = &name{}
	_ validator.String = &isURL{}
	_ validator.String = &runFuncs{}
	_ validator.String = &uidpVal{}
)

// Capability validates the string value is a valid role capability.
func Capability() validator.String {
	return capability{}
}

type capability struct{}

func (v capability) Description(_ context.Context) string {
	return "Check a given name is a valid Chainguard role capability."
}

func (v capability) MarkdownDescription(ctx context.Context) string {
	// TODO(colin): look into this further
	return v.Description(ctx)
}

func (v capability) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
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

func IsURL(requireHTTPS bool) validator.String {
	return isURL{RequireHTTPS: requireHTTPS}
}

type isURL struct {
	RequireHTTPS bool
}

func (v isURL) Description(_ context.Context) string {
	return "Check if a string is valid URL, and optionally validate the schema is HTTPS."
}

func (v isURL) MarkdownDescription(ctx context.Context) string {
	// TODO(colin): look into this further
	return v.Description(ctx)
}

func (v isURL) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	// Attributes may be optional, and thus null, which should not fail validation.
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	raw := req.ConfigValue.ValueString()
	u, err := url.Parse(raw)

	// url.Parse is fairly lenient, let's be stricter.
	if err != nil || u.Scheme == "" || u.Host == "" {
		resp.Diagnostics.AddError("failed URL validation", fmt.Sprintf("failed to parse %q as a URL", raw))
		return
	}

	if v.RequireHTTPS && u.Scheme != "https" {
		resp.Diagnostics.AddError("failed HTTPS validation", fmt.Sprintf("URL must have HTTPS scheme, got %q", u.Scheme))
	}
}

type ValidateStringFunc func(string) error

type runFuncs struct {
	funcs []ValidateStringFunc
}

func RunFuncs(fns ...ValidateStringFunc) validator.String {
	return runFuncs{
		funcs: fns,
	}
}

func (v runFuncs) Description(_ context.Context) string {
	return "Validate a string with an arbitrary number of functions that accept a string and return an error."
}

func (v runFuncs) MarkdownDescription(ctx context.Context) string {
	// TODO(colin): look into this further
	return v.Description(ctx)
}

func (v runFuncs) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	// Attributes may be optional, and thus null, which should not fail validation.
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	s := req.ConfigValue.ValueString()

	for _, fn := range v.funcs {
		if err := fn(s); err != nil {
			resp.Diagnostics.AddError("failed string validation", err.Error())
		}
	}
}

// Name validates the string value is a valid Chainguard name.
func Name() validator.String {
	return name{}
}

type name struct{}

func (v name) Description(_ context.Context) string {
	return "Check a given name is a valid Chainguard resource name."
}

func (v name) MarkdownDescription(ctx context.Context) string {
	// TODO(colin): look into this further
	return v.Description(ctx)
}

func (v name) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
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

// UIDP validates the string value is a valid Chainguard UIDP.
func UIDP(allowRoot bool) validator.String {
	return uidpVal{allowRoot: allowRoot}
}

type uidpVal struct {
	allowRoot bool
}

func (v uidpVal) Description(_ context.Context) string {
	return "Check that the given string is a valid Chainguard UIDP."
}

func (v uidpVal) MarkdownDescription(ctx context.Context) string {
	// TODO(colin): look into this further
	return v.Description(ctx)
}

func (v uidpVal) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	// Attributes may be optional, and thus null, which should not fail validation.
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	id := strings.TrimSpace(req.ConfigValue.ValueString())
	if !uidp.Valid(id) && !(v.allowRoot && id == "/") {
		switch {
		case v.allowRoot:
			resp.Diagnostics.AddError("failed uidp validation",
				fmt.Sprintf("%s is not a valid UIDP (or the '/' sentinel)", id))
		default:
			resp.Diagnostics.AddError("failed uidp validation",
				fmt.Sprintf("%s is not a valid UIDP", id))
		}
	}
}

// ValidRegExp validates the string value is a compilable regular expression.
func ValidRegExp() validator.String {
	return validRegExp{}
}

type validRegExp struct{}

func (v validRegExp) Description(_ context.Context) string {
	return "Check that the given string is a compilable regular expression."
}

func (v validRegExp) MarkdownDescription(ctx context.Context) string {
	// TODO(colin): look into this further
	return v.Description(ctx)
}

func (v validRegExp) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	// Attributes may be optional, and thus null, which should not fail validation.
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	exp := strings.TrimSpace(req.ConfigValue.ValueString())
	_, err := regexp.Compile(exp)
	if err != nil {
		resp.Diagnostics.AddError("failed regexp validation", err.Error())
	}
}
