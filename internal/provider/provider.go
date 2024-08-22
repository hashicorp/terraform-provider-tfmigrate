// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ provider.Provider = &tfmProvider{}
)

const (
	GITHUB_TOKEN_ENV_NAME = "GITHUB_TOKEN"
	HCP_TERRAFORM_HOST    = "app.terraform.io"
)

// New is a helper function to simplify provider server and testing implementation.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &tfmProvider{
			version: version,
		}
	}
}

// tfmProvider is the provider implementation.
type tfmProvider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and ran locally, and "test" when running acceptance
	// testing.
	version string
}

// tfmProviderModel maps provider schema data to a Go type.
type tfmProviderModel struct {
	GithubToken types.String `tfsdk:"github_token"`
	Hostname    types.String `tfsdk:"hostname"`
}

// ProviderResourceData is a struct to hold the provider configuration data.
type ProviderResourceData struct {
	GithubToken string
	Hostname    string
}

// Metadata returns the provider type name.
func (p *tfmProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "tfmigrate"
	resp.Version = p.version
}

// Schema defines the provider-level schema for configuration data.
func (p *tfmProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"github_token": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "The Github PAT token to be used for creating Pull-Requests",
			},
			"hostname": schema.StringAttribute{
				Optional:    true,
				Sensitive:   false,
				Description: "The Hostname of the TFE instance to connect to, if empty will default to HCP Terraform at app.terraform.io",
			},
		},
	}
}

// Configure prepares a HashiCups API client for data sources and resources.
func (p *tfmProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	tflog.Info(ctx, "Configuring tfmigrate provider")
	// Retrieve provider data from configuration
	var config tfmProviderModel
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// If practitioner provided a configuration value for any of the
	// attributes, it must be a known value.

	if config.GithubToken.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("github_token"),
			"Unknown Github PAT Token",
			"The provider cannot create the Github API client as there is an unknown configuration value for the Github Token. "+
				"Either target apply the source of the value first, set the value statically in the configuration, or use the TFM_GITHUB_TOKEN environment variable.",
		)
	}

	if config.Hostname.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("hostname"),
			"Unknown Hostname",
			"The provider cannot initialize the TFE API client as there is an unknown configuration value for the Hostname. "+
				"Please set the value statically in the configuration.",
		)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	// Default values to environment variables, but override
	// with Terraform configuration value if set.

	githubToken := os.Getenv(GITHUB_TOKEN_ENV_NAME)
	hostname := HCP_TERRAFORM_HOST

	if !config.GithubToken.IsNull() {
		githubToken = config.GithubToken.ValueString()
	}

	if !config.Hostname.IsNull() {
		hostname = config.Hostname.ValueString()
	}

	// If any of the expected configurations are missing, return
	// errors with provider-specific guidance.

	if githubToken == "" {
		resp.Diagnostics.AddAttributeError(path.Root("github_token"), PROVIDER_PAT_TOKEN_MISSING,
			PROVIDER_PAT_TOKEN_MISSING_DETAILED)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	providerResourceData := ProviderResourceData{
		githubToken, hostname}

	// Required to pass this information into the resources.
	resp.ResourceData = providerResourceData
}

// DataSources defines the data sources implemented in the provider.
func (p *tfmProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}

// Resources defines the resources implemented in the provider.
func (p *tfmProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewTerraformInitResource,
		NewTerraformPlanResource,
		NewGitResetResource,
		NewGitCommitPushResource,
		NewGithubPrResource,
		NewDirectoryActionResource,
		NewStateMigrationResource,
	}
}
