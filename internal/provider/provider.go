/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"chainguard.dev/api/pkg/auth"
	"chainguard.dev/api/proto/platform"
)

const (
	chainguardTokenFilename = "oidc-token"

	EnvChainguardConsoleAPI = "CHAINGUARD_CONSOLE_API"
	DefaultConsoleAPI       = "https://console-api.enforce.dev"

	EnvAccAudience   = "TF_ACC_AUDIENCE"
	EnvAccConsoleAPI = "TF_ACC_CONSOLE_API"
	EnvAccGroupID    = "TF_ACC_GROUP_ID"
	EnvAccIssuer     = "TF_ACC_ISSUER"
)

var EnvAccVars = []string{
	EnvAccAudience,
	EnvAccConsoleAPI,
	EnvAccGroupID,
	// TODO(colin): uncomment after implementing identities
	//EnvAccIssuer,
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
}

// Metadata returns the provider type name.
func (p *Provider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "chainguard"
	resp.Version = p.version
}

// Schema defines the provider-level schema for configuration data.
func (p *Provider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manage resources on the Chainguard platform.",
		Attributes: map[string]schema.Attribute{
			"console_api": schema.StringAttribute{
				Optional:    true,
				Description: "URL of Chainguard console API. Ensure a valid token has been generated for this URL with `chainctl auth login`.",
			},
		},
	}
}

type providerData struct {
	client  platform.Clients
	version string
}

// Configure prepares a Chainguard API client for data sources and resources.
func (p *Provider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data ProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	// TODO(colin): data validation
	if resp.Diagnostics.HasError() {
		return
	}

	// TODO(colin): is this order of precedence correct?
	// Load default values and environment variables
	// Order of precedence for values:
	//   1. Environment variable
	//   2. Value from config
	//   3. Default value

	consoleAPI := DefaultConsoleAPI
	if data.ConsoleAPI.ValueString() != "" {
		consoleAPI = data.ConsoleAPI.ValueString()
	}
	if v, ok := os.LookupEnv(EnvChainguardConsoleAPI); ok {
		consoleAPI = v
	}

	audience := consoleAPI
	if p.version == "acctest" {
		// In acceptance tests override the console api and audience from env var
		tflog.Info(ctx, "** Running Acceptance Tests **")
		consoleAPI = os.Getenv(EnvAccConsoleAPI)
		audience = os.Getenv(EnvAccAudience)
	}

	ctx = tflog.SetField(ctx, "chainguard.console_api", consoleAPI)
	tflog.Info(ctx, "configuring chainguard client")

	token, err := loadChainguardToken(audience)
	if err != nil {
		resp.Diagnostics.AddAttributeError(
			path.Root("console_api"),
			"failed to retrieve Chainguard token",
			fmt.Sprintf("Either no token was found for audience %q or there was an error reading it.\n"+
				"Please check the value of \"chainguard.console_api\" in your Terraform provider configuration, "+
				"and log in to the Chainguard platform with `chainctl auth login` to generate a valid token.", audience))
		return
	}

	cred := auth.NewFromToken(ctx, fmt.Sprintf("Bearer %s", token), false)
	ctx = platform.WithUserAgent(ctx, fmt.Sprintf("terraform-provider-chainguard/%s %s/%s", p.version, runtime.GOOS, runtime.GOARCH))
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
		version: p.version,
	}

	resp.DataSourceData = d
	resp.ResourceData = d
}

// DataSources defines the data sources implemented in the provider.
func (p *Provider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewClusterCIDRDataSource,
		NewClusterDiscoveryDataSource,
		NewGroupDataSource,
		NewIdentityDataSource,
		NewRoleDataSource,
	}
}

// Resources defines the resources implemented in the provider.
func (p *Provider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewGroupResource,
		NewRoleResource,
	}
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

// The following functions cacheFilePath, loadChainguardToken, latestChainguardToken,
// and getChainguardTokenLocation are copied from commands.go in chainctl

// expandJoin takes the Chainguard config root, expands the path, and returns
// the expanded path joined with the passed in file name.
// expandJoin takes the Chainguard config root, expands the path, and returns
// the expanded path joined with the passed in file name.
func cacheFilePath(file string) (string, error) {
	path, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	path = filepath.Join(path, "chainguard", file)
	if _, err := os.Stat(filepath.Dir(path)); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
			return "", err
		}
	}
	return path, nil
}

// loadChainguardToken loads the chainguard token from the configured cache directory.
func loadChainguardToken(audience string) ([]byte, error) {
	path, err := getChainguardTokenLocation(audience)
	if err != nil {
		return nil, err
	}

	return os.ReadFile(path)
}

func getChainguardTokenLocation(audience string) (string, error) {
	// first, try to get a token specific to the audience
	a := strings.ReplaceAll(audience, "/", "-")
	fp := filepath.Join(a, chainguardTokenFilename)
	return cacheFilePath(fp)
}
