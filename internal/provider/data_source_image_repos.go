/*
Copyright 2025 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	common "chainguard.dev/sdk/proto/platform/common/v1"
	registry "chainguard.dev/sdk/proto/platform/registry/v1"
	"github.com/chainguard-dev/terraform-provider-chainguard/internal/validators"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ datasource.DataSource              = &imageReposDataSource{}
	_ datasource.DataSourceWithConfigure = &imageReposDataSource{}
)

func NewImageReposDataSource() datasource.DataSource {
	return &imageReposDataSource{}
}

type imageReposDataSource struct {
	dataSource
}

type imageReposDataSourceModel struct {
	ID       types.String      `tfsdk:"id"`
	Name     types.String      `tfsdk:"name"`
	ParentID types.String      `tfsdk:"parent_id"`
	Items    []*imageRepoModel `tfsdk:"items"`
}

func (d imageReposDataSourceModel) InputParams() string {
	return fmt.Sprintf("[name=%s, parent_id=%s]", d.Name, d.ParentID)
}

// Metadata returns the data source type name.
func (d *imageReposDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_image_repos"
}

func (d *imageReposDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.configure(ctx, req, resp)
}

// Schema defines the schema for the data source.
func (d *imageReposDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lookup multiple image repositories with optional filtering. Returns zero or more repositories.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Placeholder identifier for the data source.",
				Computed:    true,
			},
			"name": schema.StringAttribute{
				Description: "Filter repositories by name (supports partial matching).",
				Optional:    true,
				Validators:  []validator.String{validators.Name()},
			},
			"parent_id": schema.StringAttribute{
				Description: "Filter repositories by parent group UIDP.",
				Optional:    true,
				Validators:  []validator.String{validators.UIDP(true /* allowRootSentinel */)},
			},
			"items": schema.ListNestedAttribute{
				Description: "List of image repositories matching the filter criteria.",
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
							Description: "List of bundles associated with this repo (a-z freeform keywords for sales purposes).",
							Computed:    true,
							ElementType: types.StringType,
						},
						"readme": schema.StringAttribute{
							Description: "The README for this repo.",
							Computed:    true,
						},
						"tier": schema.StringAttribute{
							Description: "Image tier associated with this repo.",
							Computed:    true,
						},
						"sync_config": schema.ObjectAttribute{
							Computed: true,
							AttributeTypes: map[string]attr.Type{
								"source":       types.StringType,
								"expiration":   types.StringType,
								"unique_tags":  types.BoolType,
								"apko_overlay": types.StringType,
							},
						},
						"aliases": schema.ListAttribute{
							Description: "Known aliases for a given image.",
							Computed:    true,
							ElementType: types.StringType,
						},
						"active_tags": schema.ListAttribute{
							Description: "List of active tags for this repo.",
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
func (d *imageReposDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data imageReposDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "read imageRepos data-source request", map[string]interface{}{"input-params": data.InputParams()})

	filter := &registry.RepoFilter{}
	if !data.Name.IsNull() {
		filter.Name = data.Name.ValueString()
	}
	if !data.ParentID.IsNull() {
		filter.Uidp = &common.UIDPFilter{
			ChildrenOf: data.ParentID.ValueString(),
		}
	}

	items, err := d.prov.client.Registry().Registry().ListRepos(ctx, filter)
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to list repos"))
		return
	}

	// Initialize items slice to ensure it's not nil even if no repos found
	data.Items = make([]*imageRepoModel, 0)

	for _, repo := range items.GetItems() {
		bundles, diags := types.ListValueFrom(ctx, types.StringType, repo.GetBundles())
		resp.Diagnostics.Append(diags...)
		if diags.HasError() {
			tflog.Error(ctx, "failed to convert bundles to basetypes.ListValue", map[string]any{"bundles": repo.GetBundles()})
			continue
		}

		aliases, diags := types.ListValueFrom(ctx, types.StringType, repo.GetAliases())
		resp.Diagnostics.Append(diags...)
		if diags.HasError() {
			tflog.Error(ctx, "failed to convert aliases to basetypes.ListValue", map[string]any{"aliases": repo.GetAliases()})
			continue
		}

		activeTags, diags := types.ListValueFrom(ctx, types.StringType, repo.GetActiveTags())
		resp.Diagnostics.Append(diags...)
		if diags.HasError() {
			tflog.Error(ctx, "failed to convert active_tags to basetypes.ListValue", map[string]any{"active_tags": repo.GetActiveTags()})
			continue
		}

		var sc *syncConfig
		if repo.SyncConfig != nil {
			sc = &syncConfig{
				Source:      types.StringValue(repo.GetSyncConfig().GetSource()),
				Expiration:  types.StringValue(repo.GetSyncConfig().GetExpiration().AsTime().Format(time.RFC3339)),
				UniqueTags:  types.BoolValue(repo.GetSyncConfig().GetUniqueTags()),
				ApkoOverlay: types.StringValue(repo.GetSyncConfig().GetApkoOverlay()),
			}
		}

		data.Items = append(data.Items, &imageRepoModel{
			ID:         types.StringValue(repo.GetId()),
			Name:       types.StringValue(repo.GetName()),
			Bundles:    bundles,
			Readme:     types.StringValue(repo.GetReadme()),
			SyncConfig: sc,
			Tier:       types.StringValue(repo.GetCatalogTier().String()),
			Aliases:    aliases,
			ActiveTags: activeTags,
		})
	}

	// Generate a unique ID based on the filter parameters
	idHash := sha256.Sum256([]byte(data.InputParams()))
	data.ID = types.StringValue(fmt.Sprintf("image_repos-%x", idHash[:8]))

	tflog.Info(ctx, "imageRepos data-source completed", map[string]interface{}{"items_found": len(data.Items)})

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
