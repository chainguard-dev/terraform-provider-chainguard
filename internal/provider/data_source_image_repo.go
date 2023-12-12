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
	registry "chainguard.dev/sdk/proto/platform/registry/v1"
	"github.com/chainguard-dev/terraform-provider-chainguard/internal/validators"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ datasource.DataSource              = &imageRepoDataSource{}
	_ datasource.DataSourceWithConfigure = &imageRepoDataSource{}
)

// NewImageRepoDataSource is a helper function to simplify the provider implementation.
func NewImageRepoDataSource() datasource.DataSource {
	return &imageRepoDataSource{}
}

// imageRepoDataSource is the data source implementation.
type imageRepoDataSource struct {
	dataSource
}

type imageRepoDataSourceModel struct {
	ID       types.String `tfsdk:"id"`
	Name     types.String `tfsdk:"name"`
	ParentID types.String `tfsdk:"parent_id"`

	Items []*repoModel `tfsdk:"items"`
}

func (d imageRepoDataSourceModel) InputParams() string {
	return fmt.Sprintf("[id=%s, name=%s, parent_id=%s]", d.ID, d.Name, d.ParentID)
}

type repoModel struct {
	ID      types.String `tfsdk:"id"`
	Name    types.String `tfsdk:"name"`
	Bundles types.List   `tfsdk:"bundles"`
}

// Metadata returns the data source type name.
func (d *imageRepoDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_image_repo"
}

func (d *imageRepoDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.configure(ctx, req, resp)
}

// Schema defines the schema for the data source.
func (d *imageRepoDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lookup an image repo with the given name, id, or group membership.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The exact UIDP of the repo to lookup.",
				Optional:    true,
				Validators:  []validator.String{validators.UIDP(false /* allowRootSentinel */)},
			},
			"name": schema.StringAttribute{
				Description: "The name of the repo to lookup.",
				Optional:    true,
				Validators:  []validator.String{validators.Name()},
			},
			"parent_id": schema.StringAttribute{
				Description: "The UIDP of the group in which to lookup the named repo.",
				Optional:    true,
				Validators:  []validator.String{validators.UIDP(false /* allowRootSentinel */)},
			},
			"items": schema.ListNestedAttribute{
				Description: "Repos matched by the data source's filter.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Description: "The UIDP of this repo.",
							Computed:    true,
						},
						"name": schema.StringAttribute{
							Description: "The name of this repo.",
							Computed:    true,
						},
						"bundles": schema.ListAttribute{
							Description: "A collection of labels applied to this repo.",
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
func (d *imageRepoDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data imageRepoDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, "read repo data-source request", map[string]interface{}{"input-params": data.InputParams()})

	// Populate the filter with any included values.
	rf := &registry.RepoFilter{
		Id:   data.ID.ValueString(),
		Name: data.Name.ValueString(),
	}
	if !data.ParentID.IsNull() && data.ParentID.ValueString() != "" {
		rf.Uidp = &common.UIDPFilter{DescendantsOf: data.ParentID.ValueString()}
	}
	all, err := d.prov.client.Registry().Registry().ListRepos(ctx, rf)
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to list repos"))
		return
	}

	for _, repo := range all.GetItems() {
		bundles, diags := types.ListValueFrom(ctx, types.StringType, repo.GetBundles())
		if diags.HasError() {
			// Failing to convert the bundles shouldn't happen, and doesn't
			// really affect the usefulness of returning the repo name/ID still.
			// Record any diagnostics as warnings and move on.
			for _, d := range diags {
				resp.Diagnostics.AddWarning(d.Summary(), d.Detail())
			}
			tflog.Error(ctx, "failed to convert bundles to basetypes.ListValue", map[string]any{"bundles": repo.Bundles, "diags": diags})
		}

		data.Items = append(data.Items, &repoModel{
			ID:      types.StringValue(repo.Id),
			Name:    types.StringValue(repo.Name),
			Bundles: bundles,
		})
	}
	// Role wasn't found, or was deleted outside Terraform
	if len(all.GetItems()) == 0 {
		resp.Diagnostics.Append(dataNotFound("repo", "" /* extra */, data))
		return
	} else if d.prov.testing {
		// Set the ID on imageRepoDataSourceModel for acceptance tests.
		// https://developer.hashicorp.com/terraform/tutorials/providers-plugin-framework/providers-plugin-framework-acceptance-testing#implement-data-source-id-attribute
		data.ID = types.StringValue("placeholder")
	}

	// Set state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
