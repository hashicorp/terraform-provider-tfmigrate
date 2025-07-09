// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"terraform-provider-tfmigrate/internal/constants"
	tfeUtil "terraform-provider-tfmigrate/internal/util/tfe"

	"github.com/hashicorp/go-tfe"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// Define constants for the stack resource.
const (
	stackMigrationResourceName = `_stack_migration` // stackMigrationResourceName is the name of the resource for migrating existing HCP Terraform workspaces to deployments within a non-VCS stack.

	// Attribute names.
	attrConfigurationDir           = `config_file_dir`              // attrConfigurationDir is the attribute for the directory containing the stack configuration files.
	attrCurrentConfigurationId     = `current_configuration_id`     // attrCurrentConfigurationId is the attribute for the ID of the current stack configuration.
	attrCurrentConfigurationStatus = `current_configuration_status` // attrCurrentConfigurationStatus is the attribute for the status of the stack configuration.
	attrName                       = `name`                         // attrName is the attribute for the name of the stack.
	attrOrganization               = `organization`                 // attrOrganization is the attribute for the HCP Terraform organization name in which the stack exists.
	attrProject                    = `project`                      // attrProject is the attribute for the HCP Terraform project name in which the stack exists.
	attrSourceBundleHash           = `source_bundle_hash`           // attrSourceBundleHash is the attribute for the hash of the configuration files in the directory.

	/* Attribute markdown descriptions. */

	// configurationDirDescription is the Markdown description for the `config_file_dir` attribute.
	configurationDirDescription = `The directory path containing configuration files. Must be an absolute path.`

	// the currentConfigurationIdDescription is the Markdown description for the `current_configuration_id` attribute.
	currentConfigurationIdDescription = `The ID of the current stack configuration. This is used to track the current configuration of the stack.`

	// currentConfigurationStatusDescription is the Markdown description for the `current_configuration_status` attribute.
	currentConfigurationStatusDescription = `The status of the stack configuration. This is used to track the status of the stack configuration upload.`

	// nameDescription is the Markdown description for the `name` attribute.
	nameDescription = `The stack name. Must be unique within the organization and project, must be a non-VCS driven stack.`

	// organizationDescription is the Markdown description for the `organization` attribute.
	organizationDescription = `The organization name to which the stack belongs. This must reference an existing organization. Either this attribute or the TFE_ORGANIZATION environment variable is required; if both are set, the attribute value takes precedence.`

	// projectDescription is the Markdown description for the `project` attribute.
	projectDescription = `The project name to which the stack belongs. This must reference an existing project. Either this attribute or the TFE_PROJECT environment variable is required; if both are set, the attribute value takes precedence.`

	// sourceBundleHashDescription is the Markdown description for the `source_bundle_hash` attribute.
	sourceBundleHashDescription = `The hash of the configuration files in the directory. This is used to detect changes in the stack configuration files.`

	// stackMigrationResourceDescription is the description of the resource for migrating existing HCP Terraform workspaces to deployments within a non-VCS stack.
	stackMigrationResourceDescription = `Defines a resource for migrating existing HCP Terraform workspaces to deployments within a non-VCS stack. Each workspace maps one-to-one with a stack deployment. The resource uploads configuration files to the stack and monitors the upload and deployment status.`

	/* validator constants. */

	// organizationNameConstraintViolationMsg is the error message for organization name constraint violations.
	organizationNameConstraintViolationMsg = `The organization name must be between 3 and 40 characters long and may contain valid characters including ASCII letters, numbers, spaces, as well as dashes (-), and underscores (_).`

	// projectNameConstraintViolationMsg is the error message for project name constraint violations.
	projectNameConstraintViolationMsg = `The project name must be between 3 and 40 characters long and may contain valid characters including ASCII letters, numbers, spaces, as well as dashes (-), and underscores (_).`

	// stackNameConstraintViolationMsg is the error message for stack name constraint violations.
	stackNameConstraintViolationMsg = `The stack name must be between 1 and 90 characters long and may contain valid characters including ASCII letters, numbers, spaces, as well as dashes (-), and underscores (_).`

	/* Configuration hash calculation error constants. */

	// configHasErrSummary is the summary for the error when the configuration hash cannot be calculated.
	configHasErrSummary = `Unable to Calculate Configuration Hash`

	// configHashErrDetails is the detailed error message when the configuration hash cannot be calculated.
	configHashErrDetails = `Could not calculate the hash of the configuration files in the directory %q, err: %s`

	/* Configuration-status-based final log message. */

	// configCanceled is the final log message when the most recent stack configuration has the status tfe.StackConfigurationStatusCanceled.
	configCanceled = "The most recent stack configuration %s for stack %s has been canceled, once you are ready to proceed, run `terraform apply` again"

	// configConverged is the final log message when the most recent stack configuration has the status tfe.StackConfigurationStatusConverged.
	configConverged = `The most recent configuration %s for stack %s has converged successfully.`

	// configConverging is the final log message when the most recent stack configuration has the status tfe.StackConfigurationStatusConverging.
	configConverging = `The most recent stack configuration %s for stack %s is converging. This means the configuration is currently rolling out to the stack. You can approve/discard/monitor the progress in the HCP Terraform UI.`

	// configErrored is the final log message when the most recent stack configuration has the status tfe.StackConfigurationStatusErrored.
	configErrored = "The most recent stack configuration %s for stack %s has errored, please modify the configuration files to resolve the issues and run ` terraform apply` again."

	// configTransitioning is the final log message when the most recent stack configuration is
	// in one of the statuses other than:
	//   - tfe.StackConfigurationStatusCanceled.
	//   - tfe.StackConfigurationStatusConverged
	//   - tfe.StackConfigurationStatusConverging
	//   - tfe.StackConfigurationStatusErrored
	configTransitioning = `The most recent stack configuration %s for stack %s is still in progress, with the status %s.
  If the configuration is awaiting approval, you can do one of the following in the HCP Terraform UI:
    - Approve the configuration to apply the changes.
    - Cancel the configuration to stop the rollout.
  Otherwise wait for it to reach a terminal status (converged, converging, errored, or canceled) before running again`

	/* Final log message metadata keys. */

	organizationNameMetadata         = "organization_name"          // organizationNameMetadata is the key for the organization name in the final log message metadata.
	projectNameMetadata              = "project_name"               // projectNameMetadata is the key for the project name in the final log message metadata.
	stackConfigurationIdMetadata     = "stack_configuration_id"     // stackConfigurationIdMetadata is the key for the stack configuration ID in the final log message metadata.
	stackConfigurationStatusMetadata = "stack_configuration_status" // stackConfigurationStatusMetadata is the key for the stack configuration status in the final log message metadata.
	stackNameMetadata                = "stack_name"                 // stackNameMetadata is the key for the stack name in the final log message metadata.

)

var (
	_ resource.Resource              = &stackMigrationResource{}
	_ resource.ResourceWithConfigure = &stackMigrationResource{}

	nameRegex = regexp.MustCompile(`^[a-zA-Z0-9 _-]{3,40}$`) // nameRegex Regex for valid organization, project, and stack names

)

// StackMigrationResourceModel is the data model for the stack migration resource used by the Terraform provider
// to create, read, and update tfmigrate_stack_migration resource's state.
type StackMigrationResourceModel struct {
	ConfigurationDir           types.String `tfsdk:"config_file_dir"`              // ConfigurationDir is the absolute directory path containing stack configuration files.
	CurrentConfigurationId     types.String `tfsdk:"current_configuration_id"`     // CurrentConfigurationId is the ID of the current stack configuration.
	CurrentConfigurationStatus types.String `tfsdk:"current_configuration_status"` // CurrentConfigurationStatus  is the status of the stack configuration.
	Name                       types.String `tfsdk:"name"`                         // Name is the name of the stack Must be unique within the organization and project, must be a non-Vcs driven stack.
	Organization               types.String `tfsdk:"organization"`                 // Organization is the HCP Terraform organization name in which the stack exists. The value can be provided by the `TFE_ORGANIZATION` environment variable.
	Project                    types.String `tfsdk:"project"`                      // Project is the HCP Terraform project name in which the stack exists. The value can be provided by the `TFE_PROJECT` environment variable.
	SourceBundleHash           types.String `tfsdk:"source_bundle_hash"`           // SourceBundleHash is the hash of the configuration files in the directory. This is used to detect changes in the configuration files.
}

// stackMigrationResource implements the resource.Resource interface for managing stack migrations in HCP Terraform.
type stackMigrationResource struct {
	existingStack        *tfe.Stack        // an existingStack is the stack to which the workspace will be migrated.
	existingOrganization *tfe.Organization // an existingOrganization is the organization in which the stack exists.
	existingProject      *tfe.Project      // an existingProject is the project in which the stack exists.
	tfeClient            *tfe.Client       // tfeClient is the TFE client used to interact with the HCP Terraform API.
	tfeUtil              tfeUtil.TfeUtil   // tfeUtil is the utility for interacting with the TFE API, used to perform operations like uploading stack configurations and calculating source bundle hashes.
}

// NewStackMigrationResource creates a new instance of the stack migration resource.
func NewStackMigrationResource() resource.Resource {
	return &stackMigrationResource{}
}

// Schema defines the schema for the stack migration resource.
func (r *stackMigrationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Version:             1,
		Description:         stackMigrationResourceDescription,
		MarkdownDescription: stackMigrationResourceDescription,
		Attributes: map[string]schema.Attribute{
			attrConfigurationDir: schema.StringAttribute{
				MarkdownDescription: configurationDirDescription,
				Required:            true,
				Validators: []validator.String{
					&absolutePathValidator{},
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			attrCurrentConfigurationId: schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: currentConfigurationIdDescription,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			attrCurrentConfigurationStatus: schema.StringAttribute{
				MarkdownDescription: currentConfigurationStatusDescription,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			attrName: schema.StringAttribute{
				MarkdownDescription: nameDescription,
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 90),
					stringvalidator.RegexMatches(nameRegex, stackNameConstraintViolationMsg),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			attrOrganization: schema.StringAttribute{
				MarkdownDescription: organizationDescription,
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.LengthBetween(3, 40),
					stringvalidator.RegexMatches(nameRegex, organizationNameConstraintViolationMsg),
					&orgEnvNameValidator{},
				},
				Default: stringdefault.StaticString(os.Getenv(TfeOrganizationEnvName)),
			},
			attrProject: schema.StringAttribute{
				MarkdownDescription: projectDescription,
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.LengthBetween(3, 40),
					stringvalidator.RegexMatches(nameRegex, projectNameConstraintViolationMsg),
					&projectEnvNameValidator{},
				},
				Default: stringdefault.StaticString(os.Getenv(TfeProjectEnvName)),
			},
			attrSourceBundleHash: schema.StringAttribute{
				MarkdownDescription: sourceBundleHashDescription,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

// Create is called when the resource is created. It uploads the stack configuration files to the stack and waits for the stack configuration to converge, cancel, or error out.
func (r *stackMigrationResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
	var plan StackMigrationResourceModel
	tflog.Debug(ctx, "Starting Create operation for stack migration resource")

	// retrieve values from the plan
	response.Diagnostics.Append(request.Plan.Get(ctx, &plan)...)
	if response.Diagnostics.HasError() {
		return
	}
	tflog.Debug(ctx, fmt.Sprintf("Received plan for stack migration resource: %+v", plan))

	// update the context of the tfeUtil with the current context
	r.tfeUtil.UpdateContext(ctx)

	// start the stack migration process
	tflog.Info(ctx, "Starting to apply stack migration configuration to a new stack migration resource")
	state, diags := r.applyStackConfiguration(ctx, plan.Organization.ValueString(), plan.Project.ValueString(), plan.Name.ValueString(), plan.ConfigurationDir.ValueString())
	response.Diagnostics.Append(diags...)
	if response.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, "Successfully applied stack migration configuration to a new stack migration resource")
	response.Diagnostics.Append(response.State.Set(ctx, &state)...)
}

func (r *stackMigrationResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
	var state StackMigrationResourceModel
	var err error
	r.tfeUtil.UpdateContext(ctx)

	// read the current state
	response.Diagnostics.Append(request.State.Get(ctx, &state)...)
	if response.Diagnostics.HasError() {
		tflog.Error(ctx, "Failed to get state values")
		return
	}

	tflog.Info(ctx, "Successfully read the current state for stack migration resource")

	// read the organization.
	if r.existingOrganization, err = r.tfeUtil.ReadOrgByName(state.Organization.ValueString(), r.tfeClient); err != nil {
		response.Diagnostics.AddError(
			"Error Reading organization",
			fmt.Sprintf("The organization %q does not exist or could not be accessed: %s", state.Organization.ValueString(), err.Error()),
		)
	}

	if response.Diagnostics.HasError() {
		return
	}

	// read the project.
	r.existingProject, err = r.tfeUtil.ReadProjectByName(r.existingOrganization.Name, state.Project.ValueString(), r.tfeClient)
	if err != nil {
		response.Diagnostics.AddError(
			"Error Reading project",
			fmt.Sprintf("The project %q does not exist or could not be accessed in organization %q: %s", state.Project.ValueString(), r.existingOrganization.Name, err.Error()),
		)
	}

	if response.Diagnostics.HasError() {
		return
	}

	// check read stack already.
	r.existingStack, err = r.tfeUtil.ReadStackByName(r.existingOrganization.Name, r.existingProject.ID, state.Name.ValueString(), r.tfeClient)
	if err != nil {
		response.Diagnostics.AddError(
			"Error Reading stack",
			fmt.Sprintf("The stack %q does not exist or could not be accessed in organization %q and project %q: %s", state.Name.ValueString(), r.existingOrganization.Name, r.existingProject.Name, err.Error()),
		)
	}

	if response.Diagnostics.HasError() {
		return
	}

	// if the stack is a VCS-driven stack, throw an error.
	if r.existingStack.VCSRepo != nil {
		response.Diagnostics.AddError(
			"Migration to VCS backed stacks is not supported",
			fmt.Sprintf("The stack %q in organization %q and project %q is a VCS backed stacks. The `tfmigrate_stack_migration` resource supports migration to non-VCS backed stacks only ", r.existingStack.Name, r.existingOrganization.Name, r.existingProject.Name),
		)
		return
	}

	if response.Diagnostics.HasError() {
		return
	}

	/* NOTE:
	   calculate the hash of the configuration files in the directory
	   during raed it is assumed that the hash of the configuration files
	   provided in the config_file_dir is the same as the one that is
	   responsible for the current stack configuration state. Hence, we
	   calculate the hash of the configuration files in the directory
	   and set it to the source_bundle_hash attribute in the state.
	*/
	sourceBundleHash, err := r.tfeUtil.CalculateStackSourceBundleHash(state.ConfigurationDir.ValueString())
	if err != nil {
		response.Diagnostics.AddError(
			"Error Calculating Configuration Hash",
			fmt.Sprintf("Could not calculate the hash of the configuration files in the directory %q: %s", state.ConfigurationDir.ValueString(), err.Error()),
		)
		return
	}

	// update the values in the updatedState
	updatedState := StackMigrationResourceModel{}
	updatedState.ConfigurationDir = state.ConfigurationDir
	updatedState.CurrentConfigurationId = types.StringValue(r.existingStack.LatestStackConfiguration.ID)
	updatedState.CurrentConfigurationStatus = types.StringValue(r.existingStack.LatestStackConfiguration.Status)
	updatedState.Name = types.StringValue(r.existingStack.Name)
	updatedState.Organization = types.StringValue(r.existingOrganization.Name)
	updatedState.Project = types.StringValue(r.existingProject.Name)
	updatedState.SourceBundleHash = types.StringValue(sourceBundleHash)

	// save the updated state
	response.Diagnostics.Append(response.State.Set(ctx, &updatedState)...)

	tflog.Info(ctx, "Successfully updated the state for stack migration resource")

}

func (r *stackMigrationResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
	var plan, state StackMigrationResourceModel
	var _ diag.Diagnostics
	var err error

	// Retrieve values from the plan
	response.Diagnostics.Append(request.Plan.Get(ctx, &plan)...)
	if response.Diagnostics.HasError() {
		tflog.Error(ctx, "Failed to get plan values")
		return
	}

	// Retrieve values from the state
	response.Diagnostics.Append(request.State.Get(ctx, &state)...)
	if response.Diagnostics.HasError() {
		tflog.Error(ctx, "Failed to get state values")
		return
	}

	// handle write field changes
	if plan.Organization != state.Organization ||
		plan.Project != state.Project ||
		plan.Name != state.Name {
		panic("Upload configuration files and wait for the stack configuration to converge, cancel, or error out")
	}

	// Handle configuration directory changes
	// irrespective of the value difference between plan and state;
	// for `config_file_dir` attribute, we calculate the source bundle hash
	// if the current source bundle hash differs from the one in the state,
	// we check for updatability
	currentSourceBundleHash, err := r.tfeUtil.CalculateStackSourceBundleHash(plan.ConfigurationDir.ValueString())
	if err != nil {
		response.Diagnostics.AddError(
			"Error Calculating Configuration Hash",
			fmt.Sprintf("Could not calculate the hash of the configuration files in the directory %q: %s", plan.ConfigurationDir.ValueString(), err.Error()),
		)
		return
	}

	if currentSourceBundleHash != state.SourceBundleHash.ValueString() {
		panic("Upload configuration files and wait for the stack configuration to converge, cancel, or error out")
	}

	// Handle non-write field changes,
	// only one of the following fields can deffer between plan and state:
	// 1. `current_configuration_id`
	// 2. `current_configuration_status`

	// Handle current_configuration_id changes
	if plan.CurrentConfigurationId != state.CurrentConfigurationId {
		panic(`
    1. if the new configuration has cancelled, or errored out, Upload configuration files and wait for the stack configuration to converge, cancel, or error out
    2. if the configuration is still converging check for updatability and take one of the following actions:
    - Upload configuration files and wait for the stack configuration to converge, cancel, or error out
    - Or save save the current status from plan as is
    NOTE: we leave the converged status because the source bundle hash is unchanged and we do not need to upload the configuration files again.`,
		)
	}

	// Handle current_configuration_status changes
	if plan.CurrentConfigurationStatus != state.CurrentConfigurationStatus {
		panic(`
	1. if the new plan configuration has canceled, or errored out, Upload configuration files and wait for the stack configuration to converge, cancel, or error out
	2. if the configuration is still converging check for updatability and take one of the following actions:
	- Upload configuration files and wait for the stack configuration to converge, cancel, or error out
	- Or save save the current status from plan as is`,
		)
	}

	/*
	  So the crux of the update operation is always to upload the configuration files and await for the stack configuration to converge, cancel, or error out,
	  or just save the current status from the plan as is.
	*/

}

// Delete is called when the resource is deleted, since the stack migration resource does not support deletion, it logs a warning and adds a warning to the response diagnostics.
func (r *stackMigrationResource) Delete(ctx context.Context, _ resource.DeleteRequest, response *resource.DeleteResponse) {
	tflog.Warn(ctx, DestroyActionNotSupported)
	response.Diagnostics.AddWarning(DestroyActionNotSupported, DestroyActionNotSupportedDetailed)
}

// Configure is called when the resource is configured, it sets up the TFE client and validates the provider configuration.
func (r *stackMigrationResource) Configure(ctx context.Context, configureRequest resource.ConfigureRequest, configureResponse *resource.ConfigureResponse) {
	tflog.Debug(ctx, fmt.Sprintf("Configuring Stack Migration PR Resource with ProviderData: %+v", configureRequest.ProviderData))
	if configureRequest.ProviderData == nil {
		return
	}

	providerConfigData, ok := configureRequest.ProviderData.(ProviderResourceData)
	if !ok {
		configureResponse.Diagnostics.AddError(
			"Unexpected resource Configure type",
			fmt.Sprintf("Expected tfe.ConfiguredClient, got %T.This is a bug in the tfe provider, so please report it on GitHub.", configureRequest.ProviderData),
		)
		return
	}

	r.tfeUtil = providerConfigData.TfeUtil

	if providerConfigData.Hostname != HcpTerraformHost {
		configureResponse.Diagnostics.AddError(
			"Host name must be set to app.terraform.io",
			fmt.Sprintf("The hostname must be %q, but got %q. Please check your provider configuration.", HcpTerraformHost, providerConfigData.Hostname),
		)
		return
	}

	if providerConfigData.TfeToken == "" {
		configureResponse.Diagnostics.AddError(
			"Missing TFE token",
			"Please provide a valid TFE token in the provider configuration.",
		)
		return
	}

	defaultTfeConfig := tfe.DefaultConfig()
	httpClient := defaultTfeConfig.HTTPClient
	transport := httpClient.Transport.(*http.Transport)

	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	transport.TLSClientConfig.InsecureSkipVerify = providerConfigData.SslSkipVerify

	defaultTfeConfig.Address = fmt.Sprintf("%s://%s", constants.TfRemoteScheme, providerConfigData.Hostname)
	defaultTfeConfig.Token = providerConfigData.TfeToken
	defaultTfeConfig.HTTPClient = httpClient

	hcpTerraformClient, err := r.tfeUtil.NewClient(defaultTfeConfig)
	if err != nil {
		configureResponse.Diagnostics.AddError(
			"Failed to create TFE client",
			fmt.Sprintf("Could not create TFE client: %s", err.Error()),
		)
		return
	}

	r.tfeClient = hcpTerraformClient
	tflog.Debug(ctx, fmt.Sprintf("resource configuration complete with TFE client: %+v", r.tfeClient))
}

// Metadata returns the metadata for the stack migration resource.
func (r *stackMigrationResource) Metadata(_ context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + stackMigrationResourceName
}

// applyStackConfiguration uploads the stack configuration files to the stack and waits for the stack configuration to converge, cancel, or error out.
func (r *stackMigrationResource) applyStackConfiguration(ctx context.Context, orgName string, projectName string, stackName string, configDirAbsPath string) (StackMigrationResourceModel, diag.Diagnostics) {
	var currentConfigurationId string
	var currentConfigurationStatus string
	var currentSourceBundleHash string
	var diags diag.Diagnostics
	var state StackMigrationResourceModel

	// Validate preconditions
	diags.Append(r.createActionPreconditions(orgName, projectName, stackName)...)
	if diags.HasError() {
		tflog.Error(ctx, "Preconditions for resource creation failed")
		return state, diags
	}

	// Check if a new source bundle config is allowed to be uploaded
	uploadNewConfig, err := r.allowSourceBundleUpload(ctx, r.existingStack.LatestStackConfiguration)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Failed to check if source bundle upload is allowed for stack %s: %s", r.existingStack.Name, err.Error()))
		diags.AddError(
			"Error Checking Source Bundle Upload",
			fmt.Sprintf("Failed to check if source bundle upload is allowed for stack %s: %s", r.existingStack.Name, err.Error()),
		)
		return state, diags
	}

	// Attempt to upload a source bundle if allowed else sync existing stack configuration data
	if uploadNewConfig {
		tflog.Info(ctx, fmt.Sprintf("Uploading source bundle for stack %s from directory %s", r.existingStack.Name, configDirAbsPath))
		currentConfigurationId, currentSourceBundleHash, diags = r.uploadSourceBundle(ctx, r.existingStack.ID, configDirAbsPath, true)
	} else {
		tflog.Info(ctx, fmt.Sprintf("Syncing existing stack configuration data for stack %s from directory %s", r.existingStack.Name, configDirAbsPath))
		currentConfigurationId, currentSourceBundleHash, diags = r.syncExistingStackConfigurationData(ctx, r.existingStack, configDirAbsPath)
	}

	diags.Append(diags...)
	if diags.HasError() {
		tflog.Error(ctx, fmt.Sprintf("Failed to get currentConfigurationId and sourceBundleHash for stack %s", r.existingStack.Name))
		return state, diags
	}

	// Attempt to poll for the current configuration status and await for it to converge, cancel, or error out.
	tflog.Info(ctx, fmt.Sprintf("Starting to poll for current configuration ID: %s", currentConfigurationId))
	currentConfigurationStatus = r.watchStackConfigurationUntilTerminalStatus(ctx, currentConfigurationId).String()
	tflog.Info(ctx, fmt.Sprintf("Received status: %s for configuration ID: %s after polling ", currentConfigurationStatus, currentConfigurationId))

	// handle converging status
	if tfe.StackConfigurationStatus(currentConfigurationStatus) == tfe.StackConfigurationStatusConverging {
		tflog.Info(ctx, fmt.Sprintf("The stack configuration %s is converging. awaiting to be in a non-running plan plan", currentConfigurationId))
		configStatusFromConvergingHandler := r.tfeUtil.HandleConvergingStatus(currentConfigurationId, r.tfeClient)

		if configStatusFromConvergingHandler != "" {
			currentConfigurationStatus = configStatusFromConvergingHandler
		}

		// if the status is still converging after handling, we log a warning and continue
		if tfe.StackConfigurationStatus(currentConfigurationStatus) == tfe.StackConfigurationStatusConverging {
			tflog.Warn(ctx, fmt.Sprintf("Awaited for converging configuration with %s to complete, but no update", currentConfigurationId))
		}
	}

	// Ensure the currentConfigurationId is not empty before proceeding
	if currentConfigurationId == "" {
		diags.AddError(
			"Post create validation failure: Missing Configuration ID",
			"After uploading or syncing the stack configuration, the resulting configuration ID was empty. This indicates an internal logic or API error. Aborting resource creation.",
		)
		return state, diags
	}

	// Ensure the currentConfigurationStatus is not empty before proceeding
	if currentConfigurationStatus == "" {
		diags.AddError(
			"Post create validation failure: Missing Configuration Status",
			"After uploading or syncing the stack configuration, the resulting configuration status was empty. This indicates an internal logic or API error. Aborting resource creation.",
		)
		return state, diags
	}

	state.ConfigurationDir = types.StringValue(configDirAbsPath)
	state.CurrentConfigurationId = types.StringValue(currentConfigurationId)
	state.CurrentConfigurationStatus = types.StringValue(currentConfigurationStatus)
	state.Name = types.StringValue(r.existingStack.Name)
	state.Organization = types.StringValue(r.existingOrganization.Name)
	state.Project = types.StringValue(r.existingProject.Name)
	state.SourceBundleHash = types.StringValue(currentSourceBundleHash)

	// Prepare and log a final status message
	finalLogMsgMetadata := map[string]interface{}{
		organizationNameMetadata:         r.existingOrganization.Name,
		projectNameMetadata:              r.existingProject.Name,
		stackConfigurationIdMetadata:     currentConfigurationId,
		stackConfigurationStatusMetadata: currentConfigurationStatus,
		stackNameMetadata:                r.existingStack.Name,
	}
	r.logFinalConfigStatusMsg(ctx, tfe.StackConfigurationStatus(currentConfigurationStatus), finalLogMsgMetadata)

	return state, diags
}

// allowSourceBundleUpload checks if the stack configuration is in a state that allows uploading a new source bundle.
func (r *stackMigrationResource) allowSourceBundleUpload(ctx context.Context, configuration *tfe.StackConfiguration) (bool, error) {
	if configuration == nil || len(configuration.DeploymentNames) == 0 {
		tflog.Info(ctx, "Current stack configuration is either nil, has no deployments. allowing source bundle upload.")
		return true, nil
	}

	stackConfigurationStatus := tfe.StackConfigurationStatus(configuration.Status)

	// handle converging status
	if stackConfigurationStatus == tfe.StackConfigurationStatusConverging {
		return r.handleConvergingStatusForAllowUpload(ctx, configuration)
	}

	switch stackConfigurationStatus {
	case tfe.StackConfigurationStatusConverged, tfe.StackConfigurationStatusCanceled, tfe.StackConfigurationStatusErrored:
		tflog.Info(ctx, fmt.Sprintf("Current stack configuration %s is in a terminal state (%s). Allowing source bundle upload.", configuration.ID, configuration.Status))
		return true, nil
	default:
		return false, nil
	}
}

// createActionPreconditions checks if the resource passes the preconditions for the create action.
func (r *stackMigrationResource) createActionPreconditions(orgName string, projectName string, stackName string) diag.Diagnostics {
	var diags diag.Diagnostics
	var err error

	// check if the organization exists
	if r.existingOrganization, err = r.tfeUtil.ReadOrgByName(orgName, r.tfeClient); err != nil {
		diags.AddError(
			"Error Reading organization",
			fmt.Sprintf("The organization %q does not exist or could not be accessed: %s", orgName, err.Error()),
		)
		return diags
	}

	// check if the project exists
	r.existingProject, err = r.tfeUtil.ReadProjectByName(r.existingOrganization.Name, projectName, r.tfeClient)
	if err != nil {
		diags.AddError(
			"Error Reading project",
			fmt.Sprintf("The project %q does not exist or could not be accessed in organization %q: %s", projectName, r.existingOrganization.Name, err.Error()),
		)
		return diags
	}

	// check if the stack already exists
	r.existingStack, err = r.tfeUtil.ReadStackByName(r.existingOrganization.Name, r.existingProject.ID, stackName, r.tfeClient)
	if err != nil {
		diags.AddError(
			"Error Reading stack",
			fmt.Sprintf("The stack %q does not exist or could not be accessed in organization %q and project %q: %s", stackName, r.existingOrganization.Name, r.existingProject.Name, err.Error()),
		)
		return diags
	}

	// check if the stack is a VCS-driven stack
	if r.existingStack.VCSRepo != nil {
		diags.AddError(
			"Migration to VCS backed stacks is not supported",
			fmt.Sprintf("The stack %q in organization %q and project %q is a VCS backed stacks. The `tfmigrate_stack_migration` resource supports migration to non-VCS backed stacks only ", r.existingStack.Name, r.existingOrganization.Name, r.existingProject.Name),
		)
		return diags
	}

	return nil
}

// logFinalConfigStatusMsg logs a final message with based on the last known stack configuration status.
func (r *stackMigrationResource) logFinalConfigStatusMsg(ctx context.Context, status tfe.StackConfigurationStatus, metadata map[string]interface{}) {
	var statusMsg string
	stackName := metadata[stackNameMetadata].(string)
	configId := metadata[stackConfigurationIdMetadata].(string)
	switch status {
	case tfe.StackConfigurationStatusErrored:
		statusMsg = fmt.Sprintf(configErrored, configId, stackName)
		tflog.Error(ctx, statusMsg, metadata)
	case tfe.StackConfigurationStatusConverging:
		statusMsg = fmt.Sprintf(configConverging, configId, stackName)
		tflog.Info(ctx, statusMsg, metadata)
	case tfe.StackConfigurationStatusConverged:
		statusMsg = fmt.Sprintf(configConverged, configId, stackName)
		tflog.Info(ctx, statusMsg, metadata)
	case tfe.StackConfigurationStatusCanceled:
		statusMsg = fmt.Sprintf(configCanceled, configId, stackName)
		tflog.Warn(ctx, statusMsg, metadata)
	default:
		statusMsg = fmt.Sprintf(configTransitioning, configId, stackName, status)
		tflog.Info(ctx, statusMsg, metadata)
	}

}

// syncExistingStackConfigurationData reads the current stack configuration ID and status and calculates the source bundle hash of the configuration files in the directory.
func (r *stackMigrationResource) syncExistingStackConfigurationData(ctx context.Context, stack *tfe.Stack, configDir string) (string, string, diag.Diagnostics) {
	var diags diag.Diagnostics

	if stack == nil || stack.LatestStackConfiguration == nil || stack.LatestStackConfiguration.ID == "" {
		diags.AddError(
			"Stack Configuration cannot be nil",
			"The latest stack configuration is nil. This is likely due to an error in reading the stack by name or ID. Please ensure the stack has a configuration before proceeding.",
		)
		return "", "", diags
	}

	if configDir == "" {
		diags.AddError(
			"Configuration Directory cannot be empty when syncing existing stack configuration",
			"The configuration directory is empty. Please provide a valid absolute path to the directory containing the stack configuration files.",
		)
		return "", "", diags
	}

	// Recalculate the source bundle hash of the configuration files in the directory.
	currentSourceBundleHash, err := r.tfeUtil.CalculateStackSourceBundleHash(configDir)
	if err != nil {
		diags.AddError(
			configHasErrSummary,
			fmt.Sprintf(configHashErrDetails, configDir, err.Error()),
		)
		return "", "", diags
	}
	tflog.Debug(ctx, fmt.Sprintf("Recalculated source bundle hash: %s for directory: %s", currentSourceBundleHash, configDir))

	return stack.LatestStackConfiguration.ID, currentSourceBundleHash, diags
}

// uploadSourceBundle uploads the stack configuration files from the specified directory to the stack and returns the current configuration ID and source bundle hash.
func (r *stackMigrationResource) uploadSourceBundle(ctx context.Context, stackId string, configDir string, calculateSourceBundleHash bool) (string, string, diag.Diagnostics) {
	var currentConfigurationId string
	var diags diag.Diagnostics
	var err error
	var sourceBundleHash string

	// Calculate the source bundle hash
	if calculateSourceBundleHash {
		sourceBundleHash, err = r.tfeUtil.CalculateStackSourceBundleHash(configDir)
		if err != nil {
			diags.AddError(
				configHasErrSummary,
				fmt.Sprintf(configHashErrDetails, configDir, err.Error()),
			)
			return "", "", diags
		}
		tflog.Debug(ctx, fmt.Sprintf("Calculated source bundle hash: %s for directory: %s", sourceBundleHash, configDir))
	}

	// Upload the stack configuration files
	currentConfigurationId, err = r.tfeUtil.UploadStackConfigFile(stackId, configDir, r.tfeClient)
	if err != nil {
		diags.AddError(
			"Error Uploading Stack Configuration",
			fmt.Sprintf("Failed to upload the stack configuration files from directory %q, err: %s", configDir, err.Error()),
		)
		return "", "", diags
	}
	tflog.Debug(ctx, fmt.Sprintf("Uploaded stack configuration files to stack %s with ID: %s", stackId, currentConfigurationId))

	return currentConfigurationId, sourceBundleHash, nil
}

// watchStackConfigurationUntilTerminalStatus waits for the stack configuration to reach a terminal status (converged, canceled, or errored) and returns the final status.
func (r *stackMigrationResource) watchStackConfigurationUntilTerminalStatus(ctx context.Context, configId string) tfe.StackConfigurationStatus {
	/* NOTE:
	   awaitConfigCompletion errors and warnings are logged but not propagated to prevent
	   resource operation failures after successful configuration upload. The function
	   returns the last known stack configuration status in all scenarios:
	     1. "converged" - configuration rolled out successfully
	     2. "canceled" - upload succeeded but rollout was canceled
	     3. "converging" - configuration is still being processed, approved or discarded in the UI
	     4. "errored" - configuration upload failed, but the resource operation continues
	   This design ensures resource creation/update operations continue based on the
	   last received status rather than failing due to completion monitoring issues.
	   Timeout configured at 5 minutes
	*/
	status, diags := r.tfeUtil.WatchStackConfigurationUntilTerminalStatus(configId, r.tfeClient)
	if diags.WarningsCount() > 0 {
		var warnMsg string
		for _, warnDiags := range diags.Warnings() {
			warnMsg += fmt.Sprintf("%s\n", warnDiags.Summary())
		}
		tflog.Warn(ctx, fmt.Sprintf("Warnings while awaiting stack configuration %s completion, warnings: %s", configId, warnMsg))
	}

	if diags.ErrorsCount() > 0 {
		var errMsg string
		for _, errDiags := range diags.Errors() {
			errMsg += fmt.Sprintf("%s\n", errDiags.Summary())
		}
		tflog.Error(ctx, fmt.Sprintf("Error while awaiting stack configuration %s completion, err: %s", configId, errMsg))
	}

	if status == "" {
		// This is highly unexpected, as we should always receive a status after waiting for completion.
		// However, if we do not receive a status, we log an error and default to pending optimistically.
		tflog.Error(ctx, fmt.Sprintf("No status received for stack configuration %s after waiting for completion. This is unexpected behavior.", configId))
		status = tfe.StackConfigurationStatusPending // Default to pending if no status is received
	}

	return status
}

// handleConvergingStatusForAllowUpload checks if a converging stack configuration has any running plans, return true if no running plans are found false otherwise.
func (r *stackMigrationResource) handleConvergingStatusForAllowUpload(ctx context.Context, configuration *tfe.StackConfiguration) (bool, error) {
	// check if there are any applying plans if so return false
	tflog.Info(ctx, fmt.Sprintf("Current stack configuration %s is converging. Checking if there are any applying plans.", configuration.ID))
	hasRunningPlans, err := r.tfeUtil.StackConfigurationHasRunningPlan(configuration.ID, r.tfeClient)
	if err != nil {
		return false, err
	}

	if hasRunningPlans {
		tflog.Info(ctx, fmt.Sprintf("Current stack configuration %s has running plans. Not allowing source bundle upload.", configuration.ID))
		return false, nil
	}

	return true, nil
}
