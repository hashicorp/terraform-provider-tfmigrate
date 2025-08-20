package provider

import (
	"context"
	"fmt"
	"os"
	"strconv"
	stackConstants "terraform-provider-tfmigrate/internal/constants/stack"
	"time"

	"github.com/hashicorp/go-tfe"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	convergedConfigUploadCooldownPeriod = 5 * time.Minute // convergedConfigUploadCooldownPeriod is the cool-down time between two consecutive uploads of the same stack configuration files after the stack configuration has converged.
)

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
		tflog.Error(ctx, fmt.Sprintf(errDiagDetailsSourceBundleUploadChk, r.existingStack.Name, err.Error()))
		diags.AddError(
			errDiagSummarySourceBundleUploadChk,
			fmt.Sprintf(errDiagDetailsSourceBundleUploadChk, r.existingStack.Name, err.Error()),
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

// determineStackMigrationUpdateStrategy determines an update strategy based on the differences between the plan and state during the update operation for the stack migration resource.
func (r *stackMigrationResource) determineStackMigrationUpdateStrategy(ctx context.Context, plan *StackMigrationResourceModel, state *StackMigrationResourceModel, configFileChanged bool, sourceBundleUploadAllowed bool) (stackConstants.StackPlanUpdateStrategy, diag.Diagnostics) {

	/*
	  If the configuration directory changed, we need to check if the source bundle hash is different.
	  If the current source bundle hash is different from the one in the state, it means that the configuration files
	  have changed, and we need to upload the new configuration files if we are allowed to do so.
	  If not allowed, then we should throw an error stating that the configuration update is not allowed until
	  the current configuration converges, cancels, or errors out.
	*/
	if plan.ConfigurationDir != state.ConfigurationDir {
		if configFileChanged {
			return r.handleUpdateActionConfigFileChange(ctx, sourceBundleUploadAllowed)
		}
		tflog.Debug(ctx, "Configuration directory changed but source bundle hash is unchanged, no upload needed.")
		return stackConstants.RetainPlanStackPlanUpdateStrategy, nil
	}

	/*
	  If the configuration directory is unchanged, we need to check if the source bundle hash is different.
	  If the source bundle hash is different, it means that the configuration files have changed,
	  and we need to upload the new configuration files if we are allowed to do so.
	  If not allowed, then we should throw an error stating that the configuration update is not allowed until
	  the current configuration converges, cancels, or errors out.
	*/
	if configFileChanged {
		tflog.Debug(ctx, "Configuration directory is unchanged but source bundle hash is different, checking if upload is allowed.")
		return r.handleUpdateActionConfigFileChange(ctx, sourceBundleUploadAllowed)
	}

	/*
	  if the configurationId is different in the plan and state,
	  that means the latest configuration in the plan needs to be saved
	  as the current configuration in the state.
	*/
	if plan.CurrentConfigurationId != state.CurrentConfigurationId {
		tflog.Debug(ctx, fmt.Sprintf("Current configuration ID changed from %s to %s in the plan. Saving the new configuration ID in the state.", state.CurrentConfigurationId.ValueString(), plan.CurrentConfigurationId.ValueString()))
		return stackConstants.RetainPlanStackPlanUpdateStrategy, nil
	}

	return r.handleUpdateActionConfigStatusChange(ctx, tfe.StackConfigurationStatus(plan.CurrentConfigurationStatus.ValueString()),
		tfe.StackConfigurationStatus(state.CurrentConfigurationStatus.ValueString()))
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

// handleUpdateActionConfigStatusChange handles the update action based on the configuration status change.
func (r *stackMigrationResource) handleReUploadOfSameConfigOnConverged(ctx context.Context) (stackConstants.StackPlanUpdateStrategy, diag.Diagnostics) {
	var diags diag.Diagnostics
	var diagSummary string
	var diagDetails string
	completedAt := r.existingStack.LatestStackConfiguration.StatusTimestamps.CompletedAt

	// Check if the completedAt timestamp is nil, if so, we log an error and return an error diagnostic.
	if completedAt == nil {
		tflog.Error(ctx, fmt.Sprintf("The stack configuration %s has converged, but the latest configuration rollout does not have a completed timestamp. Please check the stack configuration status.", r.existingStack.LatestStackConfiguration.ID))
		diags.AddError("Stack Configuration Converged Without Completed Timestamp",
			fmt.Sprintf("The stack configuration %s has converged, but the latest configuration rollout does not have a completed timestamp. Please check the stack configuration status.", r.existingStack.LatestStackConfiguration.ID))
		return stackConstants.UnknownStackPlanUpdateStrategy, diags
	}

	// Check if the completedAt timestamp is within the cooldown period, if so, we log a warning and return a `RetainPlanStackPlanUpdateStrategy`.
	if time.Since(*completedAt) <= convergedConfigUploadCooldownPeriod {
		diagSummary = fmt.Sprintf("Stack Configuration Converged %d Minute Ago. Current Cool-down Period is %d minute(s)",
			int64(time.Since(*completedAt).Minutes()), int64(convergedConfigUploadCooldownPeriod.Minutes()))
		diagDetails = fmt.Sprintf("The stack configuration %s has converged at %s, Please wait at least %d minute before attempting the same configuration upload.",
			r.existingStack.LatestStackConfiguration.ID, completedAt.String(), int64(convergedConfigUploadCooldownPeriod.Minutes()))
		tflog.Warn(ctx, diagDetails)
		diags.AddWarning(diagSummary, diagDetails)
		return stackConstants.RetainPlanStackPlanUpdateStrategy, diags
	}

	// If the completedAt timestamp is older than the cooldown period, we log a warning and return a `ModifyPlanStackPlanUpdateStrategy`.
	tflog.Warn(ctx, fmt.Sprintf(configTerminalStateMsg, tfe.StackConfigurationStatusConverged.String()))
	diagSummary = fmt.Sprintf("Stack Configuration Converged %d Minute Ago. Current Cool-down Period is %d minute(s)",
		int64(time.Since(*completedAt).Minutes()), int64(convergedConfigUploadCooldownPeriod.Minutes()))
	diagDetails = fmt.Sprintf("The stack configuration %s has converged at %s. Uploading configuration files and waiting for the stack configuration to converge, cancel, or error out.",
		r.existingStack.LatestStackConfiguration.ID, completedAt.String())
	diags.AddWarning(diagSummary, diagDetails)
	return stackConstants.ModifyPlanStackPlanUpdateStrategy, diags
}

// handleUpdateActionConfigFileChange handles the update action when the configuration files have changed.
func (r *stackMigrationResource) handleUpdateActionConfigFileChange(ctx context.Context, sourceBundleUploadAllowed bool) (stackConstants.StackPlanUpdateStrategy, diag.Diagnostics) {
	var diags diag.Diagnostics
	if !sourceBundleUploadAllowed {
		tflog.Error(ctx, fmt.Sprintf("Source bundle upload is not allowed for stack %s. Please wait for the current configuration to converge, cancel, or error out before uploading a new configuration.", r.existingStack.Name))
		diags.AddError("Source Bundle Upload Not Allowed",
			fmt.Sprintf("Source bundle upload is not allowed for stack %s. Please wait for the current configuration to converge, cancel, or error out before uploading a new configuration.", r.existingStack.Name))
		return stackConstants.UnknownStackPlanUpdateStrategy, diags
	} else {
		tflog.Debug(ctx, "Configuration files have changed, and source bundle upload is allowed.")
		return stackConstants.ModifyPlanStackPlanUpdateStrategy, nil
	}
}

// handleUpdateActionConfigStatusChange handles the update action based on the configuration status change.
func (r *stackMigrationResource) handleUpdateActionConfigStatusChange(ctx context.Context, planConfigStatus tfe.StackConfigurationStatus, stateConfigStatus tfe.StackConfigurationStatus) (stackConstants.StackPlanUpdateStrategy, diag.Diagnostics) {

	switch planConfigStatus {
	case tfe.StackConfigurationStatusCanceled, tfe.StackConfigurationStatusErrored:
		tflog.Debug(ctx, fmt.Sprintf(configTerminalStateMsg, planConfigStatus.String()))
		return stackConstants.ModifyPlanStackPlanUpdateStrategy, nil
	case tfe.StackConfigurationStatusConverged:
		return r.handleUploadActionConfigStatusChangedToConverged(ctx, planConfigStatus, stateConfigStatus)
	case tfe.StackConfigurationStatusConverging:
		return r.handleUpdateActionConfigStatusChangedToConverging(ctx, planConfigStatus)
	default:
		tflog.Debug(ctx, fmt.Sprintf("Configuration status changed to %s. Saving the current status from plan as is.", planConfigStatus.String()))
		return stackConstants.RetainPlanStackPlanUpdateStrategy, nil
	}
}

// handleUploadActionConfigStatusChangedToConverged handles the upload action when the configuration status has changed to `converged`.
func (r *stackMigrationResource) handleUploadActionConfigStatusChangedToConverged(ctx context.Context, planConfigStatus tfe.StackConfigurationStatus, stateConfigStatus tfe.StackConfigurationStatus) (stackConstants.StackPlanUpdateStrategy, diag.Diagnostics) {
	var diags diag.Diagnostics
	allowResyncOnConverged := false

	// read env variable TF_MIGRATE_STACK_ALLOW_RESYNC_ON_CONVERGED is set
	if allowResyncOnConvergedEnv, ok := os.LookupEnv(TfMigrateResyncOnConvergedEnvName); ok {
		allowResyncOnConvergedEnvBool, err := strconv.ParseBool(allowResyncOnConvergedEnv)
		if err != nil {
			tflog.Error(ctx, fmt.Sprintf("Error parsing environment variable %s: %s", TfMigrateResyncOnConvergedEnvName, err.Error()))
		}
		allowResyncOnConverged = allowResyncOnConvergedEnvBool
	}

	// If the state configuration status in both plan and state is converged, and the environment variable is set to true,
	// we allow re-uploading the same configuration files.
	if stateConfigStatus == tfe.StackConfigurationStatusConverged && allowResyncOnConverged {
		return r.handleReUploadOfSameConfigOnConverged(ctx)
	}

	// If the state configuration is anything other than converged and the plan configuration is converged,
	// we return a `RetainPlanStackPlanUpdateStrategy` to retain the plan as is as the stack configuration
	// has converged.
	tflog.Debug(ctx, fmt.Sprintf(configTerminalStateMsg, planConfigStatus.String()))
	return stackConstants.RetainPlanStackPlanUpdateStrategy, diags
}

// handleUpdateActionConfigStatusChangedToConverging handles the update action when the configuration status has changed to `converging`.
func (r *stackMigrationResource) handleUpdateActionConfigStatusChangedToConverging(ctx context.Context, planConfigStatus tfe.StackConfigurationStatus) (stackConstants.StackPlanUpdateStrategy, diag.Diagnostics) {
	var diags diag.Diagnostics

	uploadAllowed, err := r.handleConvergingStatusForAllowUpload(ctx, r.existingStack.LatestStackConfiguration)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf(errDiagDetailsSourceBundleUploadChk, r.existingStack.Name, err.Error()))
		diags.AddError(errDiagSummarySourceBundleUploadChk,
			fmt.Sprintf(errDiagDetailsSourceBundleUploadChk, r.existingStack.Name, err.Error()))
		return stackConstants.UnknownStackPlanUpdateStrategy, diags
	}

	if uploadAllowed {
		tflog.Debug(ctx, fmt.Sprintf("Configuration status changed to %s, which is converging. Uploading configuration files and waiting for the stack configuration to converge, cancel, or error out.", planConfigStatus.String()))
		return stackConstants.ModifyPlanStackPlanUpdateStrategy, diags
	}

	tflog.Debug(ctx, fmt.Sprintf("Configuration status changed to %s. Saving the current status from plan as is.", planConfigStatus.String()))
	return stackConstants.RetainPlanStackPlanUpdateStrategy, diags
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

// modifyPlanForStrategy modifies the plan based on the update strategy.
func modifyPlanForStrategy(ctx context.Context, planModifyStrategy stackConstants.StackPlanUpdateStrategy, plan *StackMigrationResourceModel) bool {
	if planModifyStrategy == stackConstants.ModifyPlanStackPlanUpdateStrategy {
		plan.CurrentConfigurationStatus = basetypes.NewStringUnknown()
		plan.CurrentConfigurationId = basetypes.NewStringUnknown()
		plan.SourceBundleHash = basetypes.NewStringUnknown()
		tflog.Debug(ctx, fmt.Sprintf("Plan modified for configuration upload: set status, ID, and hash to unknown, strategy: %s", planModifyStrategy.String()))
		return true
	}

	tflog.Debug(ctx, fmt.Sprintf("No plan modifications needed for strategy: %s", planModifyStrategy.String()))
	return false
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
