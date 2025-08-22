package provider

import (
	"context"
	"fmt"
	"terraform-provider-tfmigrate/internal/models"

	"github.com/hashicorp/go-tfe"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

func (r *stackMigrationResource) uploadStackDeploymentsState(ctx context.Context, migrationMap map[string]string) map[string]StackMigrationData {
	var migrationDataMap = make(map[string]StackMigrationData)

	for workspaceName, deploymentName := range migrationMap {
		migrationData := r.uploadWorkspaceStateToStackDeployment(ctx, workspaceName, deploymentName)
		migrationDataMap[workspaceName] = migrationData
	}
	return migrationDataMap
}

func (r *stackMigrationResource) uploadWorkspaceStateToStackDeployment(ctx context.Context, workspaceName string, deploymentName string) StackMigrationData {
	migrationData := StackMigrationData{
		DeploymentName: deploymentName,
	}
	organizationName := r.existingOrganization.Name

	// 1. Get the workspace
	workspace, err := r.tfeUtil.ReadWorkspaceByName(organizationName, workspaceName, r.tfeClient)
	if err != nil {
		errorMessage := fmt.Sprintf("Error reading workspace name: %s, error: %v", workspaceName, err)
		tflog.Error(ctx, errorMessage)
		migrationData.FailureReason = errorMessage
		return migrationData
	}
	migrationData.WorkspaceId = workspace.ID

	// 2. Get the latest deployment runId for the deployment
	continueToFetchDeploymentRunSteps, mostRecentDeploymentRun := r.getLatestDeploymentRun(ctx, deploymentName, &migrationData)
	if !continueToFetchDeploymentRunSteps {
		return migrationData
	}

	// 3. check if a rerun is needed
	deploymentGroupStatus := tfe.DeploymentGroupStatus(mostRecentDeploymentRun.StackDeploymentGroup.Status)
	continueOnImport := r.handleDeploymentGroupTerminalState(ctx, deploymentGroupStatus, &migrationData, mostRecentDeploymentRun.StackDeploymentGroup.ID, deploymentName)
	if !continueOnImport {
		return migrationData
	}

	// In case of a failed deployment group,
	// we need to trigger a rerun
	// which creates new deployment runIds that is why we need to fetch the latest deployment run again

	if deploymentGroupStatus == tfe.DeploymentGroupStatusFailed {
		continueToFetchDeploymentRunSteps, mostRecentDeploymentRun = r.getLatestDeploymentRun(ctx, deploymentName, &migrationData)

		if !continueToFetchDeploymentRunSteps {
			errorMessage := fmt.Sprintf("No deployment run found for deployment: %s", deploymentName)
			tflog.Error(ctx, errorMessage)
			migrationData.FailureReason = errorMessage
			return migrationData
		}
	}

	// 4. get the steps of the deployment run
	deploymentRunSteps := r.fetchDeploymentRunStep(ctx, &migrationData, mostRecentDeploymentRun.ID, deploymentName)
	if deploymentRunSteps == nil {
		return migrationData
	}

	// 5. Validate an allow-import step
	allowImportStep, allowImport, callAdvanceOnAllowImport := r.handleDeploymentRunStepsAllowImport(ctx, deploymentRunSteps, deploymentName, &migrationData)
	if !allowImport {
		return migrationData
	}

	// 6. Call the advance endpoint on an allow-import step if it is in the pending_operator state
	if callAdvanceOnAllowImport {
		if err := r.tfeUtil.AdvanceDeploymentRunStep(allowImportStep.Id, r.tfeClient); err != nil {
			errorMessage := fmt.Sprintf("Error advancing deployment run to allow import for stack: %s, deployment: %s, error: %v", r.existingStack.ID, deploymentName, err)
			tflog.Error(ctx, errorMessage)
			migrationData.FailureReason = errorMessage
			return migrationData
		}
		tflog.Info(ctx, fmt.Sprintf("Advanced deployment run to allow import for stack: %s, deployment: %s", r.existingStack.ID, deploymentName))
		return migrationData
	}

	// 7. If the allow-import step is completed, check for an import-state step
	importStateStep := r.getImportStateStep(deploymentRunSteps)
	if importStateStep == nil {
		errorMessage := fmt.Sprintf("No import-state step found for deployment: %s", deploymentName)
		tflog.Error(ctx, errorMessage)
		migrationData.FailureReason = errorMessage
		return migrationData
	}

	// check for upload-url if importStateStep.Links
	if importStateStep.Links == nil || importStateStep.Links["upload-url"] == nil {
		errorMessage := fmt.Sprintf("No upload-url found for import-state step for deployment: % s", deploymentName)
		tflog.Error(ctx, errorMessage)
		migrationData.FailureReason = errorMessage
		return migrationData
	}
	uploadURL := importStateStep.Links["upload-url"].(string)

	continueMigration, triggerStateUpload := r.handleImportStateStep(ctx, importStateStep, deploymentName, &migrationData)
	if !continueMigration {
		return migrationData
	}

	callAdvanceOnImportState := false

	if triggerStateUpload {
		diags := r.convertWorkspaceStateAndUpload(ctx, migrationData.WorkspaceId, uploadURL)

		if diags.HasError() {
			errorMessage := fmt.Sprintf("Error converting and uploading workspace state for stack: %s, deployment: %s, error: %v", r.existingStack.ID, deploymentName, diags[0].Summary())
			tflog.Error(ctx, errorMessage)
			migrationData.FailureReason = errorMessage
			return migrationData
		}
		tflog.Info(ctx, fmt.Sprintf("Successfully converted and uploaded workspace state for stack: %s, deployment: %s", r.existingStack.ID, deploymentName))
		callAdvanceOnImportState = true
	}

	// FIXME: Validate the import-state step status is pending_operator before advancing

	if callAdvanceOnImportState {
		if err := r.tfeUtil.AdvanceDeploymentRunStep(importStateStep.Id, r.tfeClient); err != nil {
			errorMessage := fmt.Sprintf("Error advancing deployment run step for import-state for stack: %s, deployment: %s, error: %v", r.existingStack.ID, deploymentName, err)
			tflog.Error(ctx, errorMessage)
			migrationData.FailureReason = errorMessage
			return migrationData
		}
	}

	tflog.Info(ctx, fmt.Sprintf("Advanced deployment run step for import-state for stack: %s, deployment: %s", r.existingStack.ID, deploymentName))

	return migrationData

}

func (r *stackMigrationResource) handleImportStateStep(ctx context.Context, importStateStep *models.StackDeploymentStep, deploymentName string, migrationData *StackMigrationData) (continueMigration bool, triggerStateUpload bool) {
	switch importStateStep.Attributes.Status {
	case "pending_operator":
		// If the import-state step is in the pending_operator state, we need to advance it
		if err := r.tfeUtil.AdvanceDeploymentRunStep(importStateStep.Id, r.tfeClient); err != nil {
			errorMessage := fmt.Sprintf("Error advancing deployment run step for import-state for stack: %s, deployment: %s, error: %v", r.existingStack.ID, deploymentName, err)
			tflog.Error(ctx, errorMessage)
			migrationData.FailureReason = errorMessage
			return false, false
		} else {
			tflog.Info(ctx, fmt.Sprintf("Advanced deployment run step for import-state for stack: %s, deployment: %s", r.existingStack.ID, deploymentName))
			return true, false
		}
	case "completed":
		tflog.Info(ctx, fmt.Sprintf("Import-state step for stack: %s, deployment: %s is already completed", r.existingStack.ID, deploymentName))
		return true, false
	case "running":
		return true, true
	default:
		errorMessage := fmt.Sprintf("Import-state step for deployment %s is in unexpected state: %s", deploymentName, importStateStep.Attributes.Status)
		tflog.Error(ctx, errorMessage)
		migrationData.FailureReason = errorMessage
		return false, false
	}
}

func (r *stackMigrationResource) getImportStateStep(deploymentRunSteps []models.StackDeploymentStep) *models.StackDeploymentStep {
	var importStateStep *models.StackDeploymentStep
	for _, step := range deploymentRunSteps {
		operationType := step.Attributes.OperationType
		if operationType == "import-state" {
			importStateStep = &step
			break
		}
	}
	return importStateStep
}

func (r *stackMigrationResource) handleDeploymentRunStepsAllowImport(ctx context.Context, deploymentRunSteps []models.StackDeploymentStep, deploymentName string, migrationData *StackMigrationData) (step *models.StackDeploymentStep, hasAllowImport bool, callAdvance bool) {
	var allowImportStep *models.StackDeploymentStep
	for _, step := range deploymentRunSteps {
		operationType := step.Attributes.OperationType
		if operationType == "allow-import" {
			allowImportStep = &step
			break
		}
	}

	if allowImportStep == nil {
		errorMessage := fmt.Sprintf("No allow-import step found for deployment: %s", deploymentName)
		tflog.Error(ctx, errorMessage)
		migrationData.FailureReason = errorMessage
		return nil, false, false
	}

	switch allowImportStep.Attributes.Status {
	case "pending_operator":
		return step, true, true
	case "completed":
		return step, true, false
	default:
		errorMessage := fmt.Sprintf("Allow-import step for deployment %s is in unexpected state: %s", deploymentName, allowImportStep.Attributes.Status)
		tflog.Error(ctx, errorMessage)
		migrationData.FailureReason = errorMessage
		return nil, false, false
	}
}

func (r *stackMigrationResource) fetchDeploymentRunStep(ctx context.Context, migrationData *StackMigrationData, mostRecentDeploymentRunId string, deploymentName string) []models.StackDeploymentStep {
	deploymentRunSteps, err := r.tfeUtil.ReadDeploymentRunSteps(mostRecentDeploymentRunId, r.httpClient, r.tfeConfig)
	if err != nil {
		errorMessage := fmt.Sprintf("Error reading deployment run steps for stack: %s, deployment: %s, error: %v", r.existingStack.ID, deploymentName, err)
		tflog.Error(ctx, errorMessage)
		migrationData.FailureReason = errorMessage
		return nil
	}

	return deploymentRunSteps
}

func (r *stackMigrationResource) handleDeploymentGroupTerminalState(ctx context.Context, deploymentGroupStatus tfe.DeploymentGroupStatus, migrationData *StackMigrationData, deploymentGroupId string, deploymentName string) bool {
	if deploymentGroupStatus == tfe.DeploymentGroupStatusSucceeded {
		return false
	}

	if deploymentGroupStatus == tfe.DeploymentGroupStatusAbandoned {
		errorMessage := fmt.Sprintf("Deployment group %s for deployment %s is in abandoned state, please fix the dployment config in the stack configuration files and reupload to trigger the process", deploymentGroupId, deploymentName)
		tflog.Error(ctx, errorMessage)
		migrationData.FailureReason = errorMessage
		return false
	}

	if deploymentGroupStatus == tfe.DeploymentGroupStatusFailed {
		tflog.Warn(ctx, fmt.Sprintf("Deployment group %s for deployment %s is in failed state, rerunning the deployment-group", deploymentGroupId, deploymentName))
		if err := r.tfeUtil.RerunDeploymentGroup(migrationData.DeploymentGroupData.Id, []string{deploymentName}, r.tfeClient); err != nil {
			errorMessage := fmt.Sprintf("Error rerunning deployment group %s for deployment %s, error: %v", migrationData.DeploymentGroupData.Id, deploymentName, err)
			tflog.Error(ctx, errorMessage)
			migrationData.FailureReason = errorMessage
			return false
		}
	}

	return true
}

func (r *stackMigrationResource) getLatestDeploymentRun(ctx context.Context, deploymentName string, migrationData *StackMigrationData) (bool, *tfe.StackDeploymentRun) {
	stateDeploymentRun, err := r.tfeUtil.ReadLatestDeploymentRun(r.existingStack.ID, deploymentName, r.httpClient, r.tfeConfig, r.tfeClient)
	if err != nil {
		errorMessage := fmt.Sprintf("Error reading latest deployment run for stack: %s, deployment: %s, error: %v", r.existingStack.ID, deploymentName, err)
		tflog.Error(ctx, errorMessage)
		migrationData.FailureReason = errorMessage
		return false, nil
	}

	if stateDeploymentRun.ID == "" {
		failureReason := fmt.Sprintf("No deployment run found deployment: %s", deploymentName)
		tflog.Warn(ctx, failureReason)
		migrationData.FailureReason = failureReason
		return false, nil
	}

	if stateDeploymentRun.StackDeploymentGroup == nil {
		errorMessage := fmt.Sprintf("No deployment group found for deployment: %s", deploymentName)
		tflog.Error(ctx, errorMessage)
		migrationData.FailureReason = errorMessage
		return false, nil

	}

	// populate the migration data with the deployment run details
	migrationData.DeploymentGroupData = StackDeploymentGroupData{}
	migrationData.DeploymentGroupData.Id = stateDeploymentRun.StackDeploymentGroup.ID
	migrationData.DeploymentGroupData.Status = tfe.DeploymentGroupStatus(stateDeploymentRun.StackDeploymentGroup.Status)

	if !r.deploymentStateImportMap[deploymentName] {
		warningMessage := fmt.Sprintf("Deployemnt %s not marked for state import, no state will be imported", deploymentName)
		migrationData.Warnings = []string{warningMessage}
		return false, nil
	}

	return true, stateDeploymentRun
}
