/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"
)

type managedResource struct {
	prov *providerData
}

func (mr *managedResource) configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	pd, ok := req.ProviderData.(*providerData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *providerData, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	// Ensure the client is initialized exactly once, even under concurrent access.
	if err := pd.setupClient(ctx); err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "unable to setup client"))
		return
	}

	mr.prov = pd
}
