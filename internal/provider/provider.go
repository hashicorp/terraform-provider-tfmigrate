// Copyright IBM Corp. 2024, 2025
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"terraform-provider-tfmigrate/internal/cli_errors"
	"terraform-provider-tfmigrate/internal/constants"
	"terraform-provider-tfmigrate/internal/gitops"
	tfeUtil "terraform-provider-tfmigrate/internal/util/tfe"
	gitUtil "terraform-provider-tfmigrate/internal/util/vcs/git"

	"github.com/hashicorp/terraform-plugin-framework/function"
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
	_ provider.Provider              = &tfmProvider{}
	_ provider.ProviderWithFunctions = &tfmProvider{}
)

const (
	GitTokenEnvName                   = "TF_GIT_PAT_TOKEN"
	HcpMigrateBranchPrefix            = "hcp-migrate-"
	HcpTerraformHost                  = "app.terraform.io"
	TfeHostEnvName                    = "TFE_HOSTNAME"
	TfeOrganizationEnvName            = "TFE_ORGANIZATION"
	TfeProjectEnvName                 = "TFE_PROJECT"
	TfeSslSkipVerifyEnvName           = "TFE_SSL_SKIP_VERIFY"
	TfeTokenEnvName                   = "TFE_TOKEN"
	TfMigrateResyncOnConvergedEnvName = "TF_MIGRATE_STACK_ALLOW_RESYNC_ON_CONVERGED"
)

// tfmProvider is the provider implementation.
type tfmProvider struct {
	version                     string
	gitOps                      gitops.GitOperations
	remoteVcsSvcProviderFactory gitRemoteSvcProvider.RemoteVcsSvcProviderFactory
	tfeUtility                  tfeUtil.TfeUtil
}

// tfmProviderModel maps provider schema data to a Go type.
type tfmProviderModel struct {
	AllowCommitPush types.Bool   `tfsdk:"allow_commit_push"`
	CreatePr        types.Bool   `tfsdk:"create_pr"`
	GitPatToken     types.String `tfsdk:"git_pat_token"`
	Hostname        types.String `tfsdk:"hostname"`
	SSLSkipVerify   types.Bool   `tfsdk:"ssl_skip_verify"`
	TfeToken        types.String `tfsdk:"tfe_token"`
}

// ProviderResourceData holds the provider configuration data.
type ProviderResourceData struct {
	AllowCommitPush bool            // AllowCommitPush determines if the provider should allow commit and push operations.
	CreatePr        bool            // CreatePr determines if the provider should create a pull request after pushing changes.
	GitPatToken     string          // GitPatToken is the Git Personal Access Token (PAT) used for creating pull or merge requests.
	Hostname        string          // Hostname is the hostname of the TFE instance.
	SslSkipVerify   bool            // SslSkipVerify determines if the provider should skip SSL verification for the TFE API.
	TfeToken        string          // TfeToken is the TFE token used for accessing the TFE API.
	TfeUtil         tfeUtil.TfeUtil // TfeUtil is the utility for interacting with TFE.
}

// New is a helper function to simplify provider server and testing implementation.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		ctx := context.Background()
		return &tfmProvider{
			version:                     version,
			gitOps:                      gitops.NewGitOperations(ctx, gitUtil.NewGitUtil(ctx)),
			remoteVcsSvcProviderFactory: gitRemoteSvcProvider.NewRemoteSvcProviderFactory(ctx),
			tfeUtility:                  tfeUtil.NewTfeUtil(ctx),
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
				Description: "The Git Personal Access Token (PAT) to be used for creating pull or merge requests. Not required for stacks migration as we do not support any vcs operation at the moment.",
			},
			"hostname": schema.StringAttribute{
				Optional:    true,
				Sensitive:   false,
				Description: "The hostname of the TFE instance. Can be configured using the TFE_HOSTNAME environment variable, If both are set, the configuration value takes precedence. Defaults to 'app.terraform.io' when neither is set.",
			},
			"allow_commit_push": schema.BoolAttribute{
				Optional:    true,
				Sensitive:   false,
				Description: "Allow commit and then push to the remote branch. Not required for stacks migration as we do not support any vcs operation at the moment.",
			},
			"create_pr": schema.BoolAttribute{
				Optional:    true,
				Sensitive:   false,
				Description: "Create a pull request after pushing the changes. Not required for stacks migration as we do not support any vcs operation at the moment.",
			},
			"tfe_token": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "The TFE token to be used for accessing the TFE API. Can be configured using the TFE_TOKEN environment variable, If both are set, the configuration value takes precedence. Uses the TFE login token when neither is set.",
			},
			"ssl_skip_verify": schema.BoolAttribute{
				Optional:    true,
				Sensitive:   false,
				Description: "Skip SSL verification for the TFE API. Can be configured using the TFE_SSL_SKIP_VERIFY environment variable. If both are set, the configuration value takes precedence. Defaults to false when neither is set.",
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
			fmt.Sprintf("The provider cannot initialize the Git client as the Git PAT token is unknown. Set it as an environment variable or use the %s environment variable.", constants.GitTokenEnvName),
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

	if config.TfeToken.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("tfe_token"),
			"Unknown TFE Token",
			"The provider cannot initialize the TFE client as the TFE token is unknown. Set it as an environment variable or in configuration.",
		)
	}

	if config.SSLSkipVerify.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("ssl_skip_verify"),
			"Unknown SSL Skip Verify",
			"The provider cannot initialize the TFE client as the SSL skip verify is unknown. Set it in configuration.",
		)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	// configure host name
	hostname, diags := p.getProviderHostNameConfig(config.Hostname)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// configure tfe token
	tfeToken, diags := p.getProviderTfeTokenConfig(config.TfeToken, hostname)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// configure SSL skip verify
	sslSkipVerify, diags := p.getProviderSSLSkipVerifyConfig(config.SSLSkipVerify)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Retrieve environment variables
	gitPatToken := os.Getenv(constants.GitTokenEnvName)

	if !config.GitPatToken.IsNull() {
		gitPatToken = config.GitPatToken.ValueString()
	}

	var allowCommitPush, createPr bool

	if !config.AllowCommitPush.IsNull() {
		allowCommitPush = config.AllowCommitPush.ValueBool()
	}

	if !config.CreatePr.IsNull() {
		createPr = config.CreatePr.ValueBool()
	}

	if err := p.validateGitOpsReadiness(allowCommitPush, &createPr, gitPatToken); err != nil {
		resp.Diagnostics.AddError("Git Operations Validation Error: "+err.Error(), err.Error())
		return
	}

	// Set the provider resource data
	resp.ResourceData = ProviderResourceData{
		AllowCommitPush: allowCommitPush,
		CreatePr:        createPr,
		GitPatToken:     gitPatToken,
		Hostname:        hostname,
		SslSkipVerify:   sslSkipVerify,
		TfeToken:        tfeToken,
		TfeUtil:         p.tfeUtility,
	}
}

// DataSources defines the data sources implemented in the provider.
func (p *tfmProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}

// Resources define the resources implemented in the provider.
func (p *tfmProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewTerraformInitResource,
		NewTerraformPlanResource,
		NewGitResetResource,
		NewGitCommitPushResource,
		NewGitPrResource,
		NewDirectoryActionResource,
		NewStateMigrationResource,
		NewStackMigrationResource,
	}
}

// Functions defines the functions implemented in the provider.
func (p *tfmProvider) Functions(_ context.Context) []func() function.Function {
	return []func() function.Function{
		NewDecodeStackMigrationHashToJson,
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

// validateGitOpsReadiness checks if the Git operations are ready for commit and push or PR creation.
func (p *tfmProvider) validateGitOpsReadiness(allowCommitPush bool, createPr *bool, tokenFromProvider string) error {
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

// getProviderHostNameConfig retrieves the hostname configuration for the provider.
func (p *tfmProvider) getProviderHostNameConfig(hostNameConfigVal types.String) (string, diag.Diagnostics) {
	var hostName string
	var diags diag.Diagnostics
	configValueIsNotNull := false

	if !hostNameConfigVal.IsNull() {
		hostName = hostNameConfigVal.ValueString()
		configValueIsNotNull = true
	}

	if hostName != "" {
		return hostName, diags
	}

	if configValueIsNotNull {
		diags.AddAttributeError(
			path.Root("hostname"),
			"Invalid Hostname",
			"The hostname cannot be empty. Please provide a valid hostname or set the TFE_HOSTNAME environment variable. If both are set, the configuration value takes precedence.")
		return "", diags
	}

	if hostNameEnvVal, envVariableSet := os.LookupEnv(TfeHostEnvName); !envVariableSet {
		hostName = HcpTerraformHost // Default to HCP Terraform host
	} else {
		hostName = hostNameEnvVal // Use the environment variable value if set
	}

	if hostName != "" {
		return hostName, diags
	}

	diags.AddAttributeError(
		path.Root("hostname"),
		"Invalid Hostname",
		"The hostname cannot be empty. Please provide a valid hostname or set the TFE_HOSTNAME environment variable. If both are set, the configuration value takes precedence.")
	return "", diags
}

// getProviderTfeTokenConfig retrieves the TFE token configuration for the provider.
func (p *tfmProvider) getProviderTfeTokenConfig(tfeTokenConfigVal types.String, hostName string) (string, diag.Diagnostics) {
	var tfeToken string
	var diags diag.Diagnostics
	var configValueIsNotNull bool

	if !tfeTokenConfigVal.IsNull() {
		tfeToken = tfeTokenConfigVal.ValueString()
		configValueIsNotNull = true
	}

	if tfeToken != "" {
		return tfeToken, diags
	}

	// If the configured token value via the `tfe_token` attribute is not null but empty,
	// add an error to the diagnostics and return
	if configValueIsNotNull {
		diags.AddAttributeError(
			path.Root("tfe_token"),
			"Invalid TFE Token",
			"The TFE token cannot be empty. Please provide a valid TFE token or set the TFE_TOKEN environment variable. If both are set, the configuration value takes precedence.")
		return "", diags
	}

	tfeTokenEnvVal, envVariableSet := os.LookupEnv(TfeTokenEnvName)
	if envVariableSet {
		// If the `TFE_TOKEN` environment variable is set to an empty string,
		// add an error to the diagnostics and return
		if tfeTokenEnvVal == "" {
			diags.AddAttributeError(
				path.Root("tfe_token"),
				"Invalid TFE Token",
				"The TFE token cannot be empty. Please provide a valid TFE token or set the TFE_TOKEN environment variable. If both are set, the configuration value takes precedence.")
			return "", diags
		}
		return tfeTokenEnvVal, diags
	}

	// if the `TFE_TOKEN` environment variable is not set,
	// read the TFE login token
	tfeToken, err := p.tfeUtility.ReadTfeToken(hostName)
	if err != nil {
		// If there is an error reading the TFE token,
		// add an error to the diagnostics and return
		diags.AddError(
			"Error Reading TFE Token",
			fmt.Sprintf("An error occurred while reading the TFE token: %s", err.Error()),
		)
		return "", diags
	}

	if tfeToken == "" {
		// If the read token is empty,
		// add an error to the diagnostics and return
		diags.AddAttributeError(
			path.Root("tfe_token"),
			"Empty TFE Login Token",
			"The TFE login token is empty. Run terraform login to get a valid token ")
		return "", diags
	}

	return tfeToken, diags

}

// getProviderSSLSkipVerifyConfig retrieves the SSL skip verify configuration for the provider.
func (p *tfmProvider) getProviderSSLSkipVerifyConfig(sslSkipVerifyConfigVal types.Bool) (bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	if !sslSkipVerifyConfigVal.IsNull() {
		return sslSkipVerifyConfigVal.ValueBool(), diags
	}

	// If the configured value via the `ssl_skip_verify` attribute is not set try to read the environment variable
	sslSkipVerifyEnvVal, envVariableSet := os.LookupEnv(TfeSslSkipVerifyEnvName)
	if !envVariableSet {
		return false, diags // Default to false if not set
	}

	if sslSkipVerifyEnvVal == "" {
		diags.AddAttributeError(
			path.Root("ssl_skip_verify"),
			"Invalid SSL Skip Verify Value",
			"The SSL skip verify value cannot be empty. Please provide a valid boolean value or set the TFE_SSL_SKIP_VERIFY environment variable. If both are set, the configuration value takes precedence.",
		)
		return false, diags
	}

	// If the environment variable is set, parse it to a boolean
	sslSkipVerifyBolVal, err := strconv.ParseBool(sslSkipVerifyEnvVal)
	if err != nil {
		diags.AddAttributeError(
			path.Root("ssl_skip_verify"),
			"Invalid SSL Skip Verify Value",
			fmt.Sprintf("The value for %s is not a valid boolean: %s", TfeSslSkipVerifyEnvName, err.Error()),
		)
		return false, diags
	}

	return sslSkipVerifyBolVal, diags
}

func (p *tfmProvider) handleGitTokenValidatorError(createPr *bool, errorMessage string) error {
	if errorMessage == cli_errors.ErrTokenDoesNotHavePrWritePermission.Error() {
		*createPr = false
		return nil
	}
	return errors.New(errorMessage)
}
