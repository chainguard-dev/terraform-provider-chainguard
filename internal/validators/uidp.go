package validators

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"

	"chainguard.dev/api/pkg/uidp"
)

type UIDPValidator struct {
	AllowRoot bool
}

func (v UIDPValidator) Description(_ context.Context) string {
	return "Check that the given string is a valid Chainguard UIDP."
}

func (v UIDPValidator) MarkdownDescription(ctx context.Context) string {
	// TODO(colin): look into this further
	return v.Description(ctx)
}

func (v UIDPValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	// Attributes may be optional, and thus null, which should not fail validation.
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	id := strings.TrimSpace(req.ConfigValue.ValueString())
	if !uidp.Valid(id) && !(v.AllowRoot && id == "/") {
		switch {
		case v.AllowRoot:
			resp.Diagnostics.AddError(fmt.Sprintf("%s is not a valid UIDP (or the '/' sentinel)", id), "")
		default:
			resp.Diagnostics.AddError(fmt.Sprintf("%s is not a valid UIDP", id), "")
		}
	}
}

var _ validator.String = &UIDPValidator{}
