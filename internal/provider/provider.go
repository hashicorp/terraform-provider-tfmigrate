// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"os"
	"strings"
	"terraform-provider-tfmigrate/internal/constants"
	gitops "terraform-provider-tfmigrate/internal/helper"
	gitUtil "terraform-provider-tfmigrate/internal/util/vcs/git"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	tokenValidator "terraform-provider-tfmigrate/internal/util/vcs/git/token_validator"

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
	GitTokenEnvName  = "TF_GIT_PAT_TOKEN"
	HcpTerraformHost = "app.terraform.io"
)

// tfmProvider is the provider implementation.
type tfmProvider struct {
	version               string
	gitOps                gitops.GitOperations
	tokenValidatorFactory tokenValidator.TokenValidatorFactory
}

// tfmProviderModel maps provider schema data to a Go type.
type tfmProviderModel struct {
	GitPatToken types.String `tfsdk:"git_pat_token"`
	Hostname    types.String `tfsdk:"hostname"`
}

// ProviderResourceData holds the provider configuration data.
type ProviderResourceData struct {
	GitPatToken string
	Hostname    string
}

// New is a helper function to simplify provider server and testing implementation.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &tfmProvider{
			version:               version,
			gitOps:                gitops.NewGitOperations(hclog.L(), gitUtil.NewGitUtil(hclog.L())),
			tokenValidatorFactory: tokenValidator.NewTokenValidatorFactory(context.Background(), hclog.L()),
		}
	}
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
			"git_pat_token": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "The Git Personal Access Token (PAT) to be used for creating pull or merge requests.",
			},
			"hostname": schema.StringAttribute{
				Optional:    true,
				Sensitive:   false,
				Description: "The hostname of the TFE instance to connect to. Defaults to HCP Terraform at app.terraform.io.",
			},
		},
	}
}

// Configure prepares the provider configuration.
func (p *tfmProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	tflog.Info(ctx, "Configuring tfmigrate provider")

	// Retrieve provider data from configuration
	var config tfmProviderModel
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Handle unknown values
	if config.GitPatToken.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("git_pat_token"),
			"Unknown TF_GIT_PAT_TOKEN",
			"The provider cannot initialize the Git client as the Git PAT token is unknown. Set it as an environment variable.",
		)
	}
	if config.Hostname.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("hostname"),
			"Unknown Hostname",
			"The provider cannot initialize the TFE API client as the hostname is unknown. Set it in configuration.",
		)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	// Default values to environment variables, but override
	// with Terraform configuration values if set
	gitPatToken := os.Getenv(GitTokenEnvName)
	hostname := HcpTerraformHost

	if !config.GitPatToken.IsNull() {
		gitPatToken = config.GitPatToken.ValueString()
	}
	if !config.Hostname.IsNull() {
		hostname = config.Hostname.ValueString()
	}

	// Validate configurations
	if gitPatToken == "" {
		resp.Diagnostics.AddError(
			"Missing Authentication Token",
			"The provider requires the Git PAT token to be configured either as a Terraform variable or an environment variable.",
		)
	}

	// Validate the Git PAT token against the remote service provider
	remoteName, err := p.gitOps.GetRemoteName()
	if err != nil {
		resp.Diagnostics.AddError(constants.ErrorFetchingRemote, err.Error())
	}
	repoUrl, err := p.gitOps.GetRemoteURL(remoteName)
	if err != nil || repoUrl == "" {
		resp.Diagnostics.AddError(strings.ToLower(fmt.Sprintf(constants.ErrorFetchingRemoteURL, err)), err.Error())
	}

	var repoIdentifier string
	if repoIdentifier = p.gitOps.GetRepoIdentifier(repoUrl); repoIdentifier == "" {
		resp.Diagnostics.AddError(strings.ToLower(fmt.Sprintf(constants.WarnNotOnGithubOrGitlab, repoUrl)), err.Error())
	}

	gitTokenValidator, err := p.tokenValidatorFactory.NewTokenValidator(p.gitOps.GetRemoteServiceProvider(repoUrl))
	if err != nil {
		resp.Diagnostics.AddError(strings.ToLower(fmt.Sprintf(constants.ErrorCreatingNewTokenvalidator, err)), err.Error())
	}

	if suggestion, err := gitTokenValidator.ValidateToken(repoUrl, repoIdentifier); err != nil {
		resp.Diagnostics.AddError(strings.ToLower(fmt.Sprintf(constants.ErrorValidatingGitToken, err)), err.Error())
		resp.Diagnostics.AddWarning("", suggestion)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	// Set the provider resource data
	resp.ResourceData = ProviderResourceData{
		GitPatToken: gitPatToken,
		Hostname:    hostname,
	}
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
