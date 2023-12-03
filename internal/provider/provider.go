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
	"time"

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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"chainguard.dev/sdk/auth"
	"chainguard.dev/sdk/auth/login"
	sdktoken "chainguard.dev/sdk/auth/token"
	"chainguard.dev/sdk/proto/platform"
	"github.com/chainguard-dev/terraform-provider-chainguard/internal/validators"
)

const (
	EnvChainguardConsoleAPI = "CHAINGUARD_CONSOLE_API"
	DefaultConsoleAPI       = "https://console-api.enforce.dev"

	EnvAccAudience   = "TF_ACC_AUDIENCE"
	EnvAccConsoleAPI = "TF_ACC_CONSOLE_API"
	EnvAccGroupID    = "TF_ACC_GROUP_ID"
	EnvAccIssuer     = "TF_ACC_ISSUER"

	// auth0ClientID is the oauth2 clientID to use the Auth0 instance.
	auth0ClientID = "auth0"
)

var EnvAccVars = []string{
	EnvAccAudience,
	EnvAccConsoleAPI,
	EnvAccGroupID,
	EnvAccIssuer,
}

// Ensure the implementation satisfies the expected interfaces.
var (
	_ provider.Provider = &Provider{}
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
	ConsoleAPI types.String `tfsdk:"console_api"`

	LoginOptions types.Object `tfsdk:"login_options"`
}

type loginModel struct {
	Disabled         types.Bool   `tfsdk:"disabled"`
	Identity         types.String `tfsdk:"identity_id"`
	IdentityProvider types.String `tfsdk:"identity_provider_id"`
	Auth0Connection  types.String `tfsdk:"auth0_connection"`
	OrgName          types.String `tfsdk:"organization_name"`
}

// Metadata returns the provider type name.
func (p *Provider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "chainguard"
	resp.Version = p.version
}

// DataSources defines the data sources implemented in the provider.
func (p *Provider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewClusterCIDRDataSource,
		NewGroupDataSource,
		NewIdentityDataSource,
		NewRoleDataSource,
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
		NewPolicyResource,
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
				Description: "URL of Chainguard console API. Ensure a valid token has been generated for this URL with `chainctl auth login`.",
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
				},
			},
		},
	}
}

type providerData struct {
	client  platform.Clients
	testing bool
}

// Configure prepares a Chainguard API client for data sources and resources.
func (p *Provider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data ProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Load default values and environment variables
	// Order of precedence for values:
	//   1. Environment variable
	//   2. Value from config
	//   3. Default value

	consoleAPI := firstNonEmpty(os.Getenv(EnvChainguardConsoleAPI), data.ConsoleAPI.ValueString(), DefaultConsoleAPI)
	audience := consoleAPI

	if p.version == "acctest" {
		// In acceptance tests override the console api and audience from env var
		tflog.Info(ctx, "** Running Acceptance Tests **")
		consoleAPI = os.Getenv(EnvAccConsoleAPI)
		audience = os.Getenv(EnvAccAudience)
	}

	ctx = tflog.SetField(ctx, "chainguard.console_api", consoleAPI)
	tflog.Info(ctx, "configuring chainguard client")

	// If token is expired or not found, login and save a new one.
	var lm loginModel
	if !data.LoginOptions.IsNull() {
		if resp.Diagnostics.Append(data.LoginOptions.As(ctx, &lm, basetypes.ObjectAsOptions{})...); resp.Diagnostics.HasError() {
			return
		}
		tflog.Info(ctx, fmt.Sprintf("login options parsed: %#v", lm))
	}

	if (os.Getenv("TF_CHAINGUARD_LOGIN") != "" || !lm.Disabled.ValueBool()) && sdktoken.RemainingLife(audience, time.Minute) <= 0 {
		tflog.Info(ctx, "launching login browser")
		// Construct the issuer URL from the console API.
		issuer := strings.Replace(consoleAPI, "console-api", "issuer", 1)
		loginCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()

		// For each option, prefer the environment var, if set.
		tokstr, err := login.Login(loginCtx,
			login.WithIssuer(issuer),
			login.WithAudience([]string{audience}),
			login.WithClientID(auth0ClientID),
			login.WithIdentity(firstNonEmpty(os.Getenv("TF_CHAINGUARD_IDENTITY"), lm.Identity.ValueString())),
			login.WithIdentityProvider(firstNonEmpty(os.Getenv("TF_CHAINGUARD_IDP"), lm.IdentityProvider.ValueString())),
			login.WithAuth0Connection(firstNonEmpty(os.Getenv("TF_CHAINGUARD_AUTH0_CONNECTION"), lm.Auth0Connection.ValueString())),
			login.WithOrgName(firstNonEmpty(os.Getenv("TF_CHAINGUARD_ORG_NAME"), lm.OrgName.ValueString())),
		)
		if err != nil {
			resp.Diagnostics.Append(errorToDiagnostic(err, "failed to obtain Chainguard token"))
			return
		}
		if err = sdktoken.Save([]byte(tokstr), audience); err != nil {
			resp.Diagnostics.Append(errorToDiagnostic(err, "failed to save Chainguard token"))
			return
		}
	}

	token, err := sdktoken.Load(audience)
	if err != nil {
		resp.Diagnostics.AddAttributeError(
			path.Root("console_api"),
			"failed to retrieve Chainguard token",
			fmt.Sprintf("Either no token was found for audience %q or there was an error reading it.\n"+
				"Please check the value of \"chainguard.console_api\" in your Terraform provider configuration.", audience))
		return
	}

	useragent := fmt.Sprintf("terraform-provider-chainguard/%s %s/%s", p.version, runtime.GOOS, runtime.GOARCH)
	cred := auth.NewFromToken(ctx, fmt.Sprintf("Bearer %s", token), false)
	ctx = platform.WithUserAgent(ctx, useragent)
	clients, err := platform.NewPlatformClients(ctx, consoleAPI, cred)
	if err != nil {
		resp.Diagnostics.AddAttributeError(
			path.Root("console_api"),
			fmt.Sprintf("failed to create api client with %s", consoleAPI),
			err.Error())
		return
	}

	d := &providerData{
		client:  clients,
		testing: p.version == "acctest",
	}

	resp.DataSourceData = d
	resp.ResourceData = d
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
			"Unauthenticated. Please log in to generate a valid token: `chainctl auth login`.")
	default:
		d = diag.NewErrorDiagnostic(summary,
			fmt.Sprintf("%s: %s", stat.Code(), stat.Message()))
	}
	return d
}
