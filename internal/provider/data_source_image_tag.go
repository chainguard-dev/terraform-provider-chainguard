/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"google.golang.org/protobuf/types/known/timestamppb"

	common "chainguard.dev/sdk/proto/platform/common/v1"
	registry "chainguard.dev/sdk/proto/platform/registry/v1"
	"github.com/chainguard-dev/terraform-provider-chainguard/internal/protoutil"
	"github.com/chainguard-dev/terraform-provider-chainguard/internal/validators"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ datasource.DataSource              = &imageTagDataSource{}
	_ datasource.DataSourceWithConfigure = &imageTagDataSource{}
)

// NewImageTagDataSource is a helper function to simplify the provider implementation.
func NewImageTagDataSource() datasource.DataSource {
	return &imageTagDataSource{}
}

// imageTagDataSource is the data source implementation.
type imageTagDataSource struct {
	dataSource
}

type imageTagDataSourceModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	RepoID           types.String `tfsdk:"repo_id"`
	UpdatedSince     types.String `tfsdk:"updated_since"`
	ExcludeDates     types.Bool   `tfsdk:"exclude_dates"`
	ExcludeEpochs    types.Bool   `tfsdk:"exclude_epochs"`
	ExcludeReferrers types.Bool   `tfsdk:"exclude_referrers"`

	Items []*tagModel `tfsdk:"items"`
}

func (d imageTagDataSourceModel) InputParams() string {
	return fmt.Sprintf("[id=%s, name=%s, repo_id=%s, updated_since=%s, exclude_dates=%t, exclude_epochs=%t, exclude_referrers=%t]",
		d.ID, d.Name, d.RepoID, d.UpdatedSince, d.ExcludeDates.ValueBool(), d.ExcludeEpochs.ValueBool(), d.ExcludeReferrers.ValueBool())
}

type tagModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Digest      types.String `tfsdk:"digest"`
	LastUpdated types.String `tfsdk:"last_updated"`
	Deprecated  types.Bool   `tfsdk:"deprecated"`
	Bundles     types.List   `tfsdk:"bundles"`
}

// Metadata returns the data source type name.
func (d *imageTagDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_image_tag"
}

func (d *imageTagDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.configure(ctx, req, resp)
}

// Schema defines the schema for the data source.
func (d *imageTagDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lookup a tag for a given repo or set of repos.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The exact UIDP of the tag to lookup.",
				Optional:    true,
				Validators:  []validator.String{validators.UIDP(false /* allowRootSentinel */)},
			},
			"name": schema.StringAttribute{
				Description: "The name of the tag to lookup.",
				Optional:    true,
				Validators:  []validator.String{validators.Name()},
			},
			"repo_id": schema.StringAttribute{
				Description: "The UIDP of the repo in which to lookup the named tag.",
				Optional:    true,
				Validators:  []validator.String{validators.UIDP(false /* allowRootSentinel */)},
			},
			"updated_since": schema.StringAttribute{
				Description: "The timestamp after which returned tags were updated, in RFC3339 format (2006-01-02T15:04:05Z07:00).",
				Optional:    true,
				Validators:  []validator.String{validators.ValidateStringFuncs(checkRFC3339)},
			},
			"exclude_dates": schema.BoolAttribute{
				Description: "Exclude tags of the form \"*-20yymmdd\".",
				Optional:    true,
			},
			"exclude_epochs": schema.BoolAttribute{
				Description: "Exclude tags of the form \"*-r[0-9]+\".",
				Optional:    true,
			},
			"exclude_referrers": schema.BoolAttribute{
				Description: "Exclude tags of the form \"sha256-*\".",
				Optional:    true,
			},
			"items": schema.ListNestedAttribute{
				Description: "Tags matched by the data source's filter.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Description: "The UIDP of this tag.",
							Computed:    true,
						},
						"name": schema.StringAttribute{
							Description: "The name of this tag.",
							Computed:    true,
						},
						"digest": schema.StringAttribute{
							Description: "The digest of the manifest with this tag.",
							Computed:    true,
						},
						"last_updated": schema.StringAttribute{
							Description: "Last time this tag was updated.",
							Computed:    true,
						},
						"deprecated": schema.BoolAttribute{
							Description: "True if the tag is deprecated.",
							Computed:    true,
						},
						"bundles": schema.ListAttribute{
							Description: "A collection of labels applied to this tag.",
							Computed:    true,
							ElementType: types.StringType,
						},
					},
				},
			},
		},
	}
}

// checkRFC3339 implements validators.ValidateStringFunc.
func checkRFC3339(raw string) error {
	_, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return fmt.Errorf("failed to parse %s: %w", raw, err)
	}
	return nil
}

// Read refreshes the Terraform state with the latest data.
func (d *imageTagDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data imageTagDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, "read tag data-source request", map[string]interface{}{"input-params": data.InputParams()})

	// Populate the filter with any included values.
	tf := &registry.TagFilter{
		Id:               data.ID.ValueString(),
		Name:             data.Name.ValueString(),
		ExcludeDates:     protoutil.DefaultBool(data.ExcludeDates, false),
		ExcludeEpochs:    protoutil.DefaultBool(data.ExcludeEpochs, false),
		ExcludeReferrers: protoutil.DefaultBool(data.ExcludeReferrers, false),
	}
	if !data.RepoID.IsNull() && data.RepoID.ValueString() != "" {
		tf.Uidp = &common.UIDPFilter{DescendantsOf: data.RepoID.ValueString()}
	}
	if !data.UpdatedSince.IsNull() && data.UpdatedSince.ValueString() != "" {
		t, err := time.Parse(time.RFC3339, data.UpdatedSince.ValueString())
		if err != nil {
			// This shouldn't happen due to validation.
			resp.Diagnostics.Append(errorToDiagnostic(err, "failed to parse update_since"))
			return
		}
		tf.UpdatedSince = timestamppb.New(t)
	}
	all, err := d.prov.client.Registry().Registry().ListTags(ctx, tf)
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to list tags"))
		return
	}

	for _, tag := range all.GetItems() {
		bundles, diags := types.ListValueFrom(ctx, types.StringType, tag.GetBundles())
		if diags.HasError() {
			// Failing to convert the bundles shouldn't happen, and doesn't
			// really affect the usefulness of returning the tag.
			// Record any diagnostics as warnings and move on.
			for _, d := range diags {
				resp.Diagnostics.AddWarning(d.Summary(), d.Detail())
			}
			tflog.Error(ctx, "failed to convert bundles to basetypes.ListValue", map[string]any{"bundles": tag.Bundles, "diags": diags})
		}

		data.Items = append(data.Items, &tagModel{
			ID:          types.StringValue(tag.Id),
			Name:        types.StringValue(tag.Name),
			Digest:      types.StringValue(tag.Digest),
			Deprecated:  types.BoolValue(tag.Deprecated),
			LastUpdated: types.StringValue(tag.LastUpdated.AsTime().Format(time.RFC3339)),
			Bundles:     bundles,
		})
	}
	// Role wasn't found, or was deleted outside Terraform
	if len(all.GetItems()) == 0 {
		resp.Diagnostics.Append(dataNotFound("tag", "" /* extra */, data))
		return
	} else if d.prov.testing {
		// Set the ID on imageTagDataSourceModel for acceptance tests.
		// https://developer.hashicorp.com/terraform/tutorials/providers-plugin-framework/providers-plugin-framework-acceptance-testing#implement-data-source-id-attribute
		data.ID = types.StringValue("placeholder")
	}

	// Set state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
