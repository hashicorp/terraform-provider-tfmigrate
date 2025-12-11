package provider

import (
	"context"
	"fmt"
	"sync"
	"terraform-provider-tfmigrate/internal/models"
	"time"

	"github.com/hashicorp/go-tfe"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

func (r *stackMigrationResource) uploadStackDeploymentsState(ctx context.Context, migrationMap map[string]string) map[string]StackMigrationData {
	var migrationDataMap = make(map[string]StackMigrationData)

	var wg sync.WaitGroup
	var mu sync.Mutex

	for workspaceName, deploymentName := range migrationMap {
		wg.Add(1)
		go func(wsName, depName string) {
			defer wg.Done()
			migrationData := r.uploadWorkspaceStateToStackDeployment(ctx, wsName, depName)
			mu.Lock()
			migrationDataMap[wsName] = migrationData
			mu.Unlock()
		}(workspaceName, deploymentName)
	}
	wg.Wait()
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
	// the handleDeploymentGroupTerminalState function
	// returns false if the deployment group is already succeeded or abandoned
	// and true if the deployment group is failed and a rerun is triggered
	deploymentGroupStatus := tfe.DeploymentGroupStatus(mostRecentDeploymentRun.StackDeploymentGroup.Status)
	continueOnImport := r.handleDeploymentGroupTerminalState(ctx, deploymentGroupStatus, &migrationData, mostRecentDeploymentRun.StackDeploymentGroup.ID, deploymentName)
	if !continueOnImport {
		return migrationData
	}

	// In case of a failed deployment group,
	// we trigger a rerun from inside the handleDeploymentGroupTerminalState function
	// which creates a new deployment runId that is why we need to fetch the latest deployment run again
	if deploymentGroupStatus == tfe.DeploymentGroupStatusFailed ||
		(r.retryAbandonedDeployments && deploymentGroupStatus == tfe.DeploymentGroupStatusAbandoned) {
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
		migrationData.DeploymentGroupData.Status = deploymentGroupStatus
		migrationData.DeploymentGroupData.Id = mostRecentDeploymentRun.StackDeploymentGroup.ID
		migrationData.FailureReason = "Failed to fetch deployment run steps, This could be due to deployment run not being created properly, If the issue persists please check your deployment config or workspace state data and retry or reach out to support."
		return migrationData
	}

	// 5. Validate an allow-import step
	// validate if an allow-import step is present, in its steps
	// also checks if we need to call advance on the allow-import step
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
		tflog.Info(ctx, fmt.Sprintf("Advanced allow-import step stack: %s, deployment: %s", r.existingStack.ID, deploymentName))
	}

	tflog.Debug(ctx, fmt.Sprintf("Completed allow-import step for stack: %s, deployment: %s", r.existingStack.ID, deploymentName))

	// 7. If the allow-import step is completed, check for an import-state step
	importStateStep := r.getImportStateStep(deploymentRunSteps)
	if importStateStep == nil {
		errorMessage := fmt.Sprintf("No import-state step found for deployment: %s", deploymentName)
		tflog.Error(ctx, errorMessage)
		migrationData.FailureReason = errorMessage
		return migrationData
	}

	tflog.Debug(ctx, fmt.Sprintf("Import-state step found for deployment: %s", deploymentName))

	// Re-fetch the import state step to get the latest status and links
	time.Sleep(5 * time.Second) // wait for 5 seconds before fetching the step again to allow TFE to process the allow-import step
	readStepById, err := r.tfeUtil.ReadStepById(importStateStep.Id, r.tfeClient)
	if err != nil {
		errorMessage := fmt.Sprintf("Error reading import-state step by id for stack: %s, deployment: %s, error: %v", r.existingStack.ID, deploymentName, err)
		tflog.Error(ctx, errorMessage)
		migrationData.FailureReason = errorMessage
		return migrationData
	}

	importStateStep.Attributes.Status = readStepById.Status
	importStateStep.Attributes.CreatedAt = readStepById.CreatedAt
	importStateStep.Attributes.UpdatedAt = readStepById.UpdatedAt
	importStateStep.Links = readStepById.Links

	tflog.Debug(ctx, fmt.Sprintf("Re-fetched import-state step for deployment: %s with status: %s", deploymentName, importStateStep.Attributes.Status))

	// 8. check for upload-url in importStateStep.Links
	if importStateStep.Links == nil || importStateStep.Links["upload-url"] == nil {
		errorMessage := fmt.Sprintf("No upload-url found for import-state step for deployment: %s", deploymentName)
		tflog.Error(ctx, errorMessage)
		migrationData.FailureReason = errorMessage
		return migrationData
	}
	uploadURL := importStateStep.Links["upload-url"].(string)
	tflog.Debug(ctx, fmt.Sprintf("Found upload-url for import-state step for deployment: %s", deploymentName))

	continueMigration, triggerStateUpload := r.handleImportStateStep(ctx, importStateStep, deploymentName, &migrationData)
	if !continueMigration {
		return migrationData
	}

	fetchImportStepAgain := false
	if triggerStateUpload {
		tflog.Debug(ctx, fmt.Sprintf("Triggering workspace to stack conversion and state upload for deployment: %s", deploymentName))
		diags := r.convertWorkspaceStateAndUpload(ctx, migrationData.WorkspaceId, uploadURL)

		if diags.HasError() {
			errorMessage := fmt.Sprintf("Error converting and uploading workspace state for stack: %s, deployment: %s, error: %v", r.existingStack.ID, deploymentName, diags[0].Summary())
			tflog.Error(ctx, errorMessage)
			migrationData.FailureReason = errorMessage
			return migrationData
		}
		tflog.Info(ctx, fmt.Sprintf("Successfully converted and uploaded workspace state for stack: %s, deployment: %s", r.existingStack.ID, deploymentName))
		fetchImportStepAgain = true
	}

	callAdvanceOnImportState := false

	// 9. Validate the import-state step status is pending_operator before advancing
	if fetchImportStepAgain {
		time.Sleep(5 * time.Second) // wait for 5 seconds before fetching the step again to allow TFE to process the state upload
		tflog.Debug(ctx, fmt.Sprintf("Re-fetching import-state step to validate status for deployment: %s", deploymentName))
		callAdvanceOnImportState = r.reFetchImportStateStepAndValidate(ctx, &migrationData, importStateStep.Id, deploymentName)
		if !callAdvanceOnImportState {
			return migrationData
		}
	}

	// 10. Call the advance endpoint on the import-state step

	if callAdvanceOnImportState {
		tflog.Debug(ctx, fmt.Sprintf("Advancing import-state step for deployment: %s", deploymentName))
		if err := r.tfeUtil.AdvanceDeploymentRunStep(importStateStep.Id, r.tfeClient); err != nil {
			errorMessage := fmt.Sprintf("Error advancing deployment run step for import-state for stack: %s, deployment: %s, error: %v", r.existingStack.ID, deploymentName, err)
			tflog.Error(ctx, errorMessage)
			migrationData.FailureReason = errorMessage
			return migrationData
		}
		tflog.Info(ctx, fmt.Sprintf("Advanced deployment run step for import-state for stack: %s, deployment: %s", r.existingStack.ID, deploymentName))
	}

	// 11. One final call to deployment steps for 30 seconds 10 seconds apart to check if the plan and apply is successful
	r.syncDeploymentGroupDataAfterStateImport(ctx, &migrationData, deploymentName)

	tflog.Info(ctx, fmt.Sprintf("State import process completed for stack: %s, deployment: %s", r.existingStack.ID, deploymentName))

	return migrationData

}

func (r *stackMigrationResource) syncDeploymentGroupDataAfterStateImport(ctx context.Context, migrationData *StackMigrationData, name string) {
	const maxRetries = 10

	for i := 0; i < maxRetries; i++ {
		time.Sleep(10 * time.Second) // wait for 10 seconds before checking the deployment group status again
		tflog.Debug(ctx, fmt.Sprintf("Checking deployment group status for deployment %s in stack %s, deploymet status sync attempt %d/%d", name, r.existingStack.Name, i+1, maxRetries))
		latestDeploymentRun, err := r.tfeUtil.ReadLatestDeploymentRun(
			r.existingStack.ID, name, r.httpClient, r.tfeConfig, r.tfeClient,
		)
		if err != nil || latestDeploymentRun == nil {
			migrationData.FailureReason = fmt.Sprintf(
				"Failed to read latest deployment run for deployment %s in stack %s after state import advance, err: %v",
				name, r.existingStack.Name, err,
			)
			tflog.Error(ctx, migrationData.FailureReason)
		} else {
			status := tfe.DeploymentGroupStatus(latestDeploymentRun.StackDeploymentGroup.Status)
			deploymentGroupId := latestDeploymentRun.StackDeploymentGroup.ID
			tflog.Debug(ctx, fmt.Sprintf("Deployment group %s for deployment %s in stack %s is in %s status after state import advance.", deploymentGroupId, name, r.existingStack.Name, status))
			migrationData.DeploymentGroupData.Status = status
			switch status {
			case tfe.DeploymentGroupStatusSucceeded:
				tflog.Info(ctx, fmt.Sprintf(
					"Deployment group %s in stack %s is in %s status after state import advance.",
					name, r.existingStack.Name, status,
				))

			case tfe.DeploymentGroupStatusFailed, tfe.DeploymentGroupStatusAbandoned:
				migrationData.FailureReason = fmt.Sprintf(
					"Deployment group %s in stack %s is in %s status after state import advance. Please check your deployment config or workspace state data and retry.",
					name, r.existingStack.Name, status,
				)
				tflog.Error(ctx, migrationData.FailureReason)

			}
			// If a terminal status is reached, break the loop
			if status == tfe.DeploymentGroupStatusSucceeded ||
				status == tfe.DeploymentGroupStatusFailed ||
				status == tfe.DeploymentGroupStatusAbandoned {
				break
			}
		}
	}
}

func (r *stackMigrationResource) reFetchImportStateStepAndValidate(ctx context.Context, migrationData *StackMigrationData, importStateStepId string, deploymentName string) bool {
	importStateStep, err := r.tfeUtil.ReadStepById(importStateStepId, r.tfeClient)
	if err != nil {
		errorMessage := fmt.Sprintf("Error reading import-state step by id for stack: %s, deployment: %s, error: %v", r.existingStack.ID, deploymentName, err)
		tflog.Error(ctx, errorMessage)
		migrationData.FailureReason = errorMessage
		return false
	}
	if importStateStep.Status != "pending_operator" {
		errorMessage := fmt.Sprintf("Import-state step for deployment %s is in unexpected state: %s after state upload", deploymentName, importStateStep.Status)
		tflog.Error(ctx, errorMessage)
		migrationData.FailureReason = errorMessage
		return false
	}
	return true
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
	var foundAllowImport bool
	for _, step := range deploymentRunSteps {
		operationType := step.Attributes.OperationType
		if operationType == "allow-import" {
			allowImportStep = &step
			foundAllowImport = true
			break
		}
	}

	tflog.Debug(ctx, fmt.Sprintf("Found allow-import step: %+v for deployment: %s", allowImportStep, deploymentName))
	if !foundAllowImport {
		errorMessage := fmt.Sprintf("No allow-import step found for deployment: %s", deploymentName)
		tflog.Error(ctx, errorMessage)
		migrationData.FailureReason = errorMessage
		return nil, false, false
	}

	switch allowImportStep.Attributes.Status {
	case "pending_operator":
		return allowImportStep, true, true
	case "completed":
		return allowImportStep, true, false
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
		if !r.retryAbandonedDeployments {
			errorMessage := fmt.Sprintf("Deployment group %s for deployment %s is in abandoned state, please fix the dployment config in the stack configuration files and reupload to trigger the process, retry=%t", deploymentGroupId, deploymentName, r.retryAbandonedDeployments)
			tflog.Error(ctx, errorMessage)
			migrationData.FailureReason = errorMessage
			return false
		} else {
			tflog.Warn(ctx, fmt.Sprintf("Deployment group %s for deployment %s is in abandoned state, rerunning the deployment-group, retry=%t", deploymentGroupId, deploymentName, r.retryAbandonedDeployments))
			if err := r.tfeUtil.RerunDeploymentGroup(migrationData.DeploymentGroupData.Id, []string{deploymentName}, r.tfeClient); err != nil {
				errorMessage := fmt.Sprintf("Error rerunning deployment group %s for deployment %s, error: %v", migrationData.DeploymentGroupData.Id, deploymentName, err)
				tflog.Error(ctx, errorMessage)
				migrationData.FailureReason = errorMessage
				return false
			}
		}
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
