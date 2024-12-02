/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-log/tflog"

	registry "chainguard.dev/sdk/proto/platform/registry/v1"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ datasource.DataSource              = &versionsDataSource{}
	_ datasource.DataSourceWithConfigure = &versionsDataSource{}
)

// NewVersionsDataSource is a helper function to simplify the provider implementation.
func NewVersionsDataSource() datasource.DataSource {
	return &versionsDataSource{}
}

// versionsDataSource is the data source implementation.
type versionsDataSource struct {
	dataSource
}

type versionsDataSourceModel struct {
	Package types.String `tfsdk:"package"`

	Versions   *versionsDataSourceProtoModel                `tfsdk:"versions"`
	VersionMap map[string]versionsDataSourceVersionMapModel `tfsdk:"version_map"`
}

// versionsDataSourceProtoModel is the schema for the "proto" version
// achieved through the versions proto. This is provided for backwards
// compatibility.
type versionsDataSourceProtoModel struct {
	GracePeriodMonths    int64                                      `tfsdk:"grace_period_months"`
	LastUpdatedTimestamp string                                     `tfsdk:"last_updated_timestamp"`
	LatestVersion        string                                     `tfsdk:"latest_version"`
	EolVersions          []*versionsDataSourceProtoEolVersionsModel `tfsdk:"eol_versions"`
	Versions             []*versionsDataSourceProtoVersionsModel    `tfsdk:"versions"`
}

type versionsDataSourceProtoEolVersionsModel struct {
	EolDate     string `tfsdk:"eol_date"`
	Exists      bool   `tfsdk:"exists"`
	ReleaseDate string `tfsdk:"release_date"`
	Version     string `tfsdk:"version"`
}

type versionsDataSourceProtoVersionsModel struct {
	Exists      bool   `tfsdk:"exists"`
	ReleaseDate string `tfsdk:"release_date"`
	Version     string `tfsdk:"version"`
}

// versionsDataSourceVersionMapModel is the schema for the "legacy" version
// achieved through the versions module. This is provided for backwards
// compatibility.
type versionsDataSourceVersionMapModel struct {
	Eol         bool   `tfsdk:"eol"`
	EolDate     string `tfsdk:"eol_date"`
	Exists      bool   `tfsdk:"exists"`
	Fips        bool   `tfsdk:"fips"`
	IsLatest    bool   `tfsdk:"is_latest"`
	Lts         string `tfsdk:"lts"`
	Main        string `tfsdk:"main"`
	ReleaseDate string `tfsdk:"release_date"`
	Version     string `tfsdk:"version"`
}

func (m versionsDataSourceModel) InputParams() string {
	return fmt.Sprintf("[package=%s]", m.Package)
}

// Metadata returns the data source type name.
func (d *versionsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_versions"
}

func (d *versionsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.configure(ctx, req, resp)
}

// Schema defines the schema for the data source.
func (d *versionsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lookup an identity with the given issuer and subject.",
		Attributes: map[string]schema.Attribute{
			"package": schema.StringAttribute{
				Description: "The name of the package to lookup.",
				Optional:    true,
			},
			"versions": schema.SingleNestedAttribute{
				Description: "The versions output of the package.",
				Computed:    true,
				Attributes: map[string]schema.Attribute{
					"grace_period_months": schema.Int64Attribute{
						Description: "The grace period in months.",
						Computed:    true,
					},
					"last_updated_timestamp": schema.StringAttribute{
						Description: "The last updated timestamp.",
						Computed:    true,
					},
					"latest_version": schema.StringAttribute{
						Description: "The latest version.",
						Computed:    true,
					},
					"eol_versions": schema.ListNestedAttribute{
						Description: "The eol versions.",
						Computed:    true,
						NestedObject: schema.NestedAttributeObject{
							Attributes: map[string]schema.Attribute{
								"eol_date": schema.StringAttribute{
									Description: "The eol date.",
									Computed:    true,
								},
								"exists": schema.BoolAttribute{
									Description: "Whether the version exists.",
									Computed:    true,
								},
								"release_date": schema.StringAttribute{
									Description: "The release date.",
									Computed:    true,
								},
								"version": schema.StringAttribute{
									Description: "The version.",
									Computed:    true,
								},
							},
						},
					},
					"versions": schema.ListNestedAttribute{
						Description: "The versions.",
						Computed:    true,
						NestedObject: schema.NestedAttributeObject{
							Attributes: map[string]schema.Attribute{
								"exists": schema.BoolAttribute{
									Description: "Whether the version exists.",
									Computed:    true,
								},
								"release_date": schema.StringAttribute{
									Description: "The release date.",
									Computed:    true,
								},
								"version": schema.StringAttribute{
									Description: "The version.",
									Computed:    true,
								},
							},
						},
					},
				},
			},
			"version_map": schema.MapNestedAttribute{
				Description: "The version map.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"eol": schema.BoolAttribute{
							Description: "Whether the version is eol.",
							Computed:    true,
						},
						"eol_date": schema.StringAttribute{
							Description: "The eol date.",
							Computed:    true,
						},
						"exists": schema.BoolAttribute{
							Description: "Whether the version exists.",
							Computed:    true,
						},
						"fips": schema.BoolAttribute{
							Description: "Whether the version is fips.",
							Computed:    true,
						},
						"is_latest": schema.BoolAttribute{
							Description: "Whether the version is the latest.",
							Computed:    true,
						},
						"lts": schema.StringAttribute{
							Description: "The lts version.",
							Computed:    true,
						},
						"main": schema.StringAttribute{
							Description: "The main version.",
							Computed:    true,
						},
						"release_date": schema.StringAttribute{
							Description: "The release date.",
							Computed:    true,
						},
						"version": schema.StringAttribute{
							Description: "The version.",
							Computed:    true,
						},
					},
				},
			},
		},
	}
}

// Read refreshes the Terraform state with the latest data.
func (d *versionsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data versionsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, "read identity data-source request", map[string]interface{}{"config": data})

	vreq := &registry.PackageVersionMetadataRequest{
		Package: data.Package.ValueString(),
	}

	v, err := d.prov.client.Registry().Registry().GetPackageVersionMetadata(ctx, vreq)
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to list package versions"))
		return
	}

	raw, err := json.Marshal(v)
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to marshal package version"))
		return
	}

	var vproto *versionsDataSourceProtoModel
	if err := json.Unmarshal(raw, &vproto); err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to unmarshal package version"))
		return
	}

	data.Versions = vproto

	// everything below is for backwards compatibility with the versions module

	vmap := make(map[string]versionsDataSourceVersionMapModel)

	for i, pv := range vproto.Versions {
		vname := data.Package.ValueString() + "-" + pv.Version

		model := versionsDataSourceVersionMapModel{
			Eol:         false,
			EolDate:     "",
			Exists:      pv.Exists,
			Fips:        false,
			IsLatest:    false,
			Lts:         "",
			Main:        vname,
			ReleaseDate: pv.ReleaseDate,
			Version:     pv.Version,
		}

		if i == 0 {
			model.IsLatest = true
		}

		vmap[vname] = model
	}

	for _, pv := range vproto.EolVersions {
		if !pv.Exists {
			continue
		}

		vname := data.Package.ValueString() + "-" + pv.Version
		model := versionsDataSourceVersionMapModel{
			Eol:         true,
			EolDate:     pv.EolDate,
			Exists:      pv.Exists,
			Fips:        false,
			IsLatest:    false,
			Lts:         "",
			Main:        vname,
			ReleaseDate: pv.ReleaseDate,
			Version:     pv.Version,
		}

		vmap[vname] = model
	}

	data.VersionMap = vmap

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
