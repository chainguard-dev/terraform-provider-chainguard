package provider

import (
	"context"
	"fmt"

	"github.com/chainguard-dev/terraform-provider-chainguard/internal/validators"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"chainguard.dev/api/pkg/uidp"
	"chainguard.dev/api/proto/platform"
	"chainguard.dev/api/proto/platform/common"
	"chainguard.dev/api/proto/platform/iam"
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
	client platform.Clients
}

// TODO(colin): should group data source return >1 group?
type groupDataSourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	ParentID    types.String `tfsdk:"parent_id"`
}

// Metadata returns the data source type name.
func (d *groupDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_group"
}

func (d *groupDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(platform.Clients)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected platform.Clients, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	d.client = client
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
				Validators:  []validator.String{validators.NameValidator{}},
			},
			"description": schema.StringAttribute{
				Description: "Description of the matched IAM group",
				Computed:    true,
			},
			"parent_id": schema.StringAttribute{
				Description: "The UIDP of the group in which to lookup the named group.",
				Optional:    true,
				// TODO(colin): default value
				Validators: []validator.String{validators.UIDPValidator{AllowRoot: true}},
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

	// TODO(colin): what if parent_id == /
	uf := &common.UIDPFilter{}
	if data.ParentID.ValueString() != "" {
		uf.ChildrenOf = data.ParentID.ValueString()
	}
	f := &iam.GroupFilter{
		Id:   data.ID.ValueString(),
		Name: data.Name.ValueString(),
		Uidp: uf,
	}
	groupList, err := d.client.IAM().Groups().List(ctx, f)
	if err != nil {
		resp.Diagnostics.Append(protoErrorToDiagnostic(err, "failed to list groups"))
		return
	}

	// TODO(colin): should group data source return >1 group?
	switch c := len(groupList.GetItems()); {
	case c == 0:
		// Group was already deleted outside TF, remove from state
		data = groupDataSourceModel{}
		resp.State.RemoveResource(ctx)
	case c == 1:
		g := groupList.GetItems()[0]
		data.ID = types.StringValue(g.Id)
		data.Name = types.StringValue(g.Name)
		data.Description = types.StringValue(g.Description)
		data.ParentID = types.StringValue(uidp.Parent(g.Id))
	default:
		tflog.Error(ctx, fmt.Sprintf("group list returned %d groups for filter %v", c, f))
		resp.Diagnostics.AddError("more than one group found matching filters", fmt.Sprintf("filters=%v\nPlease provide more context to narrow query (e.g. parent_id).", data))
		return
	}

	// Set state
	diags := resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
}
