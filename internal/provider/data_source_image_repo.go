/*
Copyright 2025 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"fmt"
	"strings"
	"time"

	regv2 "chainguard.dev/sdk/proto/chainguard/platform/registry/v2beta1"
	common "chainguard.dev/sdk/proto/platform/common/v1"
	"github.com/chainguard-dev/terraform-provider-chainguard/internal/validators"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
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
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Bundles     types.List   `tfsdk:"bundles"`
	Readme      types.String `tfsdk:"readme"`
	SyncConfig  *syncConfig  `tfsdk:"sync_config"`
	Tier        types.String `tfsdk:"tier"`
	Description types.String `tfsdk:"description"`
	Aliases     types.List   `tfsdk:"aliases"`
	ActiveTags  types.List   `tfsdk:"active_tags"`
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
						"description": schema.StringAttribute{
							Description: "The short description of this repo.",
							Computed:    true,
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

	if !data.ID.IsNull() && data.ID.ValueString() != "" {
		repo, err := d.prov.clientV2.Registry().ReposService().GetRepo(ctx, &regv2.GetRepoRequest{
			Uid: data.ID.ValueString(),
		})
		if err != nil {
			resp.Diagnostics.Append(errorToDiagnostic(err, "failed to get repo"))
			return
		}
		repoModel, diags := repoToModel(ctx, repo)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		data.Items = append(data.Items, repoModel)
		data.ID = types.StringValue(repo.GetUid())
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
		return
	}

	listReq := &regv2.ListReposRequest{}
	if !data.Name.IsNull() {
		name := data.Name.ValueString()
		listReq.Name = &name
	}
	if !data.ParentID.IsNull() {
		listReq.Uidp = &common.UIDPFilter{
			ChildrenOf: data.ParentID.ValueString(),
		}
	}
	repos, err := d.prov.clientV2.Registry().ListReposAll(ctx, listReq)
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to list repos"))
		return
	}
	for _, repo := range repos {
		repoModel, diags := repoToModel(ctx, repo)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		data.Items = append(data.Items, repoModel)
	}
	if len(repos) == 0 {
		resp.Diagnostics.Append(dataNotFound("repo", "check your input" /* extra */, data))
		return
	} else if len(repos) == 1 {
		data.ID = types.StringValue(repos[0].GetUid())
	} else if d.prov.testing {
		data.ID = types.StringValue("placeholder")
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func repoToModel(ctx context.Context, repo *regv2.Repo) (*imageRepoModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	bundles, d := types.ListValueFrom(ctx, types.StringType, repo.GetBundles())
	diags.Append(d...)
	if d.HasError() {
		return nil, diags
	}

	aliases, d := types.ListValueFrom(ctx, types.StringType, repo.GetAliases())
	diags.Append(d...)
	if d.HasError() {
		return nil, diags
	}

	activeTags, d := types.ListValueFrom(ctx, types.StringType, repo.GetActiveTags())
	diags.Append(d...)
	if d.HasError() {
		return nil, diags
	}

	var sc *syncConfig
	if repo.SyncConfig != nil {
		expiration := types.StringNull()
		if repo.GetSyncConfig().GetExpirationTime() != nil && !repo.GetSyncConfig().GetExpirationTime().AsTime().IsZero() {
			expiration = types.StringValue(repo.GetSyncConfig().GetExpirationTime().AsTime().Format(time.RFC3339))
		}
		sc = &syncConfig{
			Source:      types.StringValue(repo.GetSyncConfig().GetSource()),
			Expiration:  expiration,
			UniqueTags:  types.BoolValue(repo.GetSyncConfig().GetUniqueTags()),
			GracePeriod: types.BoolValue(repo.GetSyncConfig().GetGracePeriod()),
			Google:      types.StringValue(repo.GetSyncConfig().GetGoogle()),
			Amazon:      types.StringValue(repo.GetSyncConfig().GetAmazon()),
			Azure:       types.StringValue(repo.GetSyncConfig().GetAzure()),
			ApkoOverlay: types.StringValue(repo.GetSyncConfig().GetApkoOverlay()),
		}
	}

	return &imageRepoModel{
		ID:          types.StringValue(repo.GetUid()),
		Name:        types.StringValue(repo.GetName()),
		Bundles:     bundles,
		Readme:      types.StringValue(repo.GetReadme()),
		SyncConfig:  sc,
		Tier:        types.StringValue(catalogTierString(repo.GetCatalogTier())),
		Description: types.StringValue(repo.GetDescription()),
		Aliases:     aliases,
		ActiveTags:  activeTags,
	}, diags
}

// catalogTierString normalizes v2beta1 CatalogTier enum names to match
// the v1 format used elsewhere in the provider (e.g., "APPLICATION" not
// "CATALOG_TIER_APPLICATION").
func catalogTierString(tier regv2.CatalogTier) string {
	s := tier.String()
	return strings.TrimPrefix(s, "CATALOG_TIER_")
}
