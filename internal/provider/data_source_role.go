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

	iam "chainguard.dev/sdk/proto/platform/iam/v1"
	"github.com/chainguard-dev/terraform-provider-chainguard/internal/validators"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ datasource.DataSource              = &roleDataSource{}
	_ datasource.DataSourceWithConfigure = &roleDataSource{}
)

// NewRoleDataSource is a helper function to simplify the provider implementation.
func NewRoleDataSource() datasource.DataSource {
	return &roleDataSource{}
}

// roleDataSource is the data source implementation.
type roleDataSource struct {
	dataSource
}

type roleDataSourceModel struct {
	ID     types.String `tfsdk:"id"`
	Name   types.String `tfsdk:"name"`
	Parent types.String `tfsdk:"parent"`

	Items []*roleModel `tfsdk:"items"`
}

func (d roleDataSourceModel) InputParams() string {
	return fmt.Sprintf("[id=%s, name=%s, parentd=%s]", d.ID, d.Name, d.Parent)
}

type roleModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	Description  types.String `tfsdk:"description"`
	Capabilities types.List   `tfsdk:"capabilities"`
}

// Metadata returns the data source type name.
func (d *roleDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role"
}

func (d *roleDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.configure(ctx, req, resp)
}

// Schema defines the schema for the data source.
func (d *roleDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lookup a role with the given name.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The exact UIDP of the role to lookup.",
				Optional:    true,
				Validators:  []validator.String{validators.UIDP(false /* allowRootSentinel */)},
			},
			"name": schema.StringAttribute{
				Description: "The name of the role to lookup.",
				Optional:    true,
				Validators:  []validator.String{validators.Name()},
			},
			"parent": schema.StringAttribute{
				Description: "The UIDP of the group in which to lookup the named role.",
				Optional:    true,
				Validators:  []validator.String{validators.UIDP(true /* allowRootSentinel */)},
			},
			"items": schema.ListNestedAttribute{
				Description: "Roles matched by the data source's filter.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Description: "The UIDP of this role.",
							Computed:    true,
						},
						"name": schema.StringAttribute{
							Description: "The name of this role.",
							Computed:    true,
						},
						"description": schema.StringAttribute{
							Description: "The description of this role.",
							Computed:    true,
						},
						"capabilities": schema.ListAttribute{
							Description: "The capabilities granted to this role.",
							Computed:    true,
							ElementType: types.StringType,
						},
					},
				},
			},
		},
	}
}

// Read refreshes the Terraform state with the latest data.
func (d *roleDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data roleDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, "read role data-source request", map[string]any{"input-params": data.InputParams()})

	all, err := d.prov.client.IAM().Roles().List(ctx, &iam.RoleFilter{
		Id:     data.ID.ValueString(),
		Name:   data.Name.ValueString(),
		Parent: data.Parent.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to list roles"))
		return
	}

	for _, role := range all.GetItems() {
		caps, diags := types.ListValueFrom(ctx, types.StringType, role.Capabilities)
		// Collect returned warnings/errors.
		resp.Diagnostics.Append(diags...)
		if diags.HasError() {
			// Don't return a role if errors encountered converting the capabilities.
			// This /shouldn't/ happen since the caps are coming from the API.
			tflog.Error(ctx, "failed to convert capabilities to basetypes.ListValue", map[string]any{"caps": role.Capabilities})
			continue
		}

		data.Items = append(data.Items, &roleModel{
			ID:           types.StringValue(role.Id),
			Name:         types.StringValue(role.Name),
			Description:  types.StringValue(role.Description),
			Capabilities: caps,
		})
	}
	// Role wasn't found, or was deleted outside Terraform
	if len(all.GetItems()) == 0 {
		resp.Diagnostics.Append(dataNotFound("role", "" /* extra */, data))
		return
	} else if d.prov.testing {
		// Set the ID on roleDataSourceModel for acceptance tests.
		// https://developer.hashicorp.com/terraform/tutorials/providers-plugin-framework/providers-plugin-framework-acceptance-testing#implement-data-source-id-attribute
		data.ID = types.StringValue("placeholder")
	}

	// Set state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
