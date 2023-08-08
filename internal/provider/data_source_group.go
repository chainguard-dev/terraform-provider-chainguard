/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"fmt"

	"github.com/chainguard-dev/terraform-provider-chainguard/internal/validators"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"chainguard.dev/api/pkg/uidp"
	"chainguard.dev/api/proto/platform/common"
	"chainguard.dev/api/proto/platform/iam"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ datasource.DataSource              = &groupDataSource{}
	_ datasource.DataSourceWithConfigure = &groupDataSource{}
)

// NewGroupDataSource is a helper function to simplify the provider implementation.
func NewGroupDataSource() datasource.DataSource {
	return &groupDataSource{}
}

// groupDataSource is the data source implementation.
type groupDataSource struct {
	dataSource
}

type groupDataSourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	ParentID    types.String `tfsdk:"parent_id"`
}

func (d groupDataSourceModel) InputParams() string {
	return fmt.Sprintf("[id=%s, name=%s, parent_id=%s]", d.ID, d.Name, d.ParentID)
}

// Metadata returns the data source type name.
func (d *groupDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_group"
}

func (d *groupDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.configure(ctx, req, resp)
}

// Schema defines the schema for the data source.
func (d *groupDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lookup a group with the given name.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The exact UIDP of the group.",
				Optional:    true,
			},
			"name": schema.StringAttribute{
				Description: "The name of the group to lookup",
				Optional:    true,
				Validators:  []validator.String{validators.Name()},
			},
			"description": schema.StringAttribute{
				Description: "Description of the matched IAM group",
				Computed:    true,
			},
			"parent_id": schema.StringAttribute{
				Description: "The UIDP of the group in which to lookup the named group.",
				Optional:    true,
				// TODO(colin): default value
				Validators: []validator.String{validators.UIDP(true /* allowRoot */)},
			},
		},
	}
}

// Read refreshes the Terraform state with the latest data.
func (d *groupDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data groupDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, "read group data-source request", map[string]interface{}{"config": data})

	// TODO(colin): what if parent_id == /
	uf := &common.UIDPFilter{}
	if data.ParentID.ValueString() != "" {
		uf.ChildrenOf = data.ParentID.ValueString()
	}
	f := &iam.GroupFilter{
		Id:   data.ID.ValueString(),
		Name: data.Name.ValueString(),
		Uidp: uf,
	}
	groupList, err := d.prov.client.IAM().Groups().List(ctx, f)
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to list groups"))
		return
	}

	switch c := len(groupList.GetItems()); {
	case c == 0:
		// Group was not found (either never existed, or was deleted).
		resp.Diagnostics.Append(dataNotFound("group", "" /* extra */, data))

	case c == 1:
		g := groupList.GetItems()[0]
		data.ID = types.StringValue(g.Id)
		data.Name = types.StringValue(g.Name)
		data.Description = types.StringValue(g.Description)
		data.ParentID = types.StringValue(uidp.Parent(g.Id))

		// Set state
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)

	default:
		tflog.Error(ctx, fmt.Sprintf("group list returned %d groups for filter %v", c, f))
		resp.Diagnostics.Append(dataTooManyFound("group", "Please provide more context to narrow query (e.g. parent_id).", data))
	}
}
