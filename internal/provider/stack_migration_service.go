package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework/attr"

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/hashicorp/go-tfe"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	noStateConversionOpStatuses = []tfe.StackConfigurationStatus{
		tfe.StackConfigurationStatusPending,
		tfe.StackConfigurationStatusQueued,
		tfe.StackConfigurationStatusPreparing,
		tfe.StackConfigurationStatusEnqueueing,
	}
)

// allowSourceBundleUpload checks if the stack configuration is in a state that allows uploading a new source bundle.
func (r *stackMigrationResource) allowSourceBundleUpload(ctx context.Context, configuration *tfe.StackConfiguration) (bool, error) {
	if configuration == nil {
		tflog.Info(ctx, "Current stack configuration is either nil, has no deployments. allowing source bundle upload.")
		return true, nil
	}

	stackConfigurationStatus := tfe.StackConfigurationStatus(configuration.Status)

	// handle completed status
	if stackConfigurationStatus == tfe.StackConfigurationStatusCompleted {
		return r.handleCompletedStatusForAllowUpload(ctx, configuration)
	}

	switch stackConfigurationStatus {
	case tfe.StackConfigurationStatusCanceled, tfe.StackConfigurationStatusErrored:
		tflog.Info(ctx, fmt.Sprintf("Current stack configuration %s is in a terminal state (%s). Allowing source bundle upload.", configuration.ID, configuration.Status))
		return true, nil
	default:
		return false, nil
	}
}

// applyStackConfiguration uploads the stack configuration files to the stack and waits for the stack configuration to converge, cancel, or error out.
func (r *stackMigrationResource) applyStackConfiguration(ctx context.Context, orgName string, projectName string, stackName string, configDirAbsPath string, migrationMap map[string]string) (bool, StackMigrationResourceModel, diag.Diagnostics) {
	var currentConfigurationId string
	var currentConfigurationStatus string
	var currentSourceBundleHash string
	var diags diag.Diagnostics
	var state StackMigrationResourceModel

	deploymentNamesFromMigrationMap := mapset.NewSet[string]()
	for _, deploymentName := range migrationMap {
		if deploymentNamesFromMigrationMap.Contains(deploymentName) {
			tflog.Error(ctx, fmt.Sprintf("Duplicate deployment name found in migration map: %s", deploymentName))
			diags.AddError(
				"Duplicate Deployment Name",
				fmt.Sprintf("The deployment name %q is duplicated in the migration map. Each deployment name must be unique.", deploymentName),
			)
			return false, state, diags
		}
		deploymentNamesFromMigrationMap.Add(deploymentName)
	}

	// Validate preconditions
	diags.Append(r.createActionPreconditions(orgName, projectName, stackName, configDirAbsPath, deploymentNamesFromMigrationMap)...)
	if diags.HasError() {
		tflog.Error(ctx, "Preconditions for resource creation failed")
		return false, state, diags
	}

	// Calculate the hash of the Terraform configuration files in the directory
	terraformConfigHash, err := r.tfeUtil.CalculateConfigFileHash(configDirAbsPath)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf(configHashErrDetails, configDirAbsPath, err.Error()))
		diags.AddError(
			configHasErrSummary,
			fmt.Sprintf(configHashErrDetails, configDirAbsPath, err.Error()),
		)
		return false, state, diags
	}

	// Check if a new source bundle config is allowed to be uploaded
	uploadNewConfig, err := r.allowSourceBundleUpload(ctx, r.existingStack.LatestStackConfiguration)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf(errDiagDetailsSourceBundleUploadChk, r.existingStack.Name, err.Error()))
		diags.AddError(
			errDiagDetailsSourceBundleUploadChk,
			fmt.Sprintf(errDiagDetailsSourceBundleUploadChk, r.existingStack.Name, err.Error()),
		)
		return false, state, diags
	}

	// Attempt to upload a source bundle if allowed else sync existing stack configuration data
	if uploadNewConfig {
		tflog.Info(ctx, fmt.Sprintf("Uploading source bundle for stack %s from directory %s", r.existingStack.Name, configDirAbsPath))
		currentConfigurationId, currentSourceBundleHash, diags = r.uploadSourceBundle(ctx, r.existingStack.ID, configDirAbsPath, true)
	} else {
		//Fixme: Revisit this
		tflog.Info(ctx, fmt.Sprintf("Syncing existing stack configuration data for stack %s from directory %s", r.existingStack.Name, configDirAbsPath))
		currentConfigurationId, currentSourceBundleHash, diags = r.syncExistingStackConfigurationData(ctx, r.existingStack, configDirAbsPath)
	}

	diags.Append(diags...)
	if diags.HasError() {
		tflog.Error(ctx, fmt.Sprintf("Failed to get currentConfigurationId and sourceBundleHash for stack %s", r.existingStack.Name))
		return false, state, diags
	}

	// Attempt to poll for the current configuration status and await for it to complete, cancel, or error out.
	tflog.Info(ctx, fmt.Sprintf("Starting to poll for current configuration ID: %s", currentConfigurationId))
	currentConfigurationStatus = r.watchStackConfigurationUntilTerminalStatus(ctx, currentConfigurationId).String()
	tflog.Info(ctx, fmt.Sprintf("Received status: %s for configuration ID: %s after polling ", currentConfigurationStatus, currentConfigurationId))

	// Ensure the currentConfigurationId is not empty before proceeding
	if currentConfigurationId == "" {
		diags.AddError(
			"Post create validation failure: Missing Configuration ID",
			"After uploading or syncing the stack configuration, the resulting configuration ID was empty. This indicates an internal logic or API error. Aborting resource creation.",
		)
		return false, state, diags
	}

	// Ensure the currentConfigurationStatus is not empty before proceeding
	if currentConfigurationStatus == "" {
		diags.AddError(
			"Post create validation failure: Missing Configuration Status",
			"After uploading or syncing the stack configuration, the resulting configuration status was empty. This indicates an internal logic or API error. Aborting resource creation.",
		)
		return false, state, diags
	}

	// if the configuration has not errored or not been canceled, we save the current configuration ID and status in the state.
	configurationStatus := tfe.StackConfigurationStatus(currentConfigurationStatus)

	if configurationStatus == tfe.StackConfigurationStatusCanceled {
		diags.AddWarning(
			"Stack Configuration Canceled",
			fmt.Sprintf("The current stack configuration %s has been canceled. No state would be uploaded", currentConfigurationId),
		)
		return false, state, diags
	}

	if configurationStatus == tfe.StackConfigurationStatusErrored {
		diags.Append(r.tfeUtil.ReadStackDiagnosticsByConfigID(currentConfigurationId, r.httpClient, r.tfeConfig)...)
		return false, state, diags
	}

	state.CurrentConfigurationId = types.StringValue(currentConfigurationId)
	state.CurrentConfigurationStatus = types.StringValue(currentConfigurationStatus)
	state.Name = types.StringValue(r.existingStack.Name)
	state.Organization = types.StringValue(r.existingOrganization.Name)
	state.Project = types.StringValue(r.existingProject.Name)
	state.SourceBundleHash = types.StringValue(currentSourceBundleHash)
	state.TerraformConfigHash = types.StringValue(terraformConfigHash)
	state.ConfigurationDir = types.StringValue(r.stackSourceBundleAbsPath)
	state.TerraformConfigDir = types.StringValue(r.terraformConfigDirAbsPath)

	vals := map[string]attr.Value{}
	for k, v := range migrationMap {
		vals[k] = types.StringValue(v)
	}

	state.WorkspaceDeploymentMapping = types.MapValueMust(types.StringType, vals)

	// handle the `tfe.StackConfigurationStatuses` and determine the next steps based on the current configuration status.
	diags = r.continueWithStateUploadPostConfigUpload(currentConfigurationId, configurationStatus)
	if diags.HasError() || diags.WarningsCount() > 0 {
		tflog.Error(ctx, fmt.Sprintf("Post configuration upload diagnostics for stack %s contain errors or warnings errCount: %d, warnCount %d", r.existingStack.Name, diags.ErrorsCount(), diags.WarningsCount()))
		return true, state, diags
	}

	migrationData := r.uploadStackDeploymentsState(ctx, migrationMap)

	hash, err := r.migrationHashService.GetMigrationHash(migrationData)
	if err != nil {
		diags.AddError(
			"Error calculating migration hash",
			fmt.Sprintf("Failed to calculate migration hash: %s", err.Error()),
		)
		return true, state, diags
	}

	state.MigrationHash = types.StringValue(hash)

	data, _ := r.migrationHashService.GetMigrationData(hash)
	diags.AddWarning(
		"Migration Data Retrieved",
		prettyPrintJSON(data),
	)

	return true, state, diags
}

// createActionPreconditions checks if the resource passes the preconditions for the create action. //TODO start with map set.
func (r *stackMigrationResource) createActionPreconditions(orgName string, projectName string, stackName string, stackConfigDir string, deploymentNamesFromMigrationMap mapset.Set[string]) diag.Diagnostics {
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

	// check if the deployment names from the migration map are valid
	deploymentNamesFromStackConfigDir, err := r.getDeploymentNamesFromStackConfigDir(stackConfigDir)
	if err != nil {
		diags.AddError(
			"Error Reading deployment names from stack configuration directory",
			fmt.Sprintf("The stack configuration directory %q does not contain valid deployment names: %s", stackConfigDir, err.Error()),
		)
		return diags
	}

	if deploymentNamesFromMigrationMap.SymmetricDifference(deploymentNamesFromStackConfigDir).Cardinality() > 0 {
		diags.AddError(
			"Deployment names mismatch",
			fmt.Sprintf("The deployment names from the migration map %v do not match the deployment names in the stack configuration directory %q: %v", deploymentNamesFromMigrationMap.ToSlice(), stackConfigDir, deploymentNamesFromStackConfigDir.ToSlice()),
		)
		return diags
	}

	return nil
}

// handleConvergingStatusForAllowUpload checks if a converging stack configuration has any running plans, return true if no running plans are found false otherwise.
func (r *stackMigrationResource) handleCompletedStatusForAllowUpload(ctx context.Context, configuration *tfe.StackConfiguration) (bool, error) {
	// check if there are any applying plans if so return false
	tflog.Info(ctx, fmt.Sprintf("Current stack configuration %s is %s. Checking if there are any applying plans.", configuration.ID, configuration.Status))
	hasRunningDeploymentGroups, err := r.tfeUtil.StackConfigurationHasRunningDeploymentGroups(configuration.ID, r.tfeClient)
	if err != nil {
		return false, err
	}

	if hasRunningDeploymentGroups {
		tflog.Info(ctx, fmt.Sprintf("Current stack configuration %s has running groups. Not allowing source bundle upload.", configuration.ID))
		return false, nil
	}

	return true, nil
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
	currentSourceBundleHash, err := r.tfeUtil.CalculateConfigFileHash(configDir)
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
		sourceBundleHash, err = r.tfeUtil.CalculateConfigFileHash(configDir)
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

// handleConfigurationStatusPostUpload handles the post-upload status of the stack configuration.
func (r *stackMigrationResource) continueWithStateUploadPostConfigUpload(currentConfigurationId string, currentConfigurationStatus tfe.StackConfigurationStatus) diag.Diagnostics {
	var diags diag.Diagnostics

	// if currentConfigurationStatus == tfe.StackConfigurationStatusConverging || currentConfigurationStatus == tfe.StackConfigurationStatusConverged {
	//	diags.AddError(
	//		"Converged Stack Configuration Is Not Supported",
	//		fmt.Sprintf("The current stack configuration %s has converged. The `tfmigrate_stack_migration` resource does not support state upload for converged stack configurations.", currentConfigurationId))
	//}

	if slices.Contains(noStateConversionOpStatuses, currentConfigurationStatus) {
		diags.AddWarning(
			"Stack Configuration Status Not Ready for State Upload",
			fmt.Sprintf("The currentstack configuration %s is in a status (%s) that does not allow state upload. Please wait for the stack configuration to be in completed state before running again", currentConfigurationId, currentConfigurationStatus),
		)
		return diags
	}

	return diags
}

func prettyPrintJSON(v interface{}) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("error marshaling JSON: %v", err)
	}
	return string(b)
}
