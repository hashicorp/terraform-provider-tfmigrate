// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"terraform-provider-tfmigrate/internal/cli_errors"
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
	GitPatToken     types.String `tfsdk:"git_pat_token"`
	Hostname        types.String `tfsdk:"hostname"`
	AllowCommitPush types.Bool   `tfsdk:"allow_commit_push"`
	CreatePr        types.Bool   `tfsdk:"create_pr"`
}

// ProviderResourceData holds the provider configuration data.
type ProviderResourceData struct {
	GitPatToken     string
	Hostname        string
	AllowCommitPush bool
	CreatePr        bool
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
			"allow_commit_push": schema.BoolAttribute{
				Optional:    true,
				Sensitive:   false,
				Description: "Allow commit and then push to the remote branch.",
			},
			"create_pr": schema.BoolAttribute{
				Optional:    true,
				Sensitive:   false,
				Description: "Create a pull request after pushing the changes.",
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
	if config.AllowCommitPush.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("allow_commit_push"),
			"Unknown Allow Commit Push",
			"The provider cannot initialize the Git client as the allow push commit is unknown. Set it in configuration.",
		)
	}
	if config.CreatePr.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("create_pr"),
			"Unknown Create PR",
			"The provider cannot initialize the Git client as the create PR is unknown. Set it in configuration.",
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

	var allowCommitPush, createPr bool

	if !config.AllowCommitPush.IsNull() {
		allowCommitPush = config.AllowCommitPush.ValueBool()
	}
	if !config.CreatePr.IsNull() {
		createPr = config.CreatePr.ValueBool()
	}

	if err := p.ValidateGitOpsReadiness(allowCommitPush, &createPr, gitPatToken); err != nil {
		resp.Diagnostics.AddError("Git Operations Validation Error: "+err.Error(), err.Error())
		return
	}

	// Set the provider resource data
	resp.ResourceData = ProviderResourceData{
		GitPatToken:     gitPatToken,
		Hostname:        hostname,
		AllowCommitPush: allowCommitPush,
		CreatePr:        createPr,
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

// ValidateGitPatToken validates the Git PAT token against the remote service provider.
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
		diagnostics.AddError(strings.ToLower(fmt.Sprintf(constants.WarnNotOnSupportedVcsProvider, repoUrl)), "unable to determine the repository identifier")
		return diagnostics
	}
	remoteVcsSvcProvider, err := p.remoteVcsSvcProviderFactory.NewRemoteVcsSvcProvider(p.gitOps.GetRemoteServiceProvider(repoUrl))
	if err != nil {
		diagnostics.AddError(strings.ToLower(fmt.Sprintf(constants.ErrorCreatingNewTokenvalidator, err)), err.Error())
		return diagnostics
	}
	if suggestion, err := remoteVcsSvcProvider.ValidateToken(repoUrl, repoIdentifier, tokenFromProvider); err != nil {
		tflog.Error(context.Background(), fmt.Sprintf(constants.ErrorValidatingGitToken, err))
		diagnostics.AddError(strings.ToLower(err.Error()), fmt.Sprintf(constants.ErrorValidatingGitToken, err))
		diagnostics.AddWarning("", suggestion)
		return diagnostics
	}
	return diagnostics
}

func (p *tfmProvider) ValidateGitOpsReadiness(allowCommitPush bool, createPr *bool, tokenFromProvider string) error {
	if !allowCommitPush && !*createPr {
		return nil
	}
	isGitRepo, err := p.gitOps.IsGitRepo()
	if err != nil {
		return fmt.Errorf("error checking if directory is a git repository: %w", err)
	}
	if !isGitRepo {
		return fmt.Errorf("current directory is not a git repository")
	}
	isGitRoot, err := p.gitOps.IsGitRoot()
	if err != nil {
		return fmt.Errorf("error checking if directory is a git root: %w", err)
	}
	if !isGitRoot {
		return fmt.Errorf("current directory is not a git root")
	}
	remoteName, err := p.gitOps.GetRemoteName()
	if err != nil {
		return fmt.Errorf("error getting remote name: %w", err)
	}
	repoUrl, err := p.gitOps.GetRemoteURL(remoteName)
	if err != nil {
		return err
	}
	if !p.gitOps.IsSupportedVCSProvider(repoUrl) {
		return fmt.Errorf("unsupported VCS provider")
	}
	validatedTokenAlready := false
	if allowCommitPush && !p.gitOps.IsSSHUrl(repoUrl) {
		diagnostics := p.validateGitPatToken(tokenFromProvider)
		if diagnostics.HasError() {
			for _, diag := range diagnostics.Errors() {
				return p.handleGitTokenValidatorError(createPr, diag.Summary())
			}
		}
		validatedTokenAlready = true
	}
	if *createPr {
		if !validatedTokenAlready {
			diagnostics := p.validateGitPatToken(tokenFromProvider)
			if diagnostics.HasError() {
				for _, diag := range diagnostics.Errors() {
					return p.handleGitTokenValidatorError(createPr, diag.Summary())
				}
			}
		}
	}
	return nil
}

func (p *tfmProvider) handleGitTokenValidatorError(createPr *bool, errorMessage string) error {
	if errorMessage == cli_errors.ErrTokenDoesNotHavePrWritePermission.Error() {
		*createPr = false
		return nil
	}
	return errors.New(errorMessage)
}
