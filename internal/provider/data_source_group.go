/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	common "chainguard.dev/sdk/proto/platform/common/v1"
	iam "chainguard.dev/sdk/proto/platform/iam/v1"
	"chainguard.dev/sdk/uidp"
	"github.com/chainguard-dev/terraform-provider-chainguard/internal/validators"
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
				Validators:  []validator.String{validators.UIDP(true /* allowRootSentinel */)},
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
	tflog.Info(ctx, fmt.Sprintf("read group data-source request: name=%s, parent_id=%s", data.Name, data.ParentID))

	uf := &common.UIDPFilter{}
	if data.ParentID.ValueString() != "" && data.ParentID.ValueString() != "/" {
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

	// Remove non-root groups if parent_id is root sentinel
	if data.ParentID.ValueString() == "/" {
		tflog.Info(ctx, "filtering by root")
		groups := make([]*iam.Group, 0, len(groupList.GetItems()))
		for _, g := range groupList.GetItems() {
			if uidp.InRoot(g.Id) {
				tflog.Info(ctx, fmt.Sprintf("found a root group: %s", g.Id))
				groups = append(groups, g)
			}
		}
		groupList.Items = groups
	}

	switch c := len(groupList.GetItems()); c {
	case 0:
		// Group was not found (either never existed, or was deleted).
		resp.Diagnostics.Append(dataNotFound("group", "" /* extra */, data))

	case 1:
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
