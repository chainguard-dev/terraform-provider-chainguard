/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/objectvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"google.golang.org/protobuf/types/known/timestamppb"

	registry "chainguard.dev/sdk/proto/platform/registry/v1"
	"chainguard.dev/sdk/uidp"
	"chainguard.dev/sdk/validation"
	"github.com/chainguard-dev/terraform-provider-chainguard/internal/validators"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &imageRepoResource{}
	_ resource.ResourceWithConfigure   = &imageRepoResource{}
	_ resource.ResourceWithImportState = &imageRepoResource{}
)

// NewImageRepoResource is a helper function to simplify the provider implementation.
func NewImageRepoResource() resource.Resource {
	return &imageRepoResource{}
}

// imageRepoResource is the resource implementation.
type imageRepoResource struct {
	managedResource
}

type imageRepoResourceModel struct {
	ID         types.String `tfsdk:"id"`
	Name       types.String `tfsdk:"name"`
	ParentID   types.String `tfsdk:"parent_id"`
	Bundles    types.List   `tfsdk:"bundles"`
	Readme     types.String `tfsdk:"readme"`
	SyncConfig types.Object `tfsdk:"sync_config"`
	// Image tier (e.g. APPLICATION, BASE, etc.)
	Tier    types.String `tfsdk:"tier"`
	Aliases types.List   `tfsdk:"aliases"`
}

type syncConfig struct {
	Source      types.String `tfsdk:"source"`
	Expiration  types.String `tfsdk:"expiration"`
	UniqueTags  types.Bool   `tfsdk:"unique_tags"`
	GracePeriod types.Bool   `tfsdk:"grace_period"`
	SyncAPKs    types.Bool   `tfsdk:"sync_apks"`
	Google      types.String `tfsdk:"google"`
	Amazon      types.String `tfsdk:"amazon"`
	Azure       types.String `tfsdk:"azure"`
	ApkoOverlay types.String `tfsdk:"apko_overlay"`
}

func (r *imageRepoResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.configure(ctx, req, resp)
}

// Metadata returns the resource type name.
func (r *imageRepoResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_image_repo"
}

// Schema defines the schema for the resource.
func (r *imageRepoResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Image repo (note: delete is purposefully a no-op).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "The UIDP of this repo.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Description: "The name of this repo.",
				Required:    true,
			},
			"parent_id": schema.StringAttribute{
				Description:   "The group that owns the repo.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{
					validators.UIDP(false /* allowRootSentinel */),
				},
			},

			"bundles": schema.ListAttribute{
				Description: "List of bundles associated with this repo (valid ones: `application|base|byol|ai|ai-gpu|featured|fips`).",
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
			"aliases": schema.ListAttribute{
				Description: "Known aliases for a given image.",
				Optional:    true,
				ElementType: types.StringType,
				Validators: []validator.List{
					listvalidator.ValueStringsAre(validators.ValidateStringFuncs(validAliasesValue)),
				},
			},
		},
		Blocks: map[string]schema.Block{
			"sync_config": schema.SingleNestedBlock{
				Description: "Configuration for catalog syncing.",
				Validators: []validator.Object{
					objectvalidator.AlsoRequires(
						path.Root("sync_config").AtName("source").Expression(),
						path.Root("sync_config").AtName("expiration").Expression(),
					),
				},
				Attributes: map[string]schema.Attribute{
					"source": schema.StringAttribute{
						Description: "The UIDP of the repository to sync images from.",
						Optional:    true, // This attribute is required, but only if the block is defined. See Validators.
						Validators: []validator.String{
							validators.UIDP(false /* allowRootSentinel */),
						},
					},
					"expiration": schema.StringAttribute{
						Description: "The RFC3339 encoded date and time at which this entitlement will expire.",
						Optional:    true, // This attribute is required, but only if the block is defined. See Validators.
						Validators: []validator.String{
							validators.ValidateStringFuncs(checkRFC3339),
						},
					},
					"unique_tags": schema.BoolAttribute{
						Description: "Whether each synchronized tag should be suffixed with the image timestamp.",
						Optional:    true,
					},
					"grace_period": schema.BoolAttribute{
						Description: "Controls whether the image grace period functionality is enabled or not.",
						Optional:    true,
					},
					"sync_apks": schema.BoolAttribute{
						Description: "Whether the APKs for each image should also be synchronized.",
						Optional:    true,
					},
					"amazon": schema.StringAttribute{
						Description: "The Amazon repository under which to create a new repository with the same name as the source repository.",
						Optional:    true, // This attribute is required, but only if the block is defined. See Validators.
					},
					"google": schema.StringAttribute{
						Description: "The Google repository under which to create a new repository with the same name as the source repository.",
						Optional:    true, // This attribute is required, but only if the block is defined. See Validators.
					},
					"azure": schema.StringAttribute{
						Description: "The Azure repository under which to create a new repository with the same name as the source repository.",
						Optional:    true, // This attribute is required, but only if the block is defined. See Validators.
					},
					"apko_overlay": schema.StringAttribute{
						Description: "A json-encoded APKO configuration to overlay on rebuilds of images being synced.",
						Optional:    true,
						// TODO: Validatore for JSON + APKO
					},
				},
			},
		},
	}
}

// validBundlesValue implements validators.ValidateStringFunc.
func validBundlesValue(s string) error {
	if err := validation.ValidateBundles([]string{s}); err != nil {
		return fmt.Errorf("bundle item %q is invalid: %w", s, err)
	}
	return nil
}

// validAliasesValue implements validators.ValidateStringFunc.
func validAliasesValue(s string) error {
	if err := validation.ValidateAliases([]string{s}); err != nil {
		return fmt.Errorf("alias %q is invalid: %w", s, err)
	}
	return nil
}

// validTierValue implements validators.ValidateStringFunc.
func validTierValue(s string) error {
	if _, ok := registry.CatalogTier_value[s]; !ok {
		return fmt.Errorf("tier %q is invalid", s)
	}
	return nil
}

// validReadmeValue implements validators.ValidateStringFunc.
func validReadmeValue(s string) error {
	if diff, err := validation.ValidateReadme(s); err != nil {
		return fmt.Errorf("readme is invalid: %w. diff: %s", err, diff)
	}
	return nil
}

// ImportState imports resources by ID into the current Terraform state.
func (r *imageRepoResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

var mu sync.Mutex

// Create creates the resource and sets the initial Terraform state.
func (r *imageRepoResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// Read the plan data into the resource model.
	var plan imageRepoResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("create image repo request: name=%s, parent_id=%s", plan.Name, plan.ParentID))

	// Lock to prevent concurrent creation of the same repo.
	mu.Lock()
	defer mu.Unlock()

	var sc *registry.SyncConfig
	if !plan.SyncConfig.IsNull() {
		var cfg syncConfig
		resp.Diagnostics.Append(plan.SyncConfig.As(ctx, &cfg, basetypes.ObjectAsOptions{})...)
		if resp.Diagnostics.HasError() {
			return
		}
		ts, err := time.Parse(time.RFC3339, cfg.Expiration.ValueString())
		if err != nil {
			resp.Diagnostics.Append(errorToDiagnostic(err, "failed to parse timestamp"))
			if resp.Diagnostics.HasError() {
				return
			}
		}
		sc = &registry.SyncConfig{
			Source:      cfg.Source.ValueString(),
			Expiration:  timestamppb.New(ts),
			UniqueTags:  cfg.UniqueTags.ValueBool(),
			GracePeriod: cfg.GracePeriod.ValueBool(),
			SyncApks:    cfg.SyncAPKs.ValueBool(),
			Amazon:      cfg.Amazon.ValueString(),
			Google:      cfg.Google.ValueString(),
			Azure:       cfg.Azure.ValueString(),
			ApkoOverlay: cfg.ApkoOverlay.ValueString(),
		}
	}

	bundles := make([]string, 0, len(plan.Bundles.Elements()))
	resp.Diagnostics.Append(plan.Bundles.ElementsAs(ctx, &bundles, false /* allowUnhandled */)...)
	if resp.Diagnostics.HasError() {
		return
	}

	aliases := make([]string, 0, len(plan.Aliases.Elements()))
	resp.Diagnostics.Append(plan.Aliases.ElementsAs(ctx, &aliases, false /* allowUnhandled */)...)
	if resp.Diagnostics.HasError() {
		return
	}

	repo, err := r.prov.client.Registry().Registry().CreateRepo(ctx, &registry.CreateRepoRequest{
		ParentId: plan.ParentID.ValueString(),
		Repo: &registry.Repo{
			Name:        plan.Name.ValueString(),
			Bundles:     bundles,
			Readme:      plan.Readme.ValueString(),
			SyncConfig:  sc,
			CatalogTier: registry.CatalogTier(registry.CatalogTier_value[plan.Tier.ValueString()]),
			Aliases:     aliases,
		},
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to create image repo"))
		return
	}

	// Save repo details in the state.
	plan.ID = types.StringValue(repo.Id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state with the latest data.
func (r *imageRepoResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Read the current state into the resource model.
	var state imageRepoResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("read image repo request: %s", state.ID))

	// Lock to prevent concurrent update of the same repo.
	mu.Lock()
	defer mu.Unlock()

	// Query for the repo to update state
	id := state.ID.ValueString()
	repoList, err := r.prov.client.Registry().Registry().ListRepos(ctx, &registry.RepoFilter{
		Id: id,
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to list image repos"))
		return
	}

	switch c := len(repoList.GetItems()); {
	case c == 0:
		// Repo doesn't exist or was deleted outside TF
		resp.State.RemoveResource(ctx)
		return
	case c > 1:
		resp.Diagnostics.AddError("internal error", fmt.Sprintf("fatal data corruption: id %s matched more than one image repo", id))
		return
	}

	// Update the state with values returned from the API.
	repo := repoList.GetItems()[0]
	state.ID = types.StringValue(repo.Id)
	state.ParentID = types.StringValue(uidp.Parent(repo.Id))
	state.Name = types.StringValue(repo.Name)

	// Only update the state readme if it started as non-null or we receive a description.
	if !state.Readme.IsNull() || repo.Readme != "" {
		state.Readme = types.StringValue(repo.Readme)
	}

	if !state.Tier.IsNull() || repo.CatalogTier != registry.CatalogTier_UNKNOWN {
		state.Tier = types.StringValue(repo.CatalogTier.String())
	}

	var sc syncConfig
	var diags diag.Diagnostics
	if !state.SyncConfig.IsNull() {
		if diags = state.SyncConfig.As(ctx, &sc, basetypes.ObjectAsOptions{}); diags.HasError() {
			resp.Diagnostics.Append(diags...)
			return
		}
		update := (sc.Source.ValueString() != repo.SyncConfig.Source) ||
			(sc.Expiration.ValueString() != repo.SyncConfig.Expiration.AsTime().Format(time.RFC3339)) ||
			(sc.UniqueTags.ValueBool() != repo.SyncConfig.UniqueTags) ||
			(sc.GracePeriod.ValueBool() != repo.SyncConfig.GracePeriod) ||
			(sc.SyncAPKs.ValueBool() != repo.SyncConfig.SyncApks)

		if update {
			sc.Source = types.StringValue(repo.SyncConfig.Source)
			sc.Expiration = types.StringValue(repo.SyncConfig.Expiration.AsTime().Format(time.RFC3339))
			sc.UniqueTags = types.BoolValue(repo.SyncConfig.UniqueTags)
			sc.GracePeriod = types.BoolValue(repo.SyncConfig.GracePeriod)
			sc.SyncAPKs = types.BoolValue(repo.SyncConfig.SyncApks)
			state.SyncConfig, diags = types.ObjectValueFrom(ctx, state.SyncConfig.AttributeTypes(ctx), sc)
			resp.Diagnostics.Append(diags...)
		}
	}

	state.Bundles, diags = types.ListValueFrom(ctx, types.StringType, repo.Bundles)
	if diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	state.Aliases, diags = types.ListValueFrom(ctx, types.StringType, repo.Aliases)
	if diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	// Set state
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *imageRepoResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Read the plan into the resource model.
	var data imageRepoResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("update image repo request: %s", data.ID))

	// Lock to prevent concurrent update of the same repo.
	mu.Lock()
	defer mu.Unlock()

	var sc *registry.SyncConfig
	if !data.SyncConfig.IsNull() {
		var cfg syncConfig
		resp.Diagnostics.Append(data.SyncConfig.As(ctx, &cfg, basetypes.ObjectAsOptions{})...)
		if resp.Diagnostics.HasError() {
			return
		}
		ts, err := time.Parse(time.RFC3339, cfg.Expiration.ValueString())
		if err != nil {
			resp.Diagnostics.Append(errorToDiagnostic(err, "failed to parse timestamp"))
			if resp.Diagnostics.HasError() {
				return
			}
		}
		sc = &registry.SyncConfig{
			Source:      cfg.Source.ValueString(),
			Expiration:  timestamppb.New(ts),
			UniqueTags:  cfg.UniqueTags.ValueBool(),
			GracePeriod: cfg.GracePeriod.ValueBool(),
			SyncApks:    cfg.SyncAPKs.ValueBool(),
			Amazon:      cfg.Amazon.ValueString(),
			Google:      cfg.Google.ValueString(),
			Azure:       cfg.Azure.ValueString(),
			ApkoOverlay: cfg.ApkoOverlay.ValueString(),
		}
	}

	bundles := make([]string, 0, len(data.Bundles.Elements()))
	resp.Diagnostics.Append(data.Bundles.ElementsAs(ctx, &bundles, false /* allowUnhandled */)...)
	if resp.Diagnostics.HasError() {
		return
	}

	aliases := make([]string, 0, len(data.Aliases.Elements()))
	resp.Diagnostics.Append(data.Aliases.ElementsAs(ctx, &aliases, false /* allowUnhandled */)...)
	if resp.Diagnostics.HasError() {
		return
	}

	repo, err := r.prov.client.Registry().Registry().UpdateRepo(ctx, &registry.Repo{
		Id:          data.ID.ValueString(),
		Name:        data.Name.ValueString(),
		Bundles:     bundles,
		Readme:      data.Readme.ValueString(),
		SyncConfig:  sc,
		CatalogTier: registry.CatalogTier(registry.CatalogTier_value[data.Tier.ValueString()]),
		Aliases:     aliases,
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, "failed to update image repo"))
		return
	}

	// Update the state with values returned from the API.
	data.ID = types.StringValue(repo.Id)
	data.Name = types.StringValue(repo.Name)

	// Treat empty readme as nil
	if repo.Readme != "" {
		data.Readme = types.StringValue(repo.Readme)
	}
	// Treat UNKNOWN tier as null, but only if it was already null
	if !data.Tier.IsNull() || repo.CatalogTier != registry.CatalogTier_UNKNOWN {
		data.Tier = types.StringValue(repo.CatalogTier.String())
	} else {
		data.Tier = types.StringNull()
	}

	var diags diag.Diagnostics
	data.Bundles, diags = types.ListValueFrom(ctx, types.StringType, repo.Bundles)
	if diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	data.Aliases, diags = types.ListValueFrom(ctx, types.StringType, repo.Aliases)
	if diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete is purposefully a no-op so we don't accidentally delete repos with terraform.
// Instead, delete them with "chainctl img rm".
func (r *imageRepoResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// When not running acceptance tests, add an error to resp so Terraform does not automatically remove this resource from state.
	// See https://developer.hashicorp.com/terraform/plugin/framework/resources/delete#caveats for details.
	if !r.prov.testing {
		resp.Diagnostics.AddError("not implemented", "Image repos cannot be deleted through Terraform. Use `chainctl img repo rm` to manually delete.")
		return
	}

	// Read the current state into the resource model.
	var state imageRepoResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("ACCEPTANCE TEST: delete image repo request: %s", state.ID))

	// Lock to prevent concurrent creation of the same repo.
	mu.Lock()
	defer mu.Unlock()

	id := state.ID.ValueString()
	_, err := r.prov.client.Registry().Registry().DeleteRepo(ctx, &registry.DeleteRepoRequest{
		Id: id,
	})
	if err != nil {
		resp.Diagnostics.Append(errorToDiagnostic(err, fmt.Sprintf("failed to delete image repo %q", id)))
		return
	}
}
