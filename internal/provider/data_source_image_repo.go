/*
Copyright 2025 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"fmt"
	"time"

	common "chainguard.dev/sdk/proto/platform/common/v1"
	registry "chainguard.dev/sdk/proto/platform/registry/v1"
	"github.com/chainguard-dev/terraform-provider-chainguard/internal/validators"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ datasource.DataSource              = &imageRepoDataSource{}
	_ datasource.DataSourceWithConfigure = &imageRepoDataSource{}
)

func NewImageRepoDataSource() datasource.DataSource {
	return &imageRepoDataSource{}
}

type imageRepoDataSource struct {
	dataSource
}

type imageRepoDataSourceModel struct {
	ID       types.String      `tfsdk:"id"`
	Name     types.String      `tfsdk:"name"`
	ParentID types.String      `tfsdk:"parent_id"`
	Items    []*imageRepoModel `tfsdk:"items"`
}

func (d imageRepoDataSourceModel) InputParams() string {
	return fmt.Sprintf("[id=%s, name=%s, parent_id=%s]", d.ID, d.Name, d.ParentID)
}

type imageRepoModel struct {
	ID         types.String `tfsdk:"id"`
	Name       types.String `tfsdk:"name"`
	Bundles    types.List   `tfsdk:"bundles"`
	Readme     types.String `tfsdk:"readme"`
	SyncConfig *syncConfig  `tfsdk:"sync_config"`
	Tier       types.String `tfsdk:"tier"`
	Aliases    types.List   `tfsdk:"aliases"`
	ActiveTags types.List   `tfsdk:"active_tags"`
}

// Metadata returns the data source type name.
func (d *imageRepoDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_image_repo"
}

func (d *imageRepoDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.configure(ctx, req, resp)
}

// Schema defines the schema for the data source.
func (d *imageRepoDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lookup a repo/repos with the given name.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The exact UIDP of the repository to look up.",
				Optional:    true,
				Validators:  []validator.String{validators.UIDP(false /* allowRootSentinel */)},
			},
			"name": schema.StringAttribute{
				Description: "The name of the repository to lookup.",
				Optional:    true,
				Validators:  []validator.String{validators.Name()},
			},
			"parent_id": schema.StringAttribute{
				Description: "The UIDP of the group in which to lookup the repo.",
				Optional:    true,
				Validators:  []validator.String{validators.UIDP(true /* allowRootSentinel */)},
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
							Description: "List of bundles associated with this repo (a-z freeform keywords for sales purposes).",
							Optional:    true,
							ElementType: types.StringType,
							Validators: []validator.List{
								listvalidator.ValueStringsAre(validators.ValidateStringFuncs(validBundlesValue)),
							},
						},
						"readme": schema.StringAttribute{
							Description: "The README for this repo.",
							Optional:    true,
							Validators: []validator.String{
								validators.ValidateStringFuncs(validReadmeValue),
							},
						},
						"tier": schema.StringAttribute{
							Description: "Image tier associated with this repo.",
							Optional:    true,
							Validators: []validator.String{
								validators.ValidateStringFuncs(validTierValue),
							},
						},
						"sync_config": schema.ObjectAttribute{
							Optional: true,
							AttributeTypes: map[string]attr.Type{
								"source":       types.StringType,
								"expiration":   types.StringType,
								"unique_tags":  types.BoolType,
								"grace_period": types.BoolType,
								"google":       types.StringType,
								"amazon":       types.StringType,
								"azure":        types.StringType,
								"apko_overlay": types.StringType,
							},
						},
						"aliases": schema.ListAttribute{
							Description: "Known aliases for a given image.",
							Optional:    true,
							ElementType: types.StringType,
							Validators: []validator.List{
								listvalidator.ValueStringsAre(validators.ValidateStringFuncs(validAliasesValue)),
							},
						},
						"active_tags": schema.ListAttribute{
							Description: "List of active tags for this repo.",
							Optional:    true,
							ElementType: types.StringType,
							Validators: []validator.List{
								listvalidator.ValueStringsAre(validators.ValidateStringFuncs(validTagsValue)),
							},
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
	tflog.Info(ctx, "read imageRepo data-source request", map[string]any{"input-params": data.InputParams()})
	filter := &registry.RepoFilter{}
	if !data.ID.IsNull() {
		filter.Id = data.ID.ValueString()
	}
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
	for _, repo := range items.GetItems() {
		bundles, diags := types.ListValueFrom(ctx, types.StringType, repo.GetBundles())
		// Collect returned warnings/errors.
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
				GracePeriod: types.BoolValue(repo.GetSyncConfig().GetGracePeriod()),
				Google:      types.StringValue(repo.GetSyncConfig().GetGoogle()),
				Amazon:      types.StringValue(repo.GetSyncConfig().GetAmazon()),
				Azure:       types.StringValue(repo.GetSyncConfig().GetAzure()),
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
	if len(items.GetItems()) == 0 {
		resp.Diagnostics.Append(dataNotFound("repo", "check your input" /* extra */, data))
		return
	} else if len(items.GetItems()) == 1 {
		data.ID = types.StringValue(items.GetItems()[0].GetId())
	} else if d.prov.testing {
		// Set the ID on imageRepoModel for acceptance tests.
		// https://developer.hashicorp.com/terraform/tutorials/providers-plugin-framework/providers-plugin-framework-acceptance-testing#implement-data-source-id-attribute
		data.ID = types.StringValue("placeholder")
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
