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
	"github.com/hashicorp/terraform-plugin-framework/types"

	"chainguard.dev/sdk/proto/capabilities"
	"chainguard.dev/sdk/uidp"
	"chainguard.dev/sdk/validation"
)

var (
	_ validator.String = &capability{}
	_ validator.String = &ifParentDefined{}
	_ validator.String = &isURL{}
	_ validator.String = &name{}
	_ validator.String = &uidpVal{}
	_ validator.String = &validateStringFuncs{}
	_ validator.String = &validRegExp{}
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

// IfParentDefined executes the given set of validators only if the parent of the attribute this
// validator is defined for is itself defined.
// This is useful for validating attributes within a block that is mutually exclusive with other blocks.
func IfParentDefined(validators ...validator.String) validator.String {
	return ifParentDefined{
		validators: validators,
	}
}

type ifParentDefined struct {
	validators []validator.String
}

func (v ifParentDefined) Description(_ context.Context) string {
	return "Execute the given validators only if this object at the given path is defined."
}

func (v ifParentDefined) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v ifParentDefined) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	parent := req.Path.ParentPath()
	o := types.Object{}
	if diags := req.Config.GetAttribute(ctx, parent, &o); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	// Don't run validators if the object at path isn't defined.
	if o.IsNull() || o.IsUnknown() {
		return
	}

	for _, val := range v.validators {
		r := new(validator.StringResponse)
		val.ValidateString(ctx, req, r)
		resp.Diagnostics.Append(r.Diagnostics...)
	}
}

// IsURL validates the given attribute is a valid URL of the form http[s]://host.tld
// If requiresHTTPS is true, the scheme must be https.
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

// Name validates the string value is a valid Chainguard name.
func Name() validator.String {
	return name{}
}

type name struct{}

func (v name) Description(_ context.Context) string {
	return "Check a given name is a valid Chainguard resource name."
}

func (v name) MarkdownDescription(ctx context.Context) string {
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
// allowRootSentinel allows "/" as a valid UIDP, which for some endpoints signals root.
func UIDP(allowRootSentinel bool) validator.String {
	return uidpVal{allowRootSentinel: allowRootSentinel}
}

type uidpVal struct {
	allowRootSentinel bool
}

func (v uidpVal) Description(_ context.Context) string {
	return "Check that the given string is a valid Chainguard UIDP."
}

func (v uidpVal) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v uidpVal) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	// Attributes may be optional, and thus null, which should not fail validation.
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	id := strings.TrimSpace(req.ConfigValue.ValueString())
	if !uidp.Valid(id) && !(v.allowRootSentinel && id == "/") {
		switch {
		case v.allowRootSentinel:
			resp.Diagnostics.AddError("failed uidp validation",
				fmt.Sprintf("%s is not a valid UIDP (or the '/' sentinel)", id))
		default:
			resp.Diagnostics.AddError("failed uidp validation",
				fmt.Sprintf("%s is not a valid UIDP", id))
		}
	}
}

type ValidateStringFunc func(string) error

type validateStringFuncs struct {
	funcs []ValidateStringFunc
}

// ValidateStringFuncs executes the given set of ValidateStringFunc. Useful for one-off string validation functions
// that can accept a string and return an error.
func ValidateStringFuncs(fns ...ValidateStringFunc) validator.String {
	return validateStringFuncs{
		funcs: fns,
	}
}

func (v validateStringFuncs) Description(_ context.Context) string {
	return "Validate a string with an arbitrary number of functions that accept a string and return an error."
}

func (v validateStringFuncs) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v validateStringFuncs) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
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

// ValidRegExp validates the string value is a compilable regular expression.
func ValidRegExp() validator.String {
	return validRegExp{}
}

type validRegExp struct{}

func (v validRegExp) Description(_ context.Context) string {
	return "Check that the given string is a compilable regular expression."
}

func (v validRegExp) MarkdownDescription(ctx context.Context) string {
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
