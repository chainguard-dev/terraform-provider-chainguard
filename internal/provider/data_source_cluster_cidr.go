/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"chainguard.dev/api/proto/platform/tenant"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ datasource.DataSource              = &clusterCIDRDataSource{}
	_ datasource.DataSourceWithConfigure = &clusterCIDRDataSource{}
)

// NewClusterCIDRDataSource is a helper function to simplify the provider implementation.
func NewClusterCIDRDataSource() datasource.DataSource {
	return &clusterCIDRDataSource{}
}

// clusterCIDRDataSource is the data source implementation.
type clusterCIDRDataSource struct {
	dataSource
}

type clusterCIDRDataSourceModel struct {
	ID         types.String `tfsdk:"id"`
	CIDRBlocks types.List   `tfsdk:"cidr_blocks"`
}

func (m clusterCIDRDataSourceModel) InputParams() string {
	return "[]"
}

// Metadata returns the data source type name.
func (d *clusterCIDRDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cluster_cidr"
}

func (d *clusterCIDRDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.configure(ctx, req, resp)
}

// Schema defines the schema for the data source.
func (d *clusterCIDRDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "IPv4 CIDR blocks that Enforce uses to communicate with cluster API servers.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
			},
			"cidr_blocks": schema.ListAttribute{
				Description: "List of IPv4 CIDR blocks used by Enforce to reach out to clusters.",
				Computed:    true,
				ElementType: types.StringType,
			},
		},
	}
}

// Read refreshes the Terraform state with the latest data.
func (d *clusterCIDRDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	tflog.Info(ctx, "read cluster CIDR blocks data-source request", map[string]interface{}{"request": req})

	var data clusterCIDRDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cidr, err := d.prov.client.Tenant().Clusters().CIDR(ctx, &tenant.ClusterCIDRRequest{})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to list CIDR blocks"))
		return
	}

	blocks, diags := types.ListValueFrom(ctx, types.StringType, cidr.CidrBlocks)
	resp.Diagnostics.Append(diags...)
	if diags.HasError() {
		tflog.Error(ctx, "failed to convert CIDR blocks to basetypes.ListValue", map[string]any{"CIDR-blocks": cidr.CidrBlocks})
		return
	}

	// Set the ID on clusterCIDRDataSourceModel for acceptance tests.
	// https://developer.hashicorp.com/terraform/tutorials/providers-plugin-framework/providers-plugin-framework-acceptance-testing#implement-data-source-id-attribute
	// TODO(colin): replace this?
	if d.prov.version == "acctest" {
		data.ID = types.StringValue("placeholder")
	}
	data.CIDRBlocks = blocks

	// Set state
	diags = resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
}
