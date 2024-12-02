/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/sigstore/cosign/v2/pkg/providers"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"chainguard.dev/sdk/auth"
	"chainguard.dev/sdk/proto/platform"
	"github.com/chainguard-dev/terraform-provider-chainguard/internal/protoutil"
	"github.com/chainguard-dev/terraform-provider-chainguard/internal/token"
	"github.com/chainguard-dev/terraform-provider-chainguard/internal/validators"

	_ "github.com/sigstore/cosign/v2/pkg/providers/github"
)

const (
	EnvChainguardConsoleAPI = "CHAINGUARD_CONSOLE_API"
	DefaultConsoleAPI       = "https://console-api.enforce.dev"

	EnvChainguardAudience = "CHAINGUARD_AUDIENCE"

	EnvAccAudience   = "TF_ACC_AUDIENCE"
	EnvAccConsoleAPI = "TF_ACC_CONSOLE_API"
	EnvAccGroupID    = "TF_ACC_GROUP_ID"
	EnvAccIssuer     = "TF_ACC_ISSUER"

	// EnvAccAmbient signals acceptance tests are being executed by GHA with ambient credentials.
	EnvAccAmbient = "TF_ACC_AMBIENT"
)

var EnvAccVars = []string{
	EnvAccAudience,
	EnvAccConsoleAPI,
	EnvAccGroupID,
	EnvAccIssuer,
}

var (
	// Ensure the implementation satisfies the expected interfaces.
	_ provider.Provider = &Provider{}

	UserAgent = "terraform-provider-chainguard"
)

// New is a helper function to simplify provider server and testing implementation.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &Provider{
			version: version,
		}
	}
}

// Provider is the provider implementation.
type Provider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and ran locally, and "test" when running acceptance
	// testing.
	version string
}

type ProviderModel struct {
	ConsoleAPI   types.String `tfsdk:"console_api"`
	LoginOptions types.Object `tfsdk:"login_options"`
}

type LoginOptionsModel struct {
	Disabled            types.Bool   `tfsdk:"disabled"`
	Identity            types.String `tfsdk:"identity_id"`
	IdentityToken       types.String `tfsdk:"identity_token"`
	IdentityProvider    types.String `tfsdk:"identity_provider_id"`
	Auth0Connection     types.String `tfsdk:"auth0_connection"`
	OrgName             types.String `tfsdk:"organization_name"`
	EnableRefreshTokens types.Bool   `tfsdk:"enable_refresh_tokens"`
}

// Metadata returns the provider type name.
func (p *Provider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "chainguard"
	resp.Version = p.version
}

// DataSources defines the data sources implemented in the provider.
func (p *Provider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewGroupDataSource,
		NewIdentityDataSource,
		NewRoleDataSource,
		NewVersionsDataSource,
	}
}

// Resources defines the resources implemented in the provider.
func (p *Provider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewAccountAssociationsResource,
		NewGroupResource,
		NewGroupInviteResource,
		NewIdentityResource,
		NewIdentityProviderResource,
		NewImageRepoResource,
		NewImageTagResource,
		NewRoleResource,
		NewRolebindingResource,
		NewSubscriptionResource,
	}
}

// Schema defines the provider-level schema for configuration data.
func (p *Provider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	auth0Connections := []string{"google-oauth2", "gitlab", "github"}

	resp.Schema = schema.Schema{
		Description: "Manage resources on the Chainguard platform.",
		Attributes: map[string]schema.Attribute{
			"console_api": schema.StringAttribute{
				Optional:    true,
				Description: "URL of Chainguard console API.",
				Validators: []validator.String{
					validators.IsURL(false /* requireHTTPS */),
				},
			},
		},
		Blocks: map[string]schema.Block{
			"login_options": schema.SingleNestedBlock{
				Description: "Options to configure automatic login when Chainguard token is expired.",
				Attributes: map[string]schema.Attribute{
					"disabled": schema.BoolAttribute{
						Description: "Disable automatic login when Chainguard token is expired.",
						Optional:    true,
					},
					"identity_id": schema.StringAttribute{
						Description: "UIDP of the identity to assume when exchanging OIDC token for Chainguard token.",
						Optional:    true,
						Validators:  []validator.String{validators.UIDP(false /* allowRootSentinel */)},
					},
					"identity_token": schema.StringAttribute{
						Description: "A path to an OIDC identity token, or explicit identity token.",
						Optional:    true,
						Validators: []validator.String{
							stringvalidator.ConflictsWith(
								path.Root("login_options").AtName("identity_provider_id").Expression(),
								path.Root("login_options").AtName("auth0_connection").Expression(),
								path.Root("login_options").AtName("organization_name").Expression(),
								path.Root("login_options").AtName("enable_refresh_tokens").Expression(),
							),
						},
					},
					"identity_provider_id": schema.StringAttribute{
						Description: "UIDP of the identity provider authenticate with for OIDC token.",
						Optional:    true,
						Validators:  []validator.String{validators.UIDP(false /* allowRootSentinel */)},
					},
					"auth0_connection": schema.StringAttribute{
						Description: fmt.Sprintf("Auth0 social connection to use by default for OIDC token. Must be one of: %s", strings.Join(auth0Connections, ", ")),
						Optional:    true,
						Validators:  []validator.String{stringvalidator.OneOf(auth0Connections...)},
					},
					"organization_name": schema.StringAttribute{
						Description: "Verified organization name for determining identity provider to obtain OIDC token.",
						Optional:    true,
						// TODO(colin): validate with OrgCheck()
					},
					"enable_refresh_tokens": schema.BoolAttribute{
						Description: "Enable to use of refresh tokens when authenticating with an IdP (not compatible with identity_token authentication).",
						Optional:    true,
					},
				},
			},
		},
	}
}

type providerData struct {
	client      platform.Clients
	consoleAPI  string
	loginConfig token.LoginConfig
	testing     bool
}

// Configure prepares a Chainguard API client for data sources and resources.
func (p *Provider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	// Parse provider configs
	var (
		pm ProviderModel
		lo LoginOptionsModel
	)
	if resp.Diagnostics.Append(req.Config.Get(ctx, &pm)...); resp.Diagnostics.HasError() {
		return
	}
	if !pm.LoginOptions.IsNull() {
		if resp.Diagnostics.Append(pm.LoginOptions.As(ctx, &lo, basetypes.ObjectAsOptions{})...); resp.Diagnostics.HasError() {
			return
		}
		tflog.Info(ctx, fmt.Sprintf("login options parsed: %#v", lo))
	}

	// Load default values and environment variables
	// Order of precedence for values:
	//   1. Environment variable
	//   2. Value from config
	//   3. Default value

	consoleAPI := protoutil.FirstNonEmpty(os.Getenv(EnvChainguardConsoleAPI), pm.ConsoleAPI.ValueString(), DefaultConsoleAPI)
	audience := protoutil.FirstNonEmpty(os.Getenv(EnvChainguardAudience), consoleAPI)
	// Decorate the UserAgent with version and runtime info.
	UserAgent = fmt.Sprintf("%s/%s %s/%s", UserAgent, p.version, runtime.GOOS, runtime.GOARCH)

	if p.version == "acctest" {
		// In acceptance tests override the console api and audience from env var
		tflog.Info(ctx, "** Running Acceptance Tests **")
		consoleAPI = os.Getenv(EnvAccConsoleAPI)
		audience = os.Getenv(EnvAccAudience)
	}

	// Save login parameters.
	var cfg token.LoginConfig
	{
		cfg = token.LoginConfig{
			Disabled:         lo.Disabled.ValueBool(),
			Issuer:           strings.Replace(consoleAPI, "console-api", "issuer", 1),
			Audience:         audience,
			Auth0Connection:  protoutil.FirstNonEmpty(os.Getenv("TF_CHAINGUARD_AUTH0_CONNECTION"), lo.Auth0Connection.ValueString()),
			IdentityID:       protoutil.FirstNonEmpty(os.Getenv("TF_CHAINGUARD_IDENTITY"), lo.Identity.ValueString()),
			IdentityProvider: protoutil.FirstNonEmpty(os.Getenv("TF_CHAINGUARD_IDP"), lo.IdentityProvider.ValueString()),
			OrgName:          protoutil.FirstNonEmpty(os.Getenv("TF_CHAINGUARD_ORG_NAME"), lo.OrgName.ValueString()),
			UserAgent:        UserAgent,
		}

		// Enable refresh tokens for users by default.
		// NB: Refresh tokens are incompatible with assumable identities, and unnecessary
		// when providing an explicit OIDC token.
		cfg.UseRefreshTokens = protoutil.DefaultBool(lo.EnableRefreshTokens, cfg.IdentityID == "" && cfg.IdentityToken == "")

		// Look for an OIDC token in the following places (in order of precedence)
		// 1. TF_CHAINGUARD_IDENTITY_TOKEN env var
		// 2. Ambient GitHub credentials
		// 3. login_options.identity_token, which is allowed to be empty
		switch {
		case os.Getenv("TF_CHAINGUARD_IDENTITY_TOKEN") != "":
			cfg.IdentityToken = os.Getenv("TF_CHAINGUARD_IDENTITY_TOKEN")
		case providers.Enabled(ctx):
			var err error
			cfg.IdentityToken, err = providers.Provide(ctx, cfg.Issuer)
			if err != nil {
				tflog.Error(ctx, fmt.Sprintf("failed to get identity token from ambient credentials: %s", err.Error()))
			}
		default:
			cfg.IdentityToken = lo.IdentityToken.ValueString()
		}
	}

	tflog.SetField(ctx, "chainguard.console_api", consoleAPI)

	// Client is intentionally set to nil here in case this
	// provider is used in an environment which does not have
	// access to the Chainguard API. Instead, client is set by
	// setupClient() only as needed.
	d := &providerData{
		client:      nil,
		loginConfig: cfg,
		consoleAPI:  consoleAPI,
		testing:     p.version == "acctest",
	}

	resp.DataSourceData = d
	resp.ResourceData = d
}

// newPlatformClients fetches a Chainguard token for the given audience and creates new platform gRPC clients.
func newPlatformClients(ctx context.Context, token, consoleAPI string) (platform.Clients, error) {
	cred := auth.NewFromToken(ctx, fmt.Sprintf("Bearer %s", token), false)
	ctx = platform.WithUserAgent(ctx, UserAgent)
	clients, err := platform.NewPlatformClients(ctx, consoleAPI, cred)
	if err != nil {
		return nil, err
	}
	return clients, nil
}

// errorToDiagnostic converts an error into a diag.Diagnostic.
// If err is a GRPC error, attempt to parse the status code and message from the error.
// codes.Unauthenticated is handled as a special case to suggest how to generate a token.
func errorToDiagnostic(err error, summary string) diag.Diagnostic {
	var d diag.Diagnostic

	switch stat, ok := status.FromError(err); {
	case !ok:
		d = diag.NewErrorDiagnostic(summary, err.Error())
	case stat.Code() == codes.Unauthenticated:
		d = diag.NewErrorDiagnostic(summary,
			"Unauthenticated. Please log in to generate a valid token (chainctl auth login) or set provider login_options.disabled = false.")
	default:
		d = diag.NewErrorDiagnostic(summary,
			fmt.Sprintf("%s: %s", stat.Code(), stat.Message()))
	}
	return d
}

func (pd *providerData) setupClient(ctx context.Context) error {
	tflog.Info(ctx, "configuring chainguard client")

	// Configure API clients
	var clients platform.Clients
	{
		// Get the Chainguard token
		// If it doesn't exist or is expired, attempt to get a new one, depending on login_options
		cgToken, err := token.Get(ctx, pd.loginConfig, false /* forceRefresh */)
		if err != nil {
			return fmt.Errorf("Failed to retrieve token. Either no token was found for audience %q or there was an error reading it.\n"+
				"Please check the value of \"chainguard.console_api\" in your Terraform provider configuration: %s", pd.loginConfig.Audience, err.Error())
		}

		// Generate platform clients.
		clients, err = newPlatformClients(ctx, string(cgToken), pd.consoleAPI)
		if err != nil {
			return fmt.Errorf("failed to create API clients: %s", err.Error())
		}
	}

	// Finally, set client on providerData struct
	pd.client = clients
	return nil
}
