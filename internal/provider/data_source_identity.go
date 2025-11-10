/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iam "chainguard.dev/sdk/proto/platform/iam/v1"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ datasource.DataSource              = &identityDataSource{}
	_ datasource.DataSourceWithConfigure = &identityDataSource{}
)

// NewIdentityDataSource is a helper function to simplify the provider implementation.
func NewIdentityDataSource() datasource.DataSource {
	return &identityDataSource{}
}

// identityDataSource is the data source implementation.
type identityDataSource struct {
	dataSource
}

type identityDataSourceModel struct {
	ID      types.String `tfsdk:"id"`
	Issuer  types.String `tfsdk:"issuer"`
	Subject types.String `tfsdk:"subject"`
}

func (m identityDataSourceModel) InputParams() string {
	return fmt.Sprintf("[issuer=%s, subject=%s]", m.Issuer, m.Subject)
}

// Metadata returns the data source type name.
func (d *identityDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_identity"
}

func (d *identityDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.configure(ctx, req, resp)
}

// Schema defines the schema for the data source.
func (d *identityDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lookup an identity with the given issuer and subject.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The UIDP of this identity.",
				Computed:    true,
			},
			"issuer": schema.StringAttribute{
				Description: "The exact issuer of the identity.",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"subject": schema.StringAttribute{
				Description: "The exact subject of the identity.",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
		},
	}
}

// Read refreshes the Terraform state with the latest data.
func (d *identityDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data identityDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, "read identity data-source request", map[string]any{"config": data})

	lr := &iam.LookupRequest{
		Subject: data.Subject.ValueString(),
		Issuer:  data.Issuer.ValueString(),
	}
	id, err := d.prov.client.IAM().Identities().Lookup(ctx, lr)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			resp.Diagnostics.Append(dataNotFound("identity", "" /* extra */, data))
		} else {
			resp.Diagnostics.Append(errorToDiagnostic(err, "failed to list identities"))
		}
	} else {
		// Set state
		data.ID = types.StringValue(id.Id)
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	}
}
