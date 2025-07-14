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
	stackConstants "terraform-provider-tfmigrate/internal/constants/stack"
	tfeUtil "terraform-provider-tfmigrate/internal/util/tfe"

	"github.com/hashicorp/go-tfe"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Define constants for the stack resource.
const (
	stackMigrationResourceName = `_stack_migration` // stackMigrationResourceName is the name of the resource for migrating existing HCP Terraform workspaces to deployments within a non-VCS stack.

	// stackMigrationResourceDescription is the description of the resource for migrating existing HCP Terraform workspaces to deployments within a non-VCS stack.
	stackMigrationResourceDescription = `Defines a resource for migrating existing HCP Terraform workspaces to deployments within a non-VCS stack. Each workspace maps one-to-one with a stack deployment. The resource uploads configuration files to the stack and monitors the upload and deployment status.`

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

	// currentConfigurationIdDescription is the Markdown description for the `current_configuration_id` attribute.
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

	// - tfe.StackConfigurationStatusErrored.
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

	// configTerminalStateMsg is the log message constant that indicates that the update strategy is to upload the configuration files and wait for the stack configuration to converge, cancel, or error out.
	configTerminalStateMsg = "Configuration status changed to %s, which is a terminal state. Uploading configuration files and waiting for the stack configuration to converge, cancel, or error out."

	// errDiagSummarySourceBundleUploadChk is the diagnostic error summary when checking if the source bundle upload is allowed.
	errDiagSummarySourceBundleUploadChk = "Error Checking Source Bundle Upload"

	// errDiagDetailsSourceBundleUploadChk is the diagnostic error details when checking if the source bundle upload is allowed.
	errDiagDetailsSourceBundleUploadChk = "Failed to check if source bundle upload is allowed for stack %s: %s"

	// errFailedToGetStateValues is the error message when the state values cannot be retrieved.
	errFailedToGetStateValues = "Failed to get state values"
)

var (
	_ resource.Resource               = &stackMigrationResource{}
	_ resource.ResourceWithConfigure  = &stackMigrationResource{}
	_ resource.ResourceWithModifyPlan = &stackMigrationResource{}

	projectAndOrgNameRegex = regexp.MustCompile(`^[a-zA-Z0-9 _-]{3,40}$`) // projectAndOrgNameRegex Regex for valid organization, and project names
	stackNameRegex         = regexp.MustCompile(`^[a-zA-Z0-9 _-]{1,90}$`) // projectAndOrgNameRegex Regex for valid stack names

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
					stringvalidator.RegexMatches(stackNameRegex, stackNameConstraintViolationMsg),
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
					stringvalidator.RegexMatches(projectAndOrgNameRegex, organizationNameConstraintViolationMsg),
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
					stringvalidator.RegexMatches(projectAndOrgNameRegex, projectNameConstraintViolationMsg),
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

// Read is called when the resource is read, it retrieves the current state of the stack migration resource and updates the state with the latest values.
func (r *stackMigrationResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
	var state StackMigrationResourceModel
	var err error
	r.tfeUtil.UpdateContext(ctx)

	// read the current state
	response.Diagnostics.Append(request.State.Get(ctx, &state)...)
	if response.Diagnostics.HasError() {
		tflog.Error(ctx, errFailedToGetStateValues)
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

	tflog.Info(ctx, "Successfully saved the state for stack migration resource")

}

// Update is called when the resource is updated, it applies the stack configuration files to an existing stack migration resource and updates the state with the new values.
func (r *stackMigrationResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
	var plan, state StackMigrationResourceModel
	var diags diag.Diagnostics
	r.tfeUtil.UpdateContext(ctx)

	// Retrieve values from the plan
	response.Diagnostics.Append(request.Plan.Get(ctx, &plan)...)
	if response.Diagnostics.HasError() {
		tflog.Error(ctx, "Failed to get plan values")
		return
	}

	// Retrieve values from the state
	response.Diagnostics.Append(request.State.Get(ctx, &state)...)
	if response.Diagnostics.HasError() {
		tflog.Error(ctx, errFailedToGetStateValues)
		return
	}

	if plan.SourceBundleHash.IsUnknown() &&
		plan.CurrentConfigurationId.IsUnknown() &&
		plan.CurrentConfigurationStatus.IsUnknown() {
		var newState StackMigrationResourceModel
		// update the state of the existing stack migration resource by uploading the configuration files
		tflog.Info(ctx, "Starting to apply stack migration configuration to an existing stack migration resource")
		newState, diags = r.applyStackConfiguration(ctx, plan.Organization.ValueString(), plan.Project.ValueString(), plan.Name.ValueString(), plan.ConfigurationDir.ValueString())
		response.Diagnostics.Append(diags...)
		if response.Diagnostics.HasError() {
			return
		}
		// update plan with the new state
		plan.ConfigurationDir = newState.ConfigurationDir
		plan.CurrentConfigurationId = newState.CurrentConfigurationId
		plan.CurrentConfigurationStatus = newState.CurrentConfigurationStatus
		plan.SourceBundleHash = newState.SourceBundleHash
		tflog.Info(ctx, "Successfully applied stack migration configuration to an existing stack migration resource")
	}

	response.Diagnostics.Append(response.State.Set(ctx, plan)...)
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

// ModifyPlan is called to modify the plan before it is applied. It checks the current state and modifies the plan based on the existing state and the update strategy.
func (r *stackMigrationResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	// If this is a destroy operation, no modifications needed
	if req.Plan.Raw.IsNull() {
		return
	}

	// If this is a create operation, the state will be empty
	if req.State.Raw.IsNull() {
		tflog.Debug(ctx, "No existing state found, skipping plan modifications for create operation")
		return
	}

	// If this is an update operation, we need to ensure the plan matches the state

	var plan, state StackMigrationResourceModel
	r.tfeUtil.UpdateContext(ctx)

	// Retrieve values from the plan
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Error(ctx, "Failed to get plan values")
		return
	}

	// Retrieve values from the state
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		tflog.Error(ctx, errFailedToGetStateValues)
		return
	}

	tflog.Debug(ctx, "Modifying plan for stack migration resource")

	// check stack preconditions before proceeding with the update strategy
	resp.Diagnostics.Append(r.createActionPreconditions(plan.Organization.ValueString(), plan.Project.ValueString(), plan.Name.ValueString())...)
	if resp.Diagnostics.HasError() {
		tflog.Error(ctx, fmt.Sprintf("Preconditions for resource update failed: %s", plan.Name.ValueString()))
		return
	}

	// Calculate the hash of the configuration files in the directory
	currentSourceBundleHash, err := r.tfeUtil.CalculateStackSourceBundleHash(plan.ConfigurationDir.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			configHasErrSummary,
			fmt.Sprintf(configHashErrDetails, plan.ConfigurationDir.ValueString(), err.Error()),
		)
		tflog.Error(ctx, fmt.Sprintf("Failed to calculate the hash of the configuration files in the directory %s: %s", plan.ConfigurationDir.ValueString(), err.Error()))
		return
	}

	configFileChanged := false
	sourceBundleUploadAllowed := false
	resourceUpdateStrategy := stackConstants.UnknownStackPlanUpdateStrategy
	var diags diag.Diagnostics

	// determine if the configuration files have changed
	configFileChanged = state.SourceBundleHash.ValueString() != currentSourceBundleHash

	// determine if the source bundle upload is allowed
	if sourceBundleUploadAllowed, err = r.allowSourceBundleUpload(ctx, r.existingStack.LatestStackConfiguration); err != nil {
		tflog.Error(ctx, fmt.Sprintf(errDiagDetailsSourceBundleUploadChk, r.existingStack.Name, err.Error()))
		resp.Diagnostics.AddError(errDiagSummarySourceBundleUploadChk,
			fmt.Sprintf(errDiagDetailsSourceBundleUploadChk, r.existingStack.Name, err.Error()))
		return
	}

	// Determine the update strategy based on the plan and state
	resourceUpdateStrategy, diags = r.determineStackMigrationUpdateStrategy(ctx, &plan, &state, configFileChanged, sourceBundleUploadAllowed)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() || resourceUpdateStrategy == stackConstants.UnknownStackPlanUpdateStrategy {
		tflog.Error(ctx, fmt.Sprintf("Failed to determine update strategy for stack migration resource: %s", plan.Name.ValueString()))
		return
	}

	tflog.Debug(ctx, fmt.Sprintf("Determined update strategy: %s for stack migration resource: %s", resourceUpdateStrategy.String(), plan.Name.ValueString()))

	if modifyPlanForStrategy(ctx, resourceUpdateStrategy, &plan) {
		resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
		return
	}
}

// Metadata returns the metadata for the stack migration resource.
func (r *stackMigrationResource) Metadata(_ context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + stackMigrationResourceName
}
