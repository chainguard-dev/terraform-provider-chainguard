/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"golang.org/x/exp/maps"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"chainguard.dev/api/proto/platform/tenant"
	"github.com/chainguard-dev/terraform-provider-chainguard/internal/protoutil"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ datasource.DataSource              = &clusterDiscoveryDataSource{}
	_ datasource.DataSourceWithConfigure = &clusterDiscoveryDataSource{}
)

// NewClusterDiscoveryDataSource is a helper function to simplify the provider implementation.
func NewClusterDiscoveryDataSource() datasource.DataSource {
	return &clusterDiscoveryDataSource{}
}

// clusterDiscoveryDataSource is the data source implementation.
type clusterDiscoveryDataSource struct {
	dataSource
}

type clusterDiscoveryDataSourceModel struct {
	ID        types.String               `tfsdk:"id"`
	Providers types.List                 `tfsdk:"providers"`
	Profiles  types.List                 `tfsdk:"profiles"`
	States    types.List                 `tfsdk:"states"`
	Results   []*clusterDiscoveryResults `tfsdk:"results"`
}

type clusterDiscoveryResults struct {
	Provider types.String                    `tfsdk:"provider"`
	Account  types.String                    `tfsdk:"account"`
	Location types.String                    `tfsdk:"location"`
	Name     types.String                    `tfsdk:"name"`
	State    []*clusterDiscoveryResultsState `tfsdk:"state"`
}

type clusterDiscoveryResultsState struct {
	State                    types.String `tfsdk:"state"`
	Reason                   types.String `tfsdk:"reason"`
	Steps                    types.List   `tfsdk:"steps"`
	Server                   types.String `tfsdk:"server"`
	CertificateAuthorityData types.String `tfsdk:"certificate_authority_data"`
	ID                       types.String `tfsdk:"id"`
	Profiles                 types.List   `tfsdk:"profiles"`
}

func (m clusterDiscoveryDataSourceModel) InputParams() string {
	return fmt.Sprintf("[id=%s, providers=%s, profiles=%s, states=%s]", m.ID, m.Providers, m.Profiles, m.States)
}

// Metadata returns the data source type name.
func (d *clusterDiscoveryDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cluster_discovery"
}

func (d *clusterDiscoveryDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.configure(ctx, req, resp)
}

// Schema defines the schema for the data source.
func (d *clusterDiscoveryDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	providers := maps.Keys(tenant.Cluster_Provider_value)
	states := maps.Keys(tenant.ClusterDiscoveryRequest_State_value)

	resp.Schema = schema.Schema{
		Description: "Potential clusters to install found via Enforce cluster discovery.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Exact UIDP of the IAM Group to use for impersonation when discovering clusters",
				Required:    true,
			},
			"providers": schema.ListAttribute{
				Description: fmt.Sprintf("List of provider types to check. Allowed value are %s", protoutil.EnumToQuotedString(tenant.Cluster_Provider_value)),
				Required:    true,
				ElementType: types.StringType,
				Validators: []validator.List{
					listvalidator.ValueStringsAre(stringvalidator.OneOf(providers...)),
				},
			},
			"profiles": schema.ListAttribute{
				Description: `List of profiles to verify compatibility with. Allowed values are "observer", "enforcer"`,
				Required:    true,
				ElementType: types.StringType,
				Validators: []validator.List{
					listvalidator.SizeAtLeast(1),
					listvalidator.ValueStringsAre(stringvalidator.OneOf("enforcer", "observer")),
				},
			},
			"states": schema.ListAttribute{
				Description: fmt.Sprintf(`A filter on the discovered cluster states to return. Allowed value are %s (if unspecified, this defaults to ELIGIBLE and ENROLLED)`, protoutil.EnumToQuotedString(tenant.ClusterDiscoveryRequest_State_value)),
				Optional:    true,
				ElementType: types.StringType,
				Validators: []validator.List{
					listvalidator.ValueStringsAre(stringvalidator.OneOf(states...)),
				},
			},
			"results": schema.ListNestedAttribute{
				Description: "Discovered clusters.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"provider": schema.StringAttribute{
							Description: "Cluster provider type",
							Computed:    true,
						},
						"account": schema.StringAttribute{
							Description: "Cloud account where cluster was discovered",
							Computed:    true,
						},
						"location": schema.StringAttribute{
							Description: "Cluster location",
							Computed:    true,
						},
						"name": schema.StringAttribute{
							Description: "Cluster name",
							Computed:    true,
						},
						"state": schema.ListNestedAttribute{
							Description: "Cluster state",
							Computed:    true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"state": schema.StringAttribute{
										Description: "State of discovered cluster",
										Computed:    true,
									},
									"reason": schema.StringAttribute{
										Description: "If state is unsupported this is why",
										Computed:    true,
										Optional:    true,
									},
									"steps": schema.ListAttribute{
										Description: "If state is needswork, this are remediation steps",
										Computed:    true,
										Optional:    true,
										ElementType: types.StringType,
									},
									"server": schema.StringAttribute{
										Description: "If state is eligible or enrolled, this is the cluster api-server URL",
										Computed:    true,
										Optional:    true,
									},
									"certificate_authority_data": schema.StringAttribute{
										Description: "If state is eligible or enrolled, this is the PEM encoded CA chain for the api-server",
										Computed:    true,
										Optional:    true,
									},
									"id": schema.StringAttribute{
										Description: "If state is enrolled, this is the UIDP of the cluster",
										Computed:    true,
										Optional:    true,
									},
									"profiles": schema.ListAttribute{
										Description: "If state is enrolled, these are the installed profiles",
										Computed:    true,
										Optional:    true,
										ElementType: types.StringType,
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

// Read refreshes the Terraform state with the latest data.
func (d *clusterDiscoveryDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data clusterDiscoveryDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, "read cluster discovery data-source request", map[string]interface{}{"config": data})

	providers := make([]tenant.Cluster_Provider, 0, len(data.Providers.Elements()))
	ss := make([]string, 0, len(providers))
	resp.Diagnostics.Append(data.Providers.ElementsAs(ctx, &ss, false /* allowUnhandled */)...)
	for _, provider := range ss {
		providers = append(providers, tenant.Cluster_Provider(tenant.Cluster_Provider_value[provider]))
	}

	profiles := make([]string, 0, len(data.Profiles.Elements()))
	ss = make([]string, 0, len(profiles))
	resp.Diagnostics.Append(data.Profiles.ElementsAs(ctx, &ss, false /* allowUnhandled */)...)
	for _, profile := range ss {
		profiles = append(profiles, profile)
	}

	states := make([]tenant.ClusterDiscoveryRequest_State, 0, len(data.States.Elements()))
	ss = make([]string, 0, len(states))
	resp.Diagnostics.Append(data.States.ElementsAs(ctx, &ss, false /* allowUnhandled */)...)
	if len(data.States.Elements()) != 0 {
		for _, state := range ss {
			states = append(states, tenant.ClusterDiscoveryRequest_State(tenant.ClusterDiscoveryRequest_State_value[state]))
		}
	} else {
		states = []tenant.ClusterDiscoveryRequest_State{
			tenant.ClusterDiscoveryRequest_ELIGIBLE,
			tenant.ClusterDiscoveryRequest_ENROLLED,
		}
	}

	// If any of the above conversions failed, quit early.
	if resp.Diagnostics.HasError() {
		return
	}

	disc, err := d.prov.client.Tenant().Clusters().Discover(ctx, &tenant.ClusterDiscoveryRequest{
		Id:        data.ID.ValueString(),
		Providers: providers,
		Profiles:  profiles,
		States:    states,
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to discover clusters"))
		return
	}

	results := make([]*clusterDiscoveryResults, 0, len(disc.Results))
	for _, result := range disc.Results {
		// Pre-null types.List values in case they aren't set below.
		state := &clusterDiscoveryResultsState{
			Profiles: types.ListNull(types.StringType),
			Steps:    types.ListNull(types.StringType),
		}

		switch s := result.State.(type) {
		case *tenant.ClusterDiscoveryResponse_Result_Unsupported_:
			state.State = types.StringValue("unsupported")
			state.Reason = types.StringValue(s.Unsupported.Reason)

		case *tenant.ClusterDiscoveryResponse_Result_NeedsWork_:
			state.State = types.StringValue("needsworks")
			steps, diags := types.ListValueFrom(ctx, types.StringType, s.NeedsWork.Steps)
			resp.Diagnostics.Append(diags...)
			if diags.HasError() {
				tflog.Error(ctx, "failed to convert NeedsWork.Steps []string to ListValue",
					map[string]interface{}{"steps": s.NeedsWork.Steps})
				return
			}
			state.Steps = steps

		case *tenant.ClusterDiscoveryResponse_Result_Eligible_:
			state.State = types.StringValue("eligible")
			state.Server = types.StringValue(s.Eligible.Info.Server)
			state.CertificateAuthorityData = types.StringValue(string(s.Eligible.Info.CertificateAuthorityData))

		case *tenant.ClusterDiscoveryResponse_Result_Enrolled_:
			profs, diags := types.ListValueFrom(ctx, types.StringType, s.Enrolled.Profiles)
			resp.Diagnostics.Append(diags...)
			if diags.HasError() {
				tflog.Error(ctx, "failed to convert Enrolled.Profiles []string to ListValue",
					map[string]interface{}{"profiles": s.Enrolled.Profiles})
				return
			}
			state.State = types.StringValue("enrolled")
			state.ID = types.StringValue(s.Enrolled.Id)
			state.Profiles = profs
			state.Server = types.StringValue(s.Enrolled.Info.Server)
			state.CertificateAuthorityData = types.StringValue(string(s.Enrolled.Info.CertificateAuthorityData))
		}

		results = append(results, &clusterDiscoveryResults{
			Provider: types.StringValue(result.Provider.String()),
			Account:  types.StringValue(result.Account),
			Location: types.StringValue(result.Location.String()),
			Name:     types.StringValue(result.Name),
			State:    []*clusterDiscoveryResultsState{state},
		})
	}
	data.Results = results

	// Set state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
