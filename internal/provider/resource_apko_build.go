package provider

import (
	"context"
	"fmt"

	apkotypes "chainguard.dev/apko/pkg/build/types"
	v1 "chainguard.dev/sdk/proto/platform/common/v1"
	registry "chainguard.dev/sdk/proto/platform/registry/v1"
	"github.com/chainguard-dev/terraform-provider-chainguard/internal/validators"
	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"google.golang.org/protobuf/testing/protocmp"
	"gopkg.in/yaml.v2"
)

var _ resource.Resource = &BuildResource{}
var _ resource.ResourceWithImportState = &BuildResource{}

func NewBuildResource() resource.Resource {
	return &BuildResource{}
}

type BuildResource struct {
	managedResource
}

type BuildResourceModel struct {
	Id        types.String `tfsdk:"id"`
	Repo      types.String `tfsdk:"repo"`
	Config    types.String `tfsdk:"config"`
	MediaType types.String `tfsdk:"media_type"`
	ImageRef  types.String `tfsdk:"image_ref"`
}

func (r *BuildResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_apko_build"
}

func (r *BuildResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.configure(ctx, req, resp)
}

func (r *BuildResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "This performs an apko build from the provided config file",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The build report UIDP for the most recent build.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"repo": schema.StringAttribute{
				MarkdownDescription: "The UIDP of the repository in which to build the image.",
				Required:            true,
				Validators:          []validator.String{validators.UIDP(false /* allowRootSentinel */)},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"config": schema.StringAttribute{
				MarkdownDescription: "The apko configuration to build.",
				Required:            true,
				Validators:          []validator.String{
					// TODO(mattmoor): ImageConfiguration
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"media_type": schema.StringAttribute{
				MarkdownDescription: "The layer media type to build.",
				Computed:            true,
				Optional:            true,
				Required:            false,
				Default:             stringdefault.StaticString("application/vnd.oci.image.layer.v1.tar+gzip"),
				Validators:          []validator.String{
					// TODO(mattmoor): IANA?
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"image_ref": schema.StringAttribute{
				MarkdownDescription: "The resulting fully-qualified digest (e.g. {repo}@sha256:deadbeef).",
				Computed:            true,
			},
		},
	}
}

func (r *BuildResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *BuildResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// parse yaml to apkotypes.ImageConfiguration
	ic := &apkotypes.ImageConfiguration{}
	if err := yaml.Unmarshal([]byte(data.Config.ValueString()), &ic); err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to parse configuration"))
		return
	}
	cfg := registry.ToApkoProto(*ic)

	build, err := r.prov.client.Registry().Apko().BuildImage(ctx, &registry.BuildImageRequest{
		Config:    cfg,
		RepoUidp:  data.Repo.ValueString(),
		MediaType: data.MediaType.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to build image"))
		return
	}
	if build.UserError != "" {
		resp.Diagnostics.Append(diag.NewErrorDiagnostic("error performing build", build.UserError))
		return
	}

	data.Id = types.StringValue(build.BuildReportId)
	data.ImageRef = types.StringValue(build.Digest)

	tflog.Trace(ctx, "created a resource")
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *BuildResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *BuildResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The id is the BuildReportID, with which we can fetch a significant amount
	// of metadata about the previous build.  So we should:
	// 1. Fetch the build report.
	// 2. Re-resolve the build config.
	// 3. Compare the locked configurations to see if a rebuild is needed.
	if !data.Id.IsNull() {
		reports, err := r.prov.client.Registry().Registry().ListBuildReports(ctx, &registry.BuildReportFilter{
			Uidp: &v1.UIDPFilter{
				DescendantsOf: data.Id.ValueString(),
			},
		})
		if err != nil {
			// When it's not found it should be an empty list, not an error,
			// so make this fatal.
			resp.Diagnostics.Append(errorToDiagnostic(err, "failed to list build reports"))
			return
		}
		if len(reports.Reports) != 1 {
			// Force a rebuild
			data.Id = types.StringNull()
		} else if report := reports.Reports[0]; report.Config != data.Config.ValueString() {
			// Force a rebuild
			data.Id = types.StringNull()
		} else {
			// parse yaml to apkotypes.ImageConfiguration
			cfgRaw := &apkotypes.ImageConfiguration{}
			if err := yaml.Unmarshal([]byte(data.Config.ValueString()), &cfgRaw); err != nil {
				resp.Diagnostics.Append(errorToDiagnostic(err, "failed to parse configuration"))
				return
			}
			cfg := registry.ToApkoProto(*cfgRaw)
			want, err := r.prov.client.Registry().Apko().ResolveConfig(ctx, &registry.ResolveConfigRequest{
				Config:   cfg,
				RepoUidp: data.Repo.ValueString(),
			})
			if err != nil {
				resp.Diagnostics.Append(errorToDiagnostic(err, "failed to resolve configuration"))
				return
			}

			gotRaw := &apkotypes.ImageConfiguration{}
			if err := yaml.Unmarshal([]byte(report.LockedConfig), &gotRaw); err != nil {
				resp.Diagnostics.Append(errorToDiagnostic(err, "failed to parse configuration"))
				return
			}
			got := registry.ToApkoProto(*gotRaw)

			if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
				tflog.Trace(ctx, fmt.Sprintf("triggering rebuild due to diff: %s", diff))

				// Force a rebuild
				data.Id = types.StringNull()
			}
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *BuildResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data *BuildResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// parse yaml to apkotypes.ImageConfiguration
	ic := &apkotypes.ImageConfiguration{}
	if err := yaml.Unmarshal([]byte(data.Config.ValueString()), &ic); err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to parse configuration"))
		return
	}
	cfg := registry.ToApkoProto(*ic)

	build, err := r.prov.client.Registry().Apko().BuildImage(ctx, &registry.BuildImageRequest{
		Config:    cfg,
		RepoUidp:  data.Repo.ValueString(),
		MediaType: data.MediaType.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to rebuild image"))
		return
	}
	if build.UserError != "" {
		resp.Diagnostics.Append(diag.NewErrorDiagnostic("error performing build", build.UserError))
		return
	}

	data.Id = types.StringValue(build.BuildReportId)
	data.ImageRef = types.StringValue(build.Digest)

	tflog.Trace(ctx, "updated a resource")
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *BuildResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data *BuildResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// TODO: If we ever want to delete the image from the registry, we can do it here.
}

func (r *BuildResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
