/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
)

// dataModel is an interface for data source data structures.
type dataModel interface {
	InputParams() string
}

//nolint:unparam
func dataNotFound(n, extra string, m dataModel) diag.Diagnostic {
	detail := fmt.Sprintf("Input parameters: %s", m.InputParams())
	if extra != "" {
		detail = fmt.Sprintf("%s\n%s", detail, extra)
	}
	return diag.NewErrorDiagnostic(
		fmt.Sprintf("%s not found", n),
		detail,
	)
}

func dataTooManyFound(n, extra string, m dataModel) diag.Diagnostic {
	detail := fmt.Sprintf("Input parameters: %s", m.InputParams())
	if extra != "" {
		detail = fmt.Sprintf("%s\n%s", detail, extra)
	}
	return diag.NewErrorDiagnostic(
		fmt.Sprintf("more than one %s found matching input", n),
		detail,
	)
}

type dataSource struct {
	prov *providerData
}

func (ds *dataSource) configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	pd, ok := req.ProviderData.(*providerData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected providerData, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	ds.prov = pd
}
