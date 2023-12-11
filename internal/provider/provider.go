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
	"github.com/sigstore/cosign/v2/pkg/providers"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"chainguard.dev/sdk/auth"
	"chainguard.dev/sdk/auth/login"
	sdktoken "chainguard.dev/sdk/auth/token"
	"chainguard.dev/sdk/proto/platform"
	"chainguard.dev/sdk/sts"
	"github.com/chainguard-dev/terraform-provider-chainguard/internal/validators"

	_ "github.com/sigstore/cosign/v2/pkg/providers/github"
)

const (
	EnvChainguardConsoleAPI = "CHAINGUARD_CONSOLE_API"
	DefaultConsoleAPI       = "https://console-api.enforce.dev"

	EnvAccAudience   = "TF_ACC_AUDIENCE"
	EnvAccConsoleAPI = "TF_ACC_CONSOLE_API"
	EnvAccGroupID    = "TF_ACC_GROUP_ID"
	EnvAccIssuer     = "TF_ACC_ISSUER"

	// EnvAccAmbient signals acceptance tests are being executed by GHA with ambient credentials
	EnvAccAmbient = "TF_ACC_AMBIENT"

	// auth0ClientID is the oauth2 clientID to use the Auth0 instance.
	auth0ClientID = "auth0"
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
	ConsoleAPI types.String `tfsdk:"console_api"`

	LoginOptions types.Object `tfsdk:"login_options"`
}

type loginModel struct {
	Disabled         types.Bool   `tfsdk:"disabled"`
	Identity         types.String `tfsdk:"identity_id"`
	IdentityToken    types.String `tfsdk:"identity_token"`
	IdentityProvider types.String `tfsdk:"identity_provider_id"`
	Auth0Connection  types.String `tfsdk:"auth0_connection"`
	OrgName          types.String `tfsdk:"organization_name"`

	consoleAPI string
	audience   string
	issuer     string
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
				},
			},
		},
	}
}

type providerData struct {
	client       platform.Clients
	loginOptions loginModel
	testing      bool
}

// Configure prepares a Chainguard API client for data sources and resources.
func (p *Provider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	// Parse provider configs
	var (
		pm ProviderModel
		lm loginModel
	)
	if resp.Diagnostics.Append(req.Config.Get(ctx, &pm)...); resp.Diagnostics.HasError() {
		return
	}
	if !pm.LoginOptions.IsNull() {
		if resp.Diagnostics.Append(pm.LoginOptions.As(ctx, &lm, basetypes.ObjectAsOptions{})...); resp.Diagnostics.HasError() {
			return
		}
		tflog.Info(ctx, fmt.Sprintf("login options parsed: %#v", lm))
	}

	// Load default values and environment variables
	// Order of precedence for values:
	//   1. Environment variable
	//   2. Value from config
	//   3. Default value

	consoleAPI := firstNonEmpty(os.Getenv(EnvChainguardConsoleAPI), pm.ConsoleAPI.ValueString(), DefaultConsoleAPI)
	audience := consoleAPI
	// Decorate the UserAgent with version and runtime info.
	UserAgent = fmt.Sprintf("%s/%s %s/%s", UserAgent, p.version, runtime.GOOS, runtime.GOARCH)

	if p.version == "acctest" {
		// In acceptance tests override the console api and audience from env var
		tflog.Info(ctx, "** Running Acceptance Tests **")
		consoleAPI = os.Getenv(EnvAccConsoleAPI)
		audience = os.Getenv(EnvAccAudience)
	}

	// Save login parameters.
	lm.consoleAPI = consoleAPI
	lm.issuer = strings.Replace(consoleAPI, "console-api", "issuer", 1)
	lm.audience = audience

	ctx = tflog.SetField(ctx, "chainguard.console_api", consoleAPI)
	tflog.Info(ctx, "configuring chainguard client")

	// If token is expired or not found, login and save a new one.
	if sdktoken.RemainingLife(audience, time.Minute) <= 0 {
		err := refreshChainguardToken(ctx, lm)
		if err != nil {
			resp.Diagnostics.Append(errorToDiagnostic(err, "failed to refresh Chainguard token"))
			return
		}
	}

	// Generate platform clients.
	clients, err := newPlatformClients(ctx, audience, consoleAPI)
	if err != nil {
		resp.Diagnostics.AddAttributeError(
			path.Root("console_api"),
			"failed to retrieve Chainguard token",
			err.Error())
	}

	d := &providerData{
		client:       clients,
		loginOptions: lm,
		testing:      p.version == "acctest",
	}

	resp.DataSourceData = d
	resp.ResourceData = d
}

// newPlatformClients fetches a Chainguard token for the given audience and creates new platform gRPC clients.
func newPlatformClients(ctx context.Context, audience, consoleAPI string) (platform.Clients, error) {
	token, err := sdktoken.Load(audience)
	if err != nil {
		return nil, fmt.Errorf(
			fmt.Sprintf("Failed to retrieve token. Either no token was found for audience %q or there was an error reading it.\n"+
				"Please check the value of \"chainguard.console_api\" in your Terraform provider configuration.", audience))
	}

	cred := auth.NewFromToken(ctx, fmt.Sprintf("Bearer %s", token), false)
	ctx = platform.WithUserAgent(ctx, UserAgent)
	clients, err := platform.NewPlatformClients(ctx, consoleAPI, cred)
	if err != nil {
		return nil, fmt.Errorf("failed to create api client with %s: %w", consoleAPI, err)
	}
	return clients, nil
}

// refreshChainguardToken attempts to get a new Chainguard token either through user browser flow,
// or by exchanging a given OIDC token, unless auto-login is disabled.
func refreshChainguardToken(ctx context.Context, lm loginModel) error {
	// Bail if auto-login is disabled.
	if os.Getenv("TF_CHAINGUARD_LOGIN") == "" && lm.Disabled.ValueBool() {
		tflog.Info(ctx, "automatic authentication disabled")
		return status.Error(codes.Unauthenticated, "automatic auth disabled")
	}

	tflog.Info(ctx, "refreshing Chainguard token")
	var (
		err     error
		cgToken string
	)

	// Fetch an identity token, if one was passed
	idToken, err := getIdentityToken(ctx, lm)
	if err != nil {
		return err
	}

	if idToken != "" {
		cgToken, err = exchangeToken(ctx, idToken, lm)
	} else {
		cgToken, err = getChainguardToken(ctx, lm)
	}
	if err != nil {
		return fmt.Errorf("failed to get Chainguard token: %w", err)
	}

	if err = sdktoken.Save([]byte(cgToken), lm.audience); err != nil {
		return fmt.Errorf("failed to save Chainguard token: %w", err)
	}
	return nil
}

// getIdentityToken looks for an OIDC token in the following places (in order of precedence)
// 1. TF_CHAINGUARD_IDENTITY_TOKEN env var
// 2. Ambient GitHub credentials
// 3. login_options.identity_token
// If no token is found, an empty string is returned.
func getIdentityToken(ctx context.Context, lm loginModel) (string, error) {
	switch {
	case os.Getenv("TF_CHAINGUARD_IDENTITY_TOKEN") != "":
		return os.Getenv("TF_CHAINGUARD_IDENTITY_TOKEN"), nil
	case providers.Enabled(ctx):
		return providers.Provide(ctx, lm.issuer)
	default:
		return lm.IdentityToken.ValueString(), nil
	}
}

// getChainguardToken gets a Chainguard token by launching a browser and having the user authenticate
// through the configured OIDC identity provider.
func getChainguardToken(ctx context.Context, lm loginModel) (string, error) {
	tflog.Info(ctx, "launching browser login flow")
	loginCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	// For each option, prefer the environment var, if set.
	return login.Login(loginCtx,
		login.WithIssuer(lm.issuer),
		login.WithAudience([]string{lm.audience}),
		login.WithClientID(auth0ClientID),
		login.WithIdentity(firstNonEmpty(os.Getenv("TF_CHAINGUARD_IDENTITY"), lm.Identity.ValueString())),
		login.WithIdentityProvider(firstNonEmpty(os.Getenv("TF_CHAINGUARD_IDP"), lm.IdentityProvider.ValueString())),
		login.WithAuth0Connection(firstNonEmpty(os.Getenv("TF_CHAINGUARD_AUTH0_CONNECTION"), lm.Auth0Connection.ValueString())),
		login.WithOrgName(firstNonEmpty(os.Getenv("TF_CHAINGUARD_ORG_NAME"), lm.OrgName.ValueString())),
	)
}

// exchangeToken gets a Chainguard token by exchanging the given OIDC token or path to a token.
// No user interaction is required.
func exchangeToken(ctx context.Context, idToken string, lm loginModel) (string, error) {
	tflog.Info(ctx, "exchanging oidc token for chainguard token")

	// Test to see if identity token is a path or not.
	if _, err := os.Stat(idToken); err == nil {
		// Token was specified, and it is a path. Read the token in from that file.
		b, err := os.ReadFile(idToken)
		if err != nil {
			return "", err
		}
		idToken = string(b)
	}

	opts := []sts.ExchangerOption{
		sts.WithUserAgent(UserAgent),
	}
	if identity := firstNonEmpty(os.Getenv("TF_CHAINGUARD_IDENTITY"), lm.Identity.ValueString()); identity == "" {
		opts = append(opts, sts.WithIdentity(identity))
	}
	return sts.Exchange(ctx, lm.issuer, lm.audience, idToken, opts...)
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
