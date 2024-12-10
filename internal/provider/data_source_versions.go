/*
Copyright 2024 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/hashicorp/terraform-plugin-log/tflog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	registry "chainguard.dev/sdk/proto/platform/registry/v1"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
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
	Variant types.String `tfsdk:"variant"`

	Versions    *versionsDataSourceProtoModel                `tfsdk:"versions"`
	VersionMap  map[string]versionsDataSourceVersionMapModel `tfsdk:"version_map"`
	OrderedKeys []string                                     `tfsdk:"ordered_keys"`
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
	EolBroken   bool   `tfsdk:"eol_broken"`
	Exists      bool   `tfsdk:"exists"`
	Fips        bool   `tfsdk:"fips"`
	ReleaseDate string `tfsdk:"release_date"`
	Version     string `tfsdk:"version"`
}

type versionsDataSourceProtoVersionsModel struct {
	Exists      bool   `tfsdk:"exists"`
	Fips        bool   `tfsdk:"fips"`
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
	return fmt.Sprintf("[package=%s, variant=%s]", m.Package, m.Variant)
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
		Description: "Lookup a package version stream",
		Attributes: map[string]schema.Attribute{
			"package": schema.StringAttribute{
				Description: "The name of the package to lookup.",
				Required:    true,
			},
			"variant": schema.StringAttribute{
				Description: "A package variant (e.g. fips).",
				Optional:    true,
				Validators:  []validator.String{Variant()},
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
								"fips": schema.BoolAttribute{
									Description: "Whether the FIPS version exists.",
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
								"fips": schema.BoolAttribute{
									Description: "Whether the FIPS version exists.",
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
			"ordered_keys": schema.ListAttribute{
				Description: "A list of keys as they appear in the versions output, sorted semantically.",
				Computed:    true,
				ElementType: types.StringType,
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
	tflog.Info(ctx, "read versions data-source request", map[string]interface{}{"config": data})

	pkg := data.Package.ValueString()
	variant := data.Variant.ValueString()

	vproto, vmap, orderedKeys, diagnostic := calculate(ctx, d.prov.client.Registry().Registry(), pkg, variant)
	if diagnostic != nil {
		resp.Diagnostics.Append(diagnostic)
		return
	}

	data.Versions = vproto
	data.VersionMap = vmap
	data.OrderedKeys = orderedKeys

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Responsible for the generation of all calculated fields (i.e. Versions, VersionMap, OrderedKeys).
func calculate(ctx context.Context, client registry.RegistryClient, pkg string, variant string) (*versionsDataSourceProtoModel, map[string]versionsDataSourceVersionMapModel, []string, diag.Diagnostic) {
	// If variant provided (i.e. "fips"), modify the key names to include it
	key := pkg
	fips := false
	if variant := variant; variant != "" {
		// TODO: allow for more variants than just "fips"?
		if variant != "fips" {
			return nil, nil, nil, errorToDiagnostic(fmt.Errorf("invalid variant: %s", variant), "must be \"fips\"")
		}
		key = fmt.Sprintf("%s-%s", key, variant)
		fips = variant == "fips"
	}

	vreq := &registry.PackageVersionMetadataRequest{
		Package: pkg,
	}

	v, err := client.GetPackageVersionMetadata(ctx, vreq)
	if err != nil {
		if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
			// At this point, the requested version stream has not been found,
			// so we return early with default empty structures
			// TODO: disable/enable this via some input such as "must_resolve: true"?
			vproto := &versionsDataSourceProtoModel{
				GracePeriodMonths:    0,
				LastUpdatedTimestamp: "",
				LatestVersion:        "",
				EolVersions:          []*versionsDataSourceProtoEolVersionsModel{},
				Versions: []*versionsDataSourceProtoVersionsModel{
					{
						Exists:      true,
						Fips:        fips,
						ReleaseDate: "",
						Version:     "",
					},
				},
			}
			vmap := map[string]versionsDataSourceVersionMapModel{
				key: {
					Eol:         false,
					EolDate:     "",
					Exists:      true,
					Fips:        fips,
					IsLatest:    true,
					Lts:         "",
					Main:        key,
					ReleaseDate: "",
					Version:     "",
				},
			}
			orderedKeys := []string{key}
			return vproto, vmap, orderedKeys, nil
		}
		return nil, nil, nil, errorToDiagnostic(err, "failed to list package versions")
	}

	raw, err := json.Marshal(v)
	if err != nil {
		return nil, nil, nil, errorToDiagnostic(err, "failed to marshal package version")
	}

	var vproto *versionsDataSourceProtoModel
	if err := json.Unmarshal(raw, &vproto); err != nil {
		return nil, nil, nil, errorToDiagnostic(err, "failed to unmarshal package version")
	}

	// everything below is for backwards compatibility with the versions module

	vmap := make(map[string]versionsDataSourceVersionMapModel)

	orderedKeys := []string{}

	latestAssigned := false

	for _, pv := range vproto.Versions {
		// Non-FIPS and doesnt exist (via "exists" bool)? Do not use
		// FIPS and doesnt exist (via "fips" bool)? Do not use
		if (!fips && !pv.Exists) || (fips && !pv.Fips) {
			continue
		}

		vname := key + "-" + pv.Version

		model := versionsDataSourceVersionMapModel{
			Eol:         false,
			EolDate:     "",
			Exists:      pv.Exists,
			Fips:        pv.Fips,
			IsLatest:    false,
			Lts:         "",
			Main:        vname,
			ReleaseDate: pv.ReleaseDate,
			Version:     pv.Version,
		}

		if !latestAssigned {
			model.IsLatest = true
			latestAssigned = true
		}

		vmap[vname] = model
		orderedKeys = append(orderedKeys, vname)
	}

	for _, pv := range vproto.EolVersions {
		// Non-FIPS and doesnt exist (via "exists" bool)? Do not use
		// FIPS and doesnt exist (via "fips" bool)? Do not use
		// Marked as broken (via "eolBroken" bool)? Do not use
		if (!fips && !pv.Exists) || (fips && !pv.Fips) || pv.EolBroken {
			continue
		}

		insideEOLGracePeriodWindow, err := checkEOLGracePeriodWindow(pv.EolDate, vproto.GracePeriodMonths)
		if err != nil {
			return nil, nil, nil, errorToDiagnostic(err, "failed to calculate EOL grace period")
		}
		if !insideEOLGracePeriodWindow {
			continue
		}

		vname := key + "-" + pv.Version
		model := versionsDataSourceVersionMapModel{
			Eol:         true,
			EolDate:     pv.EolDate,
			Exists:      pv.Exists,
			Fips:        pv.Fips,
			IsLatest:    false,
			Lts:         "",
			Main:        vname,
			ReleaseDate: pv.ReleaseDate,
			Version:     pv.Version,
		}

		if !latestAssigned {
			model.IsLatest = true
			latestAssigned = true
		}

		vmap[vname] = model
		orderedKeys = append(orderedKeys, vname)
	}

	// We want the latest version at the end of this list
	slices.Reverse(orderedKeys)

	return vproto, vmap, orderedKeys, nil
}

func checkEOLGracePeriodWindow(eolDate string, gracePeriodMonths int64) (bool, error) {
	t, err := time.Parse(time.DateOnly, eolDate)
	if err != nil {
		return false, err
	}
	// Take the parsed EOL date, fast forward it to X months in the future
	// and ensure that it is greater than or equal to right now
	t = t.AddDate(0, int(gracePeriodMonths), 0)
	return t.Compare(time.Now().UTC()) >= 0, nil
}

// Variant validates the string value is a valid variant.
func Variant() validator.String {
	return variantVal{}
}

type variantVal struct{}

func (v variantVal) Description(_ context.Context) string {
	return "Check that the given string is a valid variant."
}

func (v variantVal) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v variantVal) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	// Attributes may be optional, and thus null, which should not fail validation.
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	s := req.ConfigValue.ValueString()
	// Empty string means caller has not set variant, do not fail validation
	if s == "" {
		return
	}
	// TODO: allow for more variants than just "fips"?
	if s != "fips" {
		resp.Diagnostics.AddError("failed variant validation",
			fmt.Sprintf("\"%s\" is not a valid variant (must be \"fips\")", s))
	}
}
