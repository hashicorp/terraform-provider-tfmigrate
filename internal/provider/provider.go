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

	"github.com/hashicorp/terraform-plugin-log/tflog"

	gitRemoteSvcProvider "terraform-provider-tfmigrate/internal/util/vcs/git/remote_svc_provider"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
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
	GitTokenEnvName        = "TF_GIT_PAT_TOKEN"
	HcpTerraformHost       = "app.terraform.io"
	HcpMigrateBranchPrefix = "hcp-migrate-"
)

// tfmProvider is the provider implementation.
type tfmProvider struct {
	version                     string
	gitOps                      gitops.GitOperations
	remoteVcsSvcProviderFactory gitRemoteSvcProvider.RemoteVcsSvcProviderFactory
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
			version:                     version,
			gitOps:                      gitops.NewGitOperations(context.Background(), gitUtil.NewGitUtil(context.Background())),
			remoteVcsSvcProviderFactory: gitRemoteSvcProvider.NewRemoteSvcProviderFactory(context.Background()),
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

	enableTokenValidation, diags := p.enableGitTokenValidation()
	if diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}
	if !enableTokenValidation {
		// Set the Hostname provider resource data
		resp.ResourceData = ProviderResourceData{
			GitPatToken: gitPatToken,
			Hostname:    hostname,
		}
		return
	}
	if resp.Diagnostics = p.validateGitPatToken(config.GitPatToken.ValueString()); resp.Diagnostics.HasError() {
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

func (p *tfmProvider) enableGitTokenValidation() (bool, diag.Diagnostics) {
	/*
	   1. Check if the working directory is a git repository or not isGitRepo() bool, err
	   2. Check if the current working directory has a .git folder isGitRoot() bool, err
	   3. If the Current working directory is a git repo isGitTreeClean() bool, err
	      and is the git root, then check if the current working directory is clean
	   4. Check the current branch has hcp-migrate prefix
	*/

	isGitRepo, err := p.gitOps.IsGitRepo()
	if err != nil {
		if strings.Contains(err.Error(), constants.ErrNotGitRepo) {
			return false, nil
		}
		return false, diag.Diagnostics{
			diag.NewErrorDiagnostic(
				"Error checking if the current working directory is a git repository",
				err.Error(),
			),
		}
	}
	isGitRoot, err := p.gitOps.IsGitRoot()
	if err != nil {
		return false, diag.Diagnostics{
			diag.NewErrorDiagnostic(
				"Error checking if the current working directory is a git root",
				err.Error(),
			),
		}
	}
	isGitTreeClean, err := p.gitOps.IsGitTreeClean()
	if err != nil {
		return false, diag.Diagnostics{
			diag.NewErrorDiagnostic(
				"Error checking if the current working directory is clean",
				err.Error(),
			),
		}
	}

	currentBranch, err := p.gitOps.GetCurrentBranch()
	if err != nil {
		return false, diag.Diagnostics{
			diag.NewErrorDiagnostic(
				"Error fetching current branch",
				err.Error(),
			),
		}
	}
	return isGitRepo && isGitRoot && isGitTreeClean && strings.HasPrefix(currentBranch, HcpMigrateBranchPrefix), nil
}
func (p *tfmProvider) validateGitPatToken(tokenFromProvider string) diag.Diagnostics {
	// Validate the Git PAT token against the remote service provider
	remoteName, err := p.gitOps.GetRemoteName()
	diagnostics := make(diag.Diagnostics, 0)
	if err != nil {
		diagnostics.AddError(fmt.Sprintf(constants.ErrorFetchingRemote, err.Error()), err.Error())
		return diagnostics
	}
	repoUrl, err := p.gitOps.GetRemoteURL(remoteName)
	if err != nil || repoUrl == "" {
		diagnostics.AddError(strings.ToLower(fmt.Sprintf(constants.ErrorFetchingRemoteURL, err)), err.Error())
		return diagnostics
	}
	var repoIdentifier string
	if repoIdentifier = p.gitOps.GetRepoIdentifier(repoUrl); repoIdentifier == "" {
		diagnostics.AddError(strings.ToLower(fmt.Sprintf(constants.WarnNotOnGithubOrGitlab, repoUrl)), "unable to determine the repository identifier")
		return diagnostics
	}
	remoteVcsSvcProvider, err := p.remoteVcsSvcProviderFactory.NewRemoteVcsSvcProvider(p.gitOps.GetRemoteServiceProvider(repoUrl))
	if err != nil {
		diagnostics.AddError(strings.ToLower(fmt.Sprintf(constants.ErrorCreatingNewTokenvalidator, err)), err.Error())
		return diagnostics
	}
	if suggestion, err := remoteVcsSvcProvider.ValidateToken(repoUrl, repoIdentifier, tokenFromProvider); err != nil {
		diagnostics.AddError(strings.ToLower(fmt.Sprintf(constants.ErrorValidatingGitToken, err)), err.Error())
		diagnostics.AddWarning("", suggestion)
		return diagnostics
	}
	return diagnostics
}
