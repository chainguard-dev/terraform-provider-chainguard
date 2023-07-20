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

	"chainguard.dev/api/proto/platform"
	"chainguard.dev/api/proto/platform/iam"
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
	client platform.Clients
}

type roleDataSourceModel struct {
	ID     types.String `tfsdk:"id"`
	Name   types.String `tfsdk:"name"`
	Parent types.String `tfsdk:"parent"`

	Items []*roleModel `tfsdk:"items"`
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

func (d *roleDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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
func (d *roleDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lookup a role with the given name.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The exact UIDP of the role to lookup.",
				Optional:    true,
				Validators:  []validator.String{validators.UIDPValidator{}},
			},
			"name": schema.StringAttribute{
				Description: "The name of the role to lookup.",
				Optional:    true,
				Validators:  []validator.String{validators.NameValidator{}},
			},
			"parent": schema.StringAttribute{
				Description: "The UIDP of the group in which to lookup the named role.",
				Optional:    true,
				// TODO(colin): default value
				Validators: []validator.String{validators.UIDPValidator{AllowRoot: true}},
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

	all, err := d.client.IAM().Roles().List(ctx, &iam.RoleFilter{
		Id:     data.ID.ValueString(),
		Name:   data.Name.ValueString(),
		Parent: data.Parent.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.Append(protoErrorToDiagnostic(err, "failed to list roles"))
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
		data = roleDataSourceModel{}
		resp.State.RemoveResource(ctx)
	} else {
		// Set the ID on roleDataSourceModel for acceptance tests.
		// TODO(colin): replace this
		data.ID = types.StringValue("replace-me")
	}

	// Set state
	diags := resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
}
