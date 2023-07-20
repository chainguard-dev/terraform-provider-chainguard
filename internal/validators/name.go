package validators

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"

	"chainguard.dev/api/pkg/validation"
)

type NameValidator struct{}

func (v NameValidator) Description(_ context.Context) string {
	return "Check a given name is a valid Chainguard resource name."
}

func (v NameValidator) MarkdownDescription(ctx context.Context) string {
	// TODO(colin): look into this further
	return v.Description(ctx)
}

func (v NameValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	// Attributes may be optional, and thus null, which should not fail validation.
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	name := req.ConfigValue.ValueString()
	if err := validation.ValidateName(name); err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("%s is not a valid Chainguard resource name", name), err.Error())
	}
}

var _ validator.String = &NameValidator{}
