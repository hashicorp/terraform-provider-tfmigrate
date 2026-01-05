package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	stackConstants "terraform-provider-tfmigrate/internal/constants/stack"
	httpUtil "terraform-provider-tfmigrate/internal/util/net"

	"github.com/hashicorp/terraform-plugin-framework/attr"

	"golang.org/x/exp/maps"

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/hashicorp/go-tfe"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

type DeploymentNameSymmetricDiffRequest struct {
	DeploymentNamesFromConfig mapset.Set[string]
	StackId                   string
	StackConfigurationId      string
	HttpClient                httpUtil.Client
	Config                    *tfe.Config
}

// allowSourceBundleUpload checks if the stack configuration is in a state that allows uploading a new source bundle.
func (r *stackMigrationResource) allowSourceBundleUpload(ctx context.Context, configuration *tfe.StackConfiguration) (bool, error) {
	if configuration == nil {
		tflog.Info(ctx, "Current stack configuration is either nil, has no deployments. allowing source bundle upload.")
		return true, nil
	}

	stackConfigurationStatus := configuration.Status

	// handle completed status
	if stackConfigurationStatus == tfe.StackConfigurationStatusCompleted {
		return r.handleCompletedStatusForAllowUpload(ctx, configuration)
	}
	if slices.Contains(stackConstants.ErroredOrCancelledStackConfigurationStatuses, stackConfigurationStatus) {
		tflog.Info(ctx, fmt.Sprintf("Current stack configuration %s is in %s status. allowing source bundle upload.", configuration.ID, configuration.Status))
		return true, nil
	}

	// for all other statuses, do not allow source bundle upload
	tflog.Info(ctx, fmt.Sprintf("Current stack configuration %s is in %s status. not allowing source bundle upload.", configuration.ID, configuration.Status))
	return false, nil
}

// applyStackConfiguration uploads the stack configuration files to the stack and waits for the stack configuration to converge, cancel, or error out.
func (r *stackMigrationResource) applyStackConfiguration(ctx context.Context, orgName string, projectName string, stackName string, configDirAbsPath string, migrationMap map[string]string, uploadNewConfig bool, validatePreCondition bool) (bool, StackMigrationResourceModel, diag.Diagnostics) {
	var currentConfigurationId string
	var currentConfigurationStatus string
	var currentSourceBundleHash string
	var diags diag.Diagnostics
	var state StackMigrationResourceModel

	// Validate preconditions
	if validatePreCondition {
		if diags := r.createActionPreconditions(ctx, orgName, projectName, stackName, configDirAbsPath, migrationMap); diags.HasError() {
			tflog.Error(ctx, "Preconditions for resource creation failed")
			return false, state, diags
		}
	}

	// Calculate the hash of the Terraform configuration files in the directory
	terraformConfigHash, err := r.tfeUtil.CalculateConfigFileHash(r.terraformConfigDirAbsPath)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf(configHashErrDetails, configDirAbsPath, err.Error()))
		diags.AddError(
			configHasErrSummary,
			fmt.Sprintf(configHashErrDetails, configDirAbsPath, err.Error()),
		)
		return false, state, diags
	}

	// Get the current configuration ID and source bundle hash
	currentConfigurationIdPtr, currentSourceBundleHashPtr, diags := r.getConfigIdAndSourceBundleHash(ctx, configDirAbsPath, uploadNewConfig)
	if diags.HasError() {
		tflog.Error(ctx, fmt.Sprintf("Failed to get currentConfigurationId and sourceBundleHash for stack %s", r.existingStack.Name))
		return false, state, diags
	}

	currentConfigurationId = *currentConfigurationIdPtr
	currentSourceBundleHash = *currentSourceBundleHashPtr

	// Ensure the currentConfigurationId is not empty before proceeding
	if currentConfigurationId == "" {
		diags.AddError(
			"Post create validation failure: Missing Configuration ID",
			"After uploading or syncing the stack configuration, the resulting configuration ID was empty. This indicates an internal logic or API error. Aborting resource creation.",
		)
		return false, state, diags
	}

	// Attempt to poll for the current configuration status and await for it to complete, cancel, or error out.
	tflog.Info(ctx, fmt.Sprintf("Starting to poll for current configuration ID: %s", currentConfigurationId))
	currentConfigurationStatus = r.watchStackConfigurationUntilTerminalStatus(ctx, currentConfigurationId).String()
	tflog.Info(ctx, fmt.Sprintf("Received status: %s for configuration ID: %s after polling ", currentConfigurationStatus, currentConfigurationId))

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
	migrationMapAsStateAttribute := map[string]attr.Value{}
	for k, v := range migrationMap {
		migrationMapAsStateAttribute[k] = types.StringValue(v)
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
	state.WorkspaceDeploymentMapping = types.MapValueMust(types.StringType, migrationMapAsStateAttribute)

	// if configurationStatus == tfe.StackConfigurationStatusCanceled {
	//	diags.AddWarning(
	//		"Stack Configuration Canceled",
	//		fmt.Sprintf("The current stack configuration %s has been canceled. No state would be uploaded", currentConfigurationId),
	//	)
	//	return false, state, diags
	//}

	if configurationStatus == tfe.StackConfigurationStatusFailed {
		diags.Append(r.tfeUtil.ReadStackDiagnosticsByConfigID(currentConfigurationId, r.httpClient, r.tfeConfig)...)
		return false, state, diags
	}

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

// createActionPreconditions checks if the resource passes the preconditions for the create action.
func (r *stackMigrationResource) createActionPreconditions(ctx context.Context, orgName string, projectName string, stackName string, stackConfigDir string, migrationMap map[string]string) diag.Diagnostics {

	var diags diag.Diagnostics
	var err error

	deploymentNamesFromMigrationMap := mapset.NewSet[string]()
	for _, deploymentName := range migrationMap {
		if deploymentNamesFromMigrationMap.Contains(deploymentName) {
			tflog.Error(ctx, fmt.Sprintf("Duplicate deployment name found in migration map: %s", deploymentName))
			diags.AddError(
				"Duplicate Deployment Name",
				fmt.Sprintf("The deployment name %q is duplicated in the migration map. Each deployment name must be unique.", deploymentName),
			)
			return diags
		}
		deploymentNamesFromMigrationMap.Add(deploymentName)
	}

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

// handleCompletedStatusForAllowUpload checks if a converging stack configuration has any running plans, return true if no running plans are found false otherwise.
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

	if slices.Contains(stackConstants.RunningStackConfigurationStatuses, currentConfigurationStatus) {
		diags.AddWarning(
			"Stack Configuration Status Not Ready for State Upload",
			fmt.Sprintf("The currentstack configuration %s is in a status (%s) that does not allow state upload. Please wait for the stack configuration to be in completed state before running again", currentConfigurationId, currentConfigurationStatus),
		)
		return diags
	}

	return diags
}

// getCurrentMigrationDataFromExistingState retrieves the current migration data based on the existing state of the resource.
func (r *stackMigrationResource) getCurrentMigrationDataFromExistingState(ctx context.Context, workspaceDeploymentMapFromState types.Map) (map[string]StackMigrationData, diag.Diagnostics) {
	workspaceDeploymentMap := map[string]string{}
	var diags diag.Diagnostics
	if diags = workspaceDeploymentMapFromState.ElementsAs(ctx, &workspaceDeploymentMap, false); diags.HasError() {
		return nil, diags
	}

	if len(workspaceDeploymentMap) == 0 {
		diags.AddError(
			"Error Reading Workspace Deployment Mapping",
			"The workspace_deployment_mapping attribute is empty. This attribute must contain a mapping of workspace names to stack deployment names.")
		return nil, diags
	}

	// get the latest deployment groups and status for each deployment in the workspaceDeploymentMap
	stackMigrationData, err := r.migrationHashService.GenerateMigrationData(StackMigrationTrackRequest{
		OrgName:      r.existingOrganization.Name,
		StackId:      r.existingStack.ID,
		MigrationMap: workspaceDeploymentMap,
	})

	if err != nil {
		diags.AddError(
			"Error Generating Migration Data",
			fmt.Sprintf("Could not generate migration data for stack %q in organization %q: %s", r.existingStack.Name, r.existingOrganization.Name, err.Error()),
		)
		return nil, diags
	}

	return stackMigrationData, diags
}

// getWorkspacesForDeploymentRetry identifies workspaces with failed or abandoned deployments from migration data.
// It returns a set of workspace names along with diagnostics in case of mismatched workspace data.
func (r *stackMigrationResource) getWorkspacesForDeploymentRetry(ctx context.Context, currentData,
	previousData map[string]StackMigrationData) (mapset.Set[string], diag.Diagnostics) {
	var diags diag.Diagnostics
	previousWorkspaceNames := mapset.NewSet(maps.Keys(previousData)...)
	currentWorkspaceNames := mapset.NewSet(maps.Keys(currentData)...)
	if !previousWorkspaceNames.Equal(currentWorkspaceNames) {
		diags.AddError(
			"Workspace Names Mismatch",
			fmt.Sprintf("The workspace names in the current migration data do not match those in the previous"+
				" migration data. Cannot determine deployment status differences For Completed ConfigurationId %q",
				r.existingStack.LatestStackConfiguration.ID),
		)
		return nil, diags
	}
	var retryWorkspaceList = mapset.NewSet[string]()
	for workspaceName := range currentData {
		migrationData := currentData[workspaceName]
		currentStatus := migrationData.DeploymentGroupData.Status

		if currentStatus == tfe.DeploymentGroupStatusFailed || currentStatus == tfe.DeploymentGroupStatusAbandoned {
			retryWorkspaceList.Add(workspaceName)
		} else if slices.Contains(stackConstants.RunningDeploymentGroupStatuses, currentStatus) {
			isAwaitingProviderAction, diagsStep := r.isCurrentStepAwaitingProviderAction(ctx, migrationData.DeploymentName)
			if diagsStep.HasError() {
				diags.Append(diagsStep...)
				return nil, diags
			}
			if isAwaitingProviderAction {
				retryWorkspaceList.Add(workspaceName)
			}
		}
	}

	return retryWorkspaceList, diags
}

func (r *stackMigrationResource) isCurrentStepAwaitingProviderAction(ctx context.Context, deploymentName string) (bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	// read the latest deployment run for the deployment
	tflog.Info(ctx, fmt.Sprintf("Reading latest deployment run for deployment %s in stack %s", deploymentName, r.existingStack.Name))
	latestRun, err := r.tfeUtil.ReadLatestDeploymentRunWithRelations(r.existingStack.ID, deploymentName, r.httpClient, r.tfeConfig, r.tfeClient)
	if err != nil {
		diags.AddError(
			"Error Reading Latest Deployment Run",
			fmt.Sprintf("Could not read latest deployment run for deployment %q in stack %q: %s",
				deploymentName,
				r.existingStack.Name,
				err.Error()),
		)
		return false, diags
	}

	if latestRun == nil {
		diags.AddError(
			"Error Reading Deployment Group from Latest Deployment Run",
			fmt.Sprintf("Could not read deployment group from latest deployment run for deployment %q in stack %q",
				deploymentName,
				r.existingStack.Name),
		)
		return false, diags
	}

	// validate the latestRun and its relationships
	latestRunRelationships := latestRun.Relationships
	if latestRunRelationships == nil {
		diags.AddError(
			"Error Reading Deployment Group from Latest Deployment Run",
			fmt.Sprintf("Could not read deployment group from latest deployment run for deployment %q in stack %q",
				deploymentName,
				r.existingStack.Name),
		)
		return false, diags
	}

	// validate the current step relationship
	if latestRunRelationships.CurrentStep == nil ||
		latestRunRelationships.CurrentStep.Data == nil ||
		latestRunRelationships.CurrentStep.Data.Id == "" {
		return false, diags
	}

	var currentStepId = latestRunRelationships.CurrentStep.Data.Id

	// read the current step details
	currentStep, err := r.tfeClient.StackDeploymentSteps.Read(ctx, currentStepId)
	if err != nil {
		diags.AddError(
			"Error Reading Deployment Run Steps",
			fmt.Sprintf("Could not read deployment run steps for step ID %q in deployment %q in stack %q: %s",
				currentStepId,
				deploymentName,
				r.existingStack.Name,
				err.Error()),
		)
		return false, diags
	}

	// validate current step details
	if currentStep == nil || currentStep.ID == "" ||
		currentStep.OperationType == "" ||
		currentStep.Status == "" {
		diags.AddError(
			"Error Reading Current Deployment Step Details",
			fmt.Sprintf("Could not read current deployment step details for step ID %q in deployment %q in stack %q",
				currentStepId,
				deploymentName,
				r.existingStack.Name),
		)
		return false, diags
	}

	currentStepOperationType := currentStep.OperationType
	currentStepStatus := currentStep.Status

	// check if the current step is awaiting provider action
	if (currentStepOperationType == "allow-import" && currentStepStatus == "pending_operator") ||
		(currentStepOperationType == "import-state" && (currentStepStatus == "pending_operator" || currentStepStatus == "running")) {
		return true, diags
	}

	return false, diags
}

func (r *stackMigrationResource) isIdempotentConfig(plan, state StackMigrationResourceModel, currentSourceBundleHash, currentTerraformConfigHash string) (bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	planWorkspaceDeploymentMappingElements := plan.WorkspaceDeploymentMapping.Elements()
	stateWorkspaceDeploymentMappingElements := state.WorkspaceDeploymentMapping.Elements()

	planDeploymentMapping := make(map[string]string, len(planWorkspaceDeploymentMappingElements))
	for key, value := range planWorkspaceDeploymentMappingElements {
		planDeploymentMapping[key] = value.(types.String).ValueString()
	}

	stateDeploymentMapping := make(map[string]string, len(stateWorkspaceDeploymentMappingElements))
	for key, value := range stateWorkspaceDeploymentMappingElements {
		stateDeploymentMapping[key] = value.(types.String).ValueString()
	}

	// check if a deployment mapping change has occurred
	if !maps.Equal(planDeploymentMapping, stateDeploymentMapping) {
		// if there is a difference, then throw an error
		diags.AddError(
			"Deployment Mapping Change Not Allowed During Running Deployments",
			fmt.Sprintf("Changes to the workspace_deployment_mapping are not allowed while there are running deployment groups for stack %q in organization %q and project %q. Please wait for the running deployments to complete before making changes to the deployment mapping.",
				r.existingStack.Name,
				r.existingOrganization.Name,
				r.existingProject.Name,
			),
		)
		return false, diags
	}

	// check for source bundle hash changes
	if state.SourceBundleHash.ValueString() != currentSourceBundleHash {
		// if there is a difference, then throw an error
		diags.AddError(
			"Source Bundle Hash Change Not Allowed During Running Deployments",
			fmt.Sprintf("Changes to the files in dir %q are not allowed while there are running deployment groups for"+
				" stack %q in organization %q and project %q. Please wait for the running deployments to complete before making changes to the source bundle.",
				plan.ConfigurationDir.ValueString(),
				r.existingStack.Name,
				r.existingOrganization.Name,
				r.existingProject.Name,
			),
		)
		return false, diags
	}

	// check for terraform config hash changes
	if state.TerraformConfigHash.ValueString() != currentTerraformConfigHash {
		// if there is a difference, then throw an error
		diags.AddError(
			"Terraform Configuration Hash Change Not Allowed During Running Deployments",
			fmt.Sprintf("Changes to the files in dir %q are not allowed while there are running deployment groups for"+
				" stack %q in organization %q and project %q. Please wait for the running deployments to complete before making changes to the terraform configuration.",
				plan.TerraformConfigDir.ValueString(),
				r.existingStack.Name,
				r.existingOrganization.Name,
				r.existingProject.Name,
			),
		)
		return false, diags
	}
	return true, diags
}

func (r *stackMigrationResource) getConfigIdAndSourceBundleHash(ctx context.Context, configDirAbsPath string, uploadNewConfig bool) (*string, *string, diag.Diagnostics) {
	var currentConfigurationId string
	var currentSourceBundleHash string
	var diags diag.Diagnostics

	if uploadNewConfig {
		tflog.Info(ctx, fmt.Sprintf("Uploading new configuration files for stack %s", r.existingStack.Name))
		return r.uploadNewConfigAndGetSourceBundleHash(ctx, configDirAbsPath)
	}

	tflog.Info(ctx, fmt.Sprintf("Syncing the latest stack configuration data for stack %s", r.existingStack.Name))
	if currentConfigurationId, currentSourceBundleHash, diags = r.syncExistingStackConfigurationData(ctx, r.existingStack, configDirAbsPath); diags.HasError() {
		tflog.Error(ctx, fmt.Sprintf("Failed to get currentConfigurationId and sourceBundleHash for stack %s", r.existingStack.Name))
		return nil, nil, diags
	}

	return &currentConfigurationId, &currentSourceBundleHash, diags
}

func (r *stackMigrationResource) uploadNewConfigAndGetSourceBundleHash(ctx context.Context, configDirAbsPath string) (*string, *string, diag.Diagnostics) {
	var diags diag.Diagnostics
	var currentConfigurationId string
	var currentSourceBundleHash string

	allowed, err := r.allowSourceBundleUpload(ctx, r.existingStack.LatestStackConfiguration)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf(errDiagDetailsSourceBundleUploadChk, r.existingStack.Name, err.Error()))
		diags.AddError(
			errDiagDetailsSourceBundleUploadChk,
			fmt.Sprintf(errDiagDetailsSourceBundleUploadChk, r.existingStack.Name, err.Error()),
		)
		return nil, nil, diags
	}
	if !allowed {
		diags.AddError(
			"Source Bundle Upload Is Requested But Is Not Allowed",
			fmt.Sprintf("The current stack configuration for stack %s is in a state that does not allow uploading a new source bundle. Please wait for any running deployments to complete before attempting to upload a new source bundle.", r.existingStack.Name),
		)
		return nil, nil, diags
	}

	tflog.Info(ctx, fmt.Sprintf("Uploading source bundle for stack %s from directory %s", r.existingStack.Name, configDirAbsPath))
	if currentConfigurationId, currentSourceBundleHash, diags = r.uploadSourceBundle(ctx, r.existingStack.ID, configDirAbsPath, true); diags.HasError() {
		return nil, nil, diags
	}
	return &currentConfigurationId, &currentSourceBundleHash, diags
}

func (r *stackMigrationResource) CheckDeploymentNameDifferences(ctx context.Context, request DeploymentNameSymmetricDiffRequest) (bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	// read the deployment groups from the API
	deploymentNamesByConfigId, err := r.tfeUtil.GetAllDeploymentNamesForAConfigId(request.StackId, request.StackConfigurationId, request.HttpClient, request.Config)
	if err != nil {
		err := fmt.Sprintf("error reading deployment names for stack %s and configuration %s, err : %q", request.StackId, request.StackConfigurationId, err)
		tflog.Error(ctx, err)
		diags.AddError(
			"Error Reading Deployment Names from Stack Configuration",
			err,
		)
		return false, diags
	}

	deploymentNameDiffExists := deploymentNamesByConfigId.SymmetricDifference(request.DeploymentNamesFromConfig).Cardinality() > 0

	if deploymentNameDiffExists {
		tflog.Info(ctx, fmt.Sprintf("Deployment names from stack config files %v do not match the deployment names from API response %v for stack %s and configurationId %s, deploymentNameDiffExists: %t",
			request.DeploymentNamesFromConfig, deploymentNamesByConfigId, request.StackId, request.StackConfigurationId, deploymentNameDiffExists))
	} else {
		tflog.Info(ctx, fmt.Sprintf("Deployment names from stack config files %v match the deployment names from API response %v for stack %s and configurationId %s, deploymentNameDiffExists: %t",
			request.DeploymentNamesFromConfig, deploymentNamesByConfigId, request.StackId, request.StackConfigurationId, deploymentNameDiffExists))
	}

	return deploymentNameDiffExists, diags

}

// prettyPrintJSON pretty prints a given interface as a JSON string.
func prettyPrintJSON(v interface{}) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("error marshaling JSON: %v", err)
	}
	return string(b)
}
