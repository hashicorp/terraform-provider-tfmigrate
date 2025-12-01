package tfe

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	cliErrs "terraform-provider-tfmigrate/internal/cli_errors"
	stackConstants "terraform-provider-tfmigrate/internal/constants/stack"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"

	"terraform-provider-tfmigrate/internal/models"
	httpUtil "terraform-provider-tfmigrate/internal/util/net"

	mapset "github.com/deckarep/golang-set/v2"
	httpHeaders "github.com/go-http-utils/headers"
	"github.com/hashicorp/go-slug"
	"github.com/hashicorp/go-tfe"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/tidwall/gjson"
)

var (
	configConvergenceTimeout = 5 * time.Minute // configConvergenceTimeout defines the maximum time to wait for a stack configuration to converge.
	// stackPlanPollInterval    = 5 * time.Second // stackPlanPollInterval defines the interval at which to poll for stack plans.
	// emptyPollCountThreshold = 6 // emptyPollCountThreshold defines the number of consecutive empty results before considering the stack plan polling as terminal.
)

const (
	StatusAwaiting StackPlanPollStatus = iota
	StatusAPIError
	StatusTimeout
	StatusStackPlanInProgress
	StatusStackPlanIsTerminal
)

type StackPlanPollStatus int

type PollResult struct {
	Status StackPlanPollStatus
	Err    error
}

// TfeUtil defines the interface for TFE utility functions.
type TfeUtil interface {
	NewClient(config *tfe.Config) (*tfe.Client, error)
	AdvanceDeploymentRunStep(stepId string, client *tfe.Client) error
	CalculateConfigFileHash(stackConfigFileAbsPath string) (string, error)
	// HandleConvergingStatus(currentConfigurationId string, client *tfe.Client) string
	GetAllDeploymentNamesForAConfigId(stackId string, stackConfigurationId string, httpClient httpUtil.Client, config *tfe.Config) (mapset.Set[string], error)
	GetDeploymentGroupSummaryByConfigID(stackConfigurationId string, client *tfe.Client) (*tfe.StackDeploymentGroupSummaryList, error)
	PullAndSaveWorkspaceStateData(organizationName string, workspaceName string, client *tfe.Client) (string, error)
	ReadDeploymentRunSteps(deploymentRunId string, httpClient httpUtil.Client, tfeConfig *tfe.Config) ([]models.StackDeploymentStep, error)
	ReadLatestDeploymentRun(stackId string, deploymentName string, httpClient httpUtil.Client, config *tfe.Config, tfeClient *tfe.Client) (*tfe.StackDeploymentRun, error)
	ReadOrgByName(organizationName string, client *tfe.Client) (*tfe.Organization, error)
	ReadProjectByName(organizationName, projectName string, client *tfe.Client) (*tfe.Project, error)
	ReadStackByName(organizationName, projectId string, stackName string, client *tfe.Client) (*tfe.Stack, error)
	ReadStackDiagnosticsByConfigID(stackConfigId string, httpClient httpUtil.Client, config *tfe.Config) diag.Diagnostics
	ReadStepById(stepId string, client *tfe.Client) (*tfe.StackDeploymentStep, error)
	ReadTfeToken(tfeRemoteHostName string) (string, error)
	ReadWorkspaceByName(organizationName, workspaceName string, client *tfe.Client) (*tfe.Workspace, error)
	RerunDeploymentGroup(stackDeploymentGroupId string, deploymentNames []string, client *tfe.Client) error
	StackConfigurationHasRunningDeploymentGroups(stackConfigurationId string, client *tfe.Client) (bool, error)
	// StackConfigurationHasRunningPlan(stackConfigurationId string, client *tfe.Client) (bool, error)
	UpdateContext(ctx context.Context)
	UploadStackConfigFile(stackId string, configFileDirAbsPath string, client *tfe.Client) (string, error)
	WatchStackConfigurationUntilTerminalStatus(stackConfigurationId string, client *tfe.Client) (tfe.StackConfigurationStatus, diag.Diagnostics)
}

type tfeUtil struct {
	convergenceTimeOut time.Duration   // convergenceTimeOut defines the maximum time to wait for a stack configuration to converge.
	ctx                context.Context // ctx is the context for TFE operations, allowing for cancellation and timeouts.
}

type stackPageResult struct {
	page int
	data []models.StackDeploymentData
	err  error
}

// NewTfeUtil creates a new instance of TfeUtil.
func NewTfeUtil(ctx context.Context) TfeUtil {
	return &tfeUtil{
		convergenceTimeOut: configConvergenceTimeout,
		ctx:                ctx,
	}
}

// NewClient creates a new TFE client with the provided configuration.
func (u *tfeUtil) NewClient(config *tfe.Config) (*tfe.Client, error) {
	client, err := tfe.NewClient(config)
	if err != nil {
		tflog.Error(context.Background(), fmt.Sprintf("Error creating TFE client: %v", err))
		return nil, fmt.Errorf("error creating TFE client: %v", err)
	}
	return client, nil
}

func (u *tfeUtil) AdvanceDeploymentRunStep(stepId string, client *tfe.Client) error {
	tflog.Debug(u.ctx, fmt.Sprintf("Advancing deployment run step with ID %s", stepId))
	// Advance the deployment run step
	err := client.StackDeploymentSteps.Advance(u.ctx, stepId)
	if err != nil {
		tflog.Error(u.ctx, fmt.Sprintf("Error advancing deployment run step with ID %s: %v", stepId, err))
		return fmt.Errorf("error advancing deployment run step with ID %s, err: %v", stepId, err)
	}
	tflog.Info(u.ctx, fmt.Sprintf("Successfully advanced deployment run step with ID %s", stepId))
	return nil
}

// CalculateConfigFileHash calculates the bundle hash for a stack/terraform source configuration file.
func (u *tfeUtil) CalculateConfigFileHash(stackConfigFileAbsPath string) (string, error) {

	// Although some of the sanity checks below are redundant, they are included to ensure that the
	// stack configuration file is valid and meets the expected criteria before proceeding with the upload.

	// check if the file exists
	var fileInfo fs.FileInfo
	var err error
	if fileInfo, err = os.Stat(stackConfigFileAbsPath); errors.Is(err, fs.ErrNotExist) {
		tflog.Error(u.ctx, fmt.Sprintf("Stack configuration file does not exist at path: %s", stackConfigFileAbsPath))
		return "", fmt.Errorf("stack configuration file does not exist at path: %s", stackConfigFileAbsPath)
	}

	if err != nil {
		tflog.Error(u.ctx, fmt.Sprintf("Error checking stack configuration file at path %s: %v", stackConfigFileAbsPath, err))
		return "", fmt.Errorf("error checking stack configuration file at path %s: %v", stackConfigFileAbsPath, err)
	}

	// check if a file is a directory
	if !fileInfo.IsDir() {
		tflog.Error(u.ctx, fmt.Sprintf("Stack configuration file is not a directory at path: %s", stackConfigFileAbsPath))
		return "", fmt.Errorf("stack configuration file is not a directory at path: %s", stackConfigFileAbsPath)
	}

	// check if a file path is an absolute path
	if !filepath.IsAbs(stackConfigFileAbsPath) {
		tflog.Error(u.ctx, fmt.Sprintf("Stack configuration file path is not an absolute path: %s", stackConfigFileAbsPath))
		return "", fmt.Errorf("stack configuration file path is not an absolute path: %s", stackConfigFileAbsPath)
	}

	buf := bytes.NewBuffer(nil)

	_, err = slug.Pack(stackConfigFileAbsPath, buf, true)
	if err != nil {
		tflog.Error(u.ctx, fmt.Sprintf("Error packing stack configuration file at path %s: %v", stackConfigFileAbsPath, err))
		return "", fmt.Errorf("error packing stack configuration file at path %s: err: %v", stackConfigFileAbsPath, err)
	}

	// Calculate the bundle hash using the slug package
	hash := sha256.Sum256(buf.Bytes())
	return fmt.Sprintf("%x", hash[:]), nil
}

// GetAllDeploymentNamesForAConfigId retrieves all deployment names associated with a specific stack configuration ID.
// It fetches data using pagination and supports concurrent retrieval of page results for improved performance.
// The method returns a set of unique deployment names or an error if fetching the data fails.
func (u *tfeUtil) GetAllDeploymentNamesForAConfigId(stackId string, stackConfigurationId string, httpClient httpUtil.Client, config *tfe.Config) (mapset.Set[string], error) {
	tflog.Debug(u.ctx, fmt.Sprintf("Getting all deployment names for stack configuration ID %s", stackConfigurationId))

	deploymentNames := mapset.NewSet[string]()
	deploymentsByConfigUrl := fmt.Sprintf(stackConstants.StackDploymentsByConfigIdApiPathTemplate, config.Address, config.BasePath, stackId, stackConfigurationId)

	// 1) Fetch the first page to get pagination info default page size = 20
	firstPage, err := u.fetchStackDeploymentsPage(deploymentsByConfigUrl, config.Token, 1, 20, httpClient)
	if err != nil || firstPage == nil {
		tflog.Error(u.ctx, fmt.Sprintf("Error fetching first page of stack deployments for configuration ID %s: %v", stackConfigurationId, err))
		return nil, fmt.Errorf("error fetching first page: %w", err)
	}

	if len(firstPage.Data) == 0 {
		return deploymentNames, nil // Return empty set if no deployments found
	}

	totalPages := firstPage.Meta.TotalPages
	pageSize := firstPage.Meta.PageSize

	// Prepare an aggregator with data from the first page
	var allDeployments []models.StackDeploymentData
	allDeployments = append(allDeployments, firstPage.Data...)

	// If only one page, we're done
	if totalPages <= 1 {
		for _, deployment := range allDeployments {
			deploymentNames.Add(deployment.Attributes.Name)
		}
		return deploymentNames, nil
	}

	// 2) Concurrently fetch the remaining pages [2..totalPages]
	results := make(chan stackPageResult, totalPages-1)

	var wg sync.WaitGroup
	for p := 2; p <= totalPages; p++ {
		wg.Add(1)
		p := p // capture loop variable
		go func(page int) {
			defer wg.Done()
			pageData, err := u.fetchStackDeploymentsPage(deploymentsByConfigUrl, config.Token, page, pageSize, httpClient)
			if err != nil {
				results <- stackPageResult{page: page, err: err}
				return
			}
			results <- stackPageResult{page: page, data: pageData.Data}
		}(p)
	}

	// Close the result channel when workers finish
	go func() {
		wg.Wait()
		close(results)
	}()

	// 3) Collect results from all pages
	moreDeployments, err := u.collectStackDeploymentResults(results, totalPages)
	if err != nil {
		tflog.Error(u.ctx, fmt.Sprintf("Error fetching stack deployments for configuration ID %s: %v", stackConfigurationId, err))
		return nil, err
	}
	allDeployments = append(allDeployments, moreDeployments...)

	// Extract deployment names
	for _, deployment := range allDeployments {
		deploymentNames.Add(deployment.Attributes.Name)
	}

	return deploymentNames, nil
}

func (u *tfeUtil) GetDeploymentGroupSummaryByConfigID(stackConfigurationId string,
	client *tfe.Client) (*tfe.StackDeploymentGroupSummaryList, error) {
	tflog.Debug(u.ctx, fmt.Sprintf("Getting deployment group summary for stack configuration ID %s", stackConfigurationId))
	// TODO: Currently only 20 deployments are supported meaning that this will have 20 deployment groups at most.
	//  We need to handle pagination if the number of deployment groups exceeds 20.
	deploymentGroupSummaryList, err := client.StackDeploymentGroupSummaries.List(u.ctx, stackConfigurationId, nil)
	if err != nil {
		tflog.Error(u.ctx, fmt.Sprintf("Error getting deployment group summary for stack configuration ID %s: %v", stackConfigurationId, err))
		return nil, fmt.Errorf("error getting deployment group summary for stack configuration ID %s: %v", stackConfigurationId, err)
	}

	return deploymentGroupSummaryList, nil
}

// HandleConvergingStatus handles the converging status of a stack configuration.
// func (u *tfeUtil) HandleConvergingStatus(currentConfigurationId string, client *tfe.Client) string {
//	tflog.Debug(u.ctx, fmt.Sprintf("Handling converging status for stack configuration ID: %s", currentConfigurationId))
//	ctx, cancel := context.WithTimeout(u.ctx, configConvergenceTimeout)
//	defer cancel()
//	resultChan := pollConvergingConfigurationsForRunningStackPlans(ctx, currentConfigurationId, client)
//	for result := range resultChan {
//		if ctx.Err() != nil {
//			tflog.Warn(u.ctx, fmt.Sprintf("Polling cancelled or timed out for stack configuration %s: %v", currentConfigurationId, ctx.Err()))
//			break
//		}
//
//		if result.Err != nil {
//			tflog.Error(u.ctx, fmt.Sprintf("Error polling stack plans for stack configuration %s: %v", currentConfigurationId, result.Err))
//			break
//		}
//
//		if result.Status == StatusStackPlanInProgress || result.Status == StatusAwaiting {
//			tflog.Info(u.ctx, fmt.Sprintf("The current stack configuration %s is still in progress or awaiting approval, check ui", currentConfigurationId))
//			tflog.Debug(u.ctx, fmt.Sprintf("Continuing to poll stack plans for stack configuration %s", currentConfigurationId))
//			continue
//		}
//	}
//
//	// get the current stack configuration
//	stackConfiguration, err := client.StackConfigurations.Read(u.ctx, currentConfigurationId)
//	if err != nil || stackConfiguration == nil {
//		tflog.Error(u.ctx, fmt.Sprintf("Error reading stack configuration %s, err: %v", currentConfigurationId, err))
//		return ""
//	}
//
//	return stackConfiguration.Status
//}

// PullAndSaveWorkspaceStateData pulls the state data from a workspace and saves it to a local file.
func (u *tfeUtil) PullAndSaveWorkspaceStateData(organizationName string, workspaceName string, client *tfe.Client) (string, error) {
	tflog.Debug(u.ctx, fmt.Sprintf("Read workspace %s in organization %s", workspaceName, organizationName))
	workspace, err := u.ReadWorkspaceByName(organizationName, workspaceName, client)
	if err != nil {
		tflog.Error(u.ctx, fmt.Sprintf("Error reading workspace %s in organization %s: %v", workspaceName, organizationName, err))
		return "", err
	}

	if workspace == nil {
		tflog.Error(u.ctx, fmt.Sprintf("Workspace %s in organization %s not found", workspaceName, organizationName))
		return "", fmt.Errorf("workspace %s in organization %s not found", workspaceName, organizationName)
	}

	defer func() {
		// Unlock the workspace
		if _, err := client.Workspaces.Unlock(u.ctx, workspace.ID); err != nil {
			tflog.Error(u.ctx, "Failed to unlock workspace")
		}
	}()

	tflog.Debug(u.ctx, fmt.Sprintf("Locking workspace %s in organization %s", workspaceName, organizationName))
	reason := "Preparing to pull workspace state to be saved locally in the temp directory"
	lockOptions := tfe.WorkspaceLockOptions{Reason: &reason}
	if _, err := client.Workspaces.Lock(u.ctx, workspace.ID, lockOptions); err != nil {
		return "", fmt.Errorf("error locking workspace %s in organization %s, err: %v", workspaceName, organizationName, err)
	}

	tflog.Debug(u.ctx, fmt.Sprintf("Reading current state version for workspace %s in organization %s", workspaceName, organizationName))
	currentStateVersion, err := client.StateVersions.ReadCurrent(u.ctx, workspace.ID)
	if err != nil {
		tflog.Error(u.ctx, fmt.Sprintf("Error reading current state version for workspace %s in organization %s err: %v", workspaceName, organizationName, err))
	}

	if currentStateVersion == nil || currentStateVersion.DownloadURL == "" {
		tflog.Error(u.ctx, fmt.Sprintf("No current state version found for workspace %s in organization %s", workspaceName, organizationName))
		return "", fmt.Errorf("no current state version found for workspace %s in organization %s", workspaceName, organizationName)
	}

	tflog.Debug(u.ctx, fmt.Sprintf("Downloading state file for workspace %s in organization %s", workspaceName, organizationName))
	rawStateData, err := client.StateVersions.Download(u.ctx, currentStateVersion.DownloadURL)
	if err != nil {
		tflog.Error(u.ctx, fmt.Sprintf("Error downloading state file for workspace %s in organization %s err: %v", workspaceName, organizationName, err))
		return "", fmt.Errorf("error downloading state file for workspace %s in organization %s, err: %v", workspaceName, organizationName, err)
	}

	if len(rawStateData) == 0 {
		tflog.Error(u.ctx, fmt.Sprintf("Downloaded state file is empty for workspace %s in organization %s", workspaceName, organizationName))
		return "", fmt.Errorf("downloaded state file is empty for workspace %s in organization %s", workspaceName, organizationName)
	}

	// Save the state data to a local file in the temp directory
	tempDir := os.TempDir()
	tempFile, err := os.CreateTemp(tempDir, fmt.Sprintf("%s-*.tfstate", workspaceName))
	if err != nil {
		tflog.Error(u.ctx, fmt.Sprintf("Error creating temp file for workspace %s in organization %s err: %v", workspaceName, organizationName, err))
		return "", fmt.Errorf("error creating temp file for workspace %s in organization %s, err: %v", workspaceName, organizationName, err)
	}
	defer tempFile.Close()
	if _, err := tempFile.Write(rawStateData); err != nil {
		tflog.Error(u.ctx, fmt.Sprintf("Error writing state data to temp file for workspace %s in organization %s err: %v", workspaceName, organizationName, err))
		return "", fmt.Errorf("error writing state data to temp file for workspace %s in organization %s, err: %v", workspaceName, organizationName, err)
	}

	tflog.Info(u.ctx, fmt.Sprintf("Successfully pulled and saved state data for workspace %s in organization %s to file %s", workspaceName, organizationName, tempFile.Name()))
	return tempFile.Name(), nil
}

// ReadDeploymentRunSteps retrieves the steps of a deployment run by its ID.
func (u *tfeUtil) ReadDeploymentRunSteps(deploymentRunId string, httpClient httpUtil.Client, config *tfe.Config) ([]models.StackDeploymentStep, error) {
	tflog.Debug(u.ctx, fmt.Sprintf("Reading deployment run steps for deployment run ID %s", deploymentRunId))
	deploymentRunStepsUrl := fmt.Sprintf(stackConstants.StackDeploymentStepApiPathTemplate, config.Address, config.BasePath, deploymentRunId)
	bearerToken := fmt.Sprintf("Bearer %s", config.Token)
	response, err := httpClient.Do(httpUtil.RequestOptions{
		Method: http.MethodGet,
		URL:    deploymentRunStepsUrl,
		Headers: map[string]string{
			httpHeaders.Authorization: bearerToken,
			httpHeaders.Accept:        "application/vnd.api+json",
		},
	})
	if err != nil {
		tflog.Error(u.ctx, fmt.Sprintf("Error reading deployment run steps for deployment run ID %s: %v", deploymentRunId, err))
		return nil, fmt.Errorf("error reading deployment run steps for deployment run ID %s, err: %v", deploymentRunId, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		tflog.Error(u.ctx, fmt.Sprintf("Error reading deployment run steps for deployment run ID %s: received status code %d", deploymentRunId, response.StatusCode))
		err = u.handleHttpClientError(response)
		return nil, fmt.Errorf("error reading deployment run steps for deployment run ID %s, err: %v", deploymentRunId, err)
	}
	var deploymentRunSteps models.StackDeploymentSteps
	if err := json.NewDecoder(response.Body).Decode(&deploymentRunSteps); err != nil {
		tflog.Error(u.ctx, fmt.Sprintf("Error decoding response for deployment run steps for deployment run ID %s: %v", deploymentRunId, err))
		return nil, fmt.Errorf("error decoding response for deployment run steps for deployment run ID %s, err: %v", deploymentRunId, err)
	}
	if len(deploymentRunSteps.Data) == 0 {
		tflog.Warn(u.ctx, fmt.Sprintf("No deployment run steps found for deployment run ID %s", deploymentRunId))
		return nil, nil // Return nil if no steps are found
	}

	return deploymentRunSteps.Data, nil
}

// ReadLatestDeploymentRun retrieves the latest deployment run for a stack by its ID and deployment name.
func (u *tfeUtil) ReadLatestDeploymentRun(stackId string, deploymentName string, httpClient httpUtil.Client, config *tfe.Config, tfeClient *tfe.Client) (*tfe.StackDeploymentRun, error) {
	tflog.Debug(u.ctx, fmt.Sprintf("Reading latest deployment run for stack ID %s and deployment name %s", stackId, deploymentName))
	deploymentRunUrl := fmt.Sprintf(stackConstants.StackDeploymentRunApiPathTemplate, config.Address, config.BasePath, stackId, deploymentName)
	bearerToken := fmt.Sprintf("Bearer %s", config.Token)
	response, err := httpClient.Do(httpUtil.RequestOptions{
		Method: http.MethodGet,
		URL:    deploymentRunUrl,
		Headers: map[string]string{
			httpHeaders.Authorization: bearerToken,
			httpHeaders.Accept:        "application/vnd.api+json",
		}})
	if err != nil {
		tflog.Error(u.ctx, fmt.Sprintf("Error reading latest deployment run for stack ID %s and deployment name %s: %v", stackId, deploymentName, err))
		return nil, fmt.Errorf("error reading latest deployment run for stack ID %s and deployment name %s, err: %v", stackId, deploymentName, err)
	}

	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		tflog.Error(u.ctx, fmt.Sprintf("Error reading latest deployment run for stack ID %s and deployment name %s: received status code %d", stackId, deploymentName, response.StatusCode))
		err = u.handleHttpClientError(response)
		return nil, fmt.Errorf("error reading latest deployment run for stack ID %s and deployment name %s, err: %v", stackId, deploymentName, err)
	}

	var deploymentRuns models.StackDeploymentRuns
	if err := json.NewDecoder(response.Body).Decode(&deploymentRuns); err != nil {
		tflog.Error(u.ctx, fmt.Sprintf("Error decoding response for latest deployment run for stack ID %s and deployment name %s: %v", stackId, deploymentName, err))
		return nil, fmt.Errorf("error decoding response for latest deployment run for stack ID %s and deployment name %s, err: %v", stackId, deploymentName, err)
	}

	if len(deploymentRuns.Data) == 0 {
		tflog.Warn(u.ctx, fmt.Sprintf("No deployment runs found for stack ID %s and deployment name %s", stackId, deploymentName))
		return &tfe.StackDeploymentRun{}, nil
	}

	// Assuming the latest deployment run is the first one in the list
	latestDeploymentRun := deploymentRuns.Data[0]

	deploymentGroupId := latestDeploymentRun.Relationships.StackDeploymentGroup.Data.Id

	// Read the stack deployment group to get the latest deployment groupId for the stack deployment run
	stackDeploymentGroup, err := tfeClient.StackDeploymentGroups.Read(u.ctx, deploymentGroupId)
	if err != nil {
		tflog.Error(u.ctx, fmt.Sprintf("Error reading stack deployment group for deployment group ID %s: %v", deploymentGroupId, err))
		err = u.handleTfeClientResourceReadError(err)
		return nil, fmt.Errorf("error reading stack deployment group for deployment group ID %s, err: %v", deploymentGroupId, err)
	}

	stackDeploymentRun := tfe.StackDeploymentRun{
		ID:                   latestDeploymentRun.Id,
		Status:               latestDeploymentRun.Attributes.Status,
		StackDeploymentGroup: stackDeploymentGroup,
	}
	return &stackDeploymentRun, nil
}

// ReadOrgByName retrieves the organization by its name.
func (u *tfeUtil) ReadOrgByName(organizationName string, client *tfe.Client) (*tfe.Organization, error) {
	org, err := client.Organizations.Read(u.ctx, organizationName)
	if err != nil {
		tflog.Error(u.ctx, fmt.Sprintf("Error reading organization %s: %v", organizationName, err))
		err = u.handleTfeClientResourceReadError(err)
		return nil, fmt.Errorf("error reading organization %s, err: %v", organizationName, err)
	}

	if org == nil {
		tflog.Error(u.ctx, fmt.Sprintf("Organization %s not found", organizationName))
		return nil, fmt.Errorf("organization %s not found", organizationName)
	}

	tflog.Debug(u.ctx, fmt.Sprintf("Successfully retrieved organization %s with ID %s", organizationName, org.ExternalID))
	return org, nil

}

// ReadProjectByName retrieves the project by its name within a specific organization.
func (u *tfeUtil) ReadProjectByName(organizationName, projectName string, client *tfe.Client) (*tfe.Project, error) {
	projects, err := client.Projects.List(u.ctx, organizationName, &tfe.ProjectListOptions{
		Name: projectName,
	})
	if err != nil {
		tflog.Error(u.ctx, fmt.Sprintf("Error listing projects in organization %s: %v", organizationName, err))
		err = u.handleTfeClientResourceReadError(err)
		return nil, fmt.Errorf("error listing projects in organization %s, err: %v", organizationName, err)
	}

	if len(projects.Items) == 0 {
		tflog.Error(u.ctx, fmt.Sprintf("Project %s not found in organization %s", projectName, organizationName))
		return nil, fmt.Errorf("project %s not found in organization %s", projectName, organizationName)
	}

	project := projects.Items[0]
	tflog.Debug(u.ctx, fmt.Sprintf("Successfully retrieved project %s with ID %s in organization %s", project.Name, project.ID, organizationName))
	return project, nil
}

// ReadStackByName retrieves the stack by its name within a specific organization and project.
func (u *tfeUtil) ReadStackByName(organizationName, projectId string, stackName string, client *tfe.Client) (*tfe.Stack, error) {

	tflog.Debug(u.ctx, fmt.Sprintf("Listing stacks in organization %s with project ID %s and stack name %s", organizationName, projectId, stackName))
	stacks, err := client.Stacks.List(u.ctx, organizationName, &tfe.StackListOptions{
		ProjectID:    projectId,
		SearchByName: stackName,
	})
	if err != nil {
		tflog.Error(u.ctx, fmt.Sprintf("Error listing stacks in organization %s: %v", organizationName, err))
		err = u.handleTfeClientResourceReadError(err)
		return nil, fmt.Errorf("error listing stacks in organization %s, err: %v", organizationName, err)
	}

	if len(stacks.Items) == 0 {
		tflog.Error(u.ctx, fmt.Sprintf("Stack %s not found in organization %s", stackName, organizationName))
		return nil, fmt.Errorf("stack %s not found in organization %s", stackName, organizationName)
	}

	stack := stacks.Items[0]
	tflog.Debug(u.ctx, fmt.Sprintf("Successfully retrieved stack %s with ID %s in organization %s", stack.Name, stack.ID, organizationName))

	// Read the latest stack configuration for the stack
	tflog.Debug(u.ctx, fmt.Sprintf("Reading latest stack configuration for stack %s with ID %s", stack.Name, stack.ID))
	stackConfigurationOpts := &tfe.StackConfigurationListOptions{
		ListOptions: tfe.ListOptions{
			PageNumber: 1,
			PageSize:   1,
		},
	}
	stackConfigurations, err := client.StackConfigurations.List(u.ctx, stack.ID, stackConfigurationOpts)
	if err != nil || stackConfigurations == nil { // We expect exactly one result in the lis and that one item should be the latest configuration, as the API returns them in descending order.
		tflog.Error(u.ctx, fmt.Sprintf("Error listing stack configurations for stack %s: %v", stack.Name, err))
		err = u.handleTfeClientResourceReadError(err)
		return nil, fmt.Errorf("error listing stack configurations for stack %s, err: %v", stack.Name, err)
	}

	if len(stackConfigurations.Items) > 0 {
		mostRecentConfig := stackConfigurations.Items[0]
		stack.LatestStackConfiguration = mostRecentConfig
		tflog.Debug(u.ctx, fmt.Sprintf("Successfully retrieved latest stack configuration %s for stack %s", mostRecentConfig.ID, stack.Name))
	} else {
		tflog.Warn(u.ctx, fmt.Sprintf("No stack configurations found for stack %s with ID: %s", stack.Name, stack.ID))
	}

	return stack, nil
}

// ReadStepById fetches a StackDeploymentStep by its ID using the provided TFE client and context.
// Returns the StackDeploymentStep or an error if the step cannot be retrieved.
func (u *tfeUtil) ReadStepById(stepId string, client *tfe.Client) (*tfe.StackDeploymentStep, error) {
	step, err := client.StackDeploymentSteps.Read(u.ctx, stepId)
	if err != nil || step == nil {
		tflog.Error(u.ctx, fmt.Sprintf("Error reading deployment run step with ID %s: %v", stepId, err))
		err = u.handleTfeClientResourceReadError(err)
		return nil, fmt.Errorf("error reading deployment run step with ID %s, err: %v", stepId, err)
	}

	tflog.Debug(u.ctx, fmt.Sprintf("Successfully retrieved deployment run step with ID %s", stepId))
	return step, nil
}

// ReadStackDiagnosticsByConfigID retrieves the diagnostics for a stack configuration.
func (u *tfeUtil) ReadStackDiagnosticsByConfigID(stackConfigId string, httpClient httpUtil.Client, config *tfe.Config) diag.Diagnostics {
	var diags diag.Diagnostics
	tflog.Debug(u.ctx, fmt.Sprintf("Reading stack diagnostics for stack configuration ID %s", stackConfigId))
	diagnosticsUrl := fmt.Sprintf(stackConstants.StackConfigDiagnosticsApiPathTemplate, config.Address, config.BasePath, stackConfigId)
	bearerToken := fmt.Sprintf("Bearer %s", config.Token)
	response, err := httpClient.Do(httpUtil.RequestOptions{
		Method: http.MethodGet,
		URL:    diagnosticsUrl,
		Headers: map[string]string{
			httpHeaders.Authorization: bearerToken,
			httpHeaders.Accept:        "application/vnd.api+json",
		},
	})

	if err != nil {
		tflog.Error(u.ctx, fmt.Sprintf("Error reading stack diagnostics for stack configuration ID %s: %v", stackConfigId, err))
		diags.AddError(
			"Error reading stack diagnostics",
			fmt.Sprintf("Error reading stack diagnostics for stack configuration ID %s, err: %v", stackConfigId, err),
		)
		return diags
	}

	if response.StatusCode != http.StatusOK {
		tflog.Error(u.ctx, fmt.Sprintf("Error reading stack diagnostics for stack configuration ID %s: received status code %d", stackConfigId, response.StatusCode))
		err = u.handleHttpClientError(response)
		diags.AddError(
			"Error reading stack diagnostics",
			fmt.Sprintf("Error reading stack diagnostics for stack configuration ID %s, err: %v", stackConfigId, err),
		)
		return diags
	}

	defer response.Body.Close()
	var diagnosticsData models.StackConfigDiagnostics
	if err := json.NewDecoder(response.Body).Decode(&diagnosticsData); err != nil || len(diagnosticsData.Data) == 0 {
		tflog.Error(u.ctx, fmt.Sprintf("Error decoding response for stack diagnostics for stack configuration ID %s: %v", stackConfigId, err))
		diags.AddError(
			"Error decoding stack diagnostics response",
			fmt.Sprintf("Error decoding response for stack diagnostics for stack configuration ID %s, err: %v", stackConfigId, err),
		)
		return diags
	}

	for _, diagData := range diagnosticsData.Data {
		diagnosticSummary := diagData.Attributes.Summary
		diagnosticDetails := getDetailsFromDiagnosticData(diagData)
		if diagData.Attributes.Severity == "warning" {
			diags.AddWarning(diagnosticSummary, diagnosticDetails)
		} else {
			diags.AddError(diagnosticSummary, diagnosticDetails)
		}
	}

	return diags
}

// ReadTfeToken reads the TFE token from the credential file.
func (u *tfeUtil) ReadTfeToken(tfeRemoteHostName string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("error getting user home directory: %v", err)
	}
	credsFile := strings.Join([]string{homeDir, ".terraform.d", "credentials.tfrc.json"}, string(os.PathSeparator)) //nolint:typecheck
	credsJson, err := os.ReadFile(credsFile)
	if err != nil {
		if os.IsNotExist(err) {
			tflog.Error(context.Background(), "Credentials file not found")
			return "", fmt.Errorf("credentials file not found at %s", credsFile)
		}
		return "", fmt.Errorf("error reading credentials file: %v", err)
	}

	var data interface{}
	if err := json.Unmarshal(credsJson, &data); err != nil {
		tflog.Error(context.Background(), fmt.Sprintf("Invalid JSON in credentials file: %v", err))
		return "", fmt.Errorf("invalid JSON in credentials file: %v", err)
	}
	tfcTokenJsonPath := fmt.Sprintf("credentials.%s.token", strings.ReplaceAll(tfeRemoteHostName, ".", "\\.")) //nolint:gocritic

	token := gjson.Get(string(credsJson), tfcTokenJsonPath).String()

	return token, nil
}

// ReadWorkspaceByName retrieves the workspace by its name within a specific organization.
func (u *tfeUtil) ReadWorkspaceByName(organizationName, workspaceName string, client *tfe.Client) (*tfe.Workspace, error) {
	tflog.Debug(u.ctx, fmt.Sprintf("Listing workspaces in organization %s with workspace name %s", organizationName, workspaceName))
	workspace, err := client.Workspaces.Read(u.ctx, organizationName, workspaceName)
	if err != nil || workspace == nil {
		tflog.Error(u.ctx, fmt.Sprintf("Error reading workspace %s in organization %s: %v", workspaceName, organizationName, err))
		err = u.handleTfeClientResourceReadError(err)
		return nil, fmt.Errorf("error listing workspaces in organization %s, err: %v", organizationName, err)
	}
	tflog.Debug(u.ctx, fmt.Sprintf("Successfully retrieved workspace %s with ID %s in organization %s", workspace.Name, workspace.ID, organizationName))
	return workspace, nil
}

// RerunDeploymentGroup triggers re-execution of deployments in a specified deployment group using the given stack ID.
func (u *tfeUtil) RerunDeploymentGroup(stackDeploymentGroupId string, deploymentNames []string, client *tfe.Client) error {
	err := client.StackDeploymentGroups.Rerun(u.ctx, stackDeploymentGroupId, &tfe.StackDeploymentGroupRerunOptions{Deployments: deploymentNames})
	if err != nil {
		tflog.Error(u.ctx, fmt.Sprintf("Error rerunning stack deployment group %s: %v", stackDeploymentGroupId, err))
		err = u.handleTfeClientResourceReadError(err)
		return fmt.Errorf("error rerunning stack deployment group %s, err: %v", stackDeploymentGroupId, err)
	}

	return nil
}

// StackConfigurationHasRunningDeploymentGroups checks if the stack configuration has any running deployment groups.
func (u *tfeUtil) StackConfigurationHasRunningDeploymentGroups(stackConfigurationId string, client *tfe.Client) (bool, error) {
	stackDeploymentGroups, err := client.StackDeploymentGroups.List(u.ctx, stackConfigurationId, nil)
	if err != nil {
		tflog.Error(u.ctx, fmt.Sprintf("Error listing stack deployment groups for stack configuration %s: %v", stackConfigurationId, err))
		return false, u.handleTfeClientResourceReadError(err)
	}

	// TODO: Currently only 20 deployments are supported meaning that this will have 20 deployment groups at most.
	//  We need to handle pagination if the number of deployment groups exceeds 20.
	for _, group := range stackDeploymentGroups.Items {
		deploymentGroupStatus := tfe.DeploymentGroupStatus(group.Status)
		if slices.Contains(stackConstants.RunningDeploymentGroupStatuses, deploymentGroupStatus) {
			tflog.Debug(u.ctx, fmt.Sprintf("Stack configuration %s has running deployment group %s with status %s", stackConfigurationId, group.ID, deploymentGroupStatus))
			return true, nil
		}
	}

	return false, nil
}

// StackConfigurationHasRunningPlan checks if the stack configuration has any running plans.
// func (u *tfeUtil) StackConfigurationHasRunningPlan(stackConfigurationId string, client *tfe.Client) (bool, error) {
//	stackPlanOpts := &tfe.StackPlansListOptions{
//		Status: tfe.StackPlansStatusFilterRunning,
//	}
//	stackPlans, err := client.StackPlans.ListByConfiguration(u.ctx, stackConfigurationId, stackPlanOpts)
//	if err != nil {
//		tflog.Error(u.ctx, fmt.Sprintf("Error listing stack plans for stack configuration %s: %v", stackConfigurationId, err))
//		return false, fmt.Errorf("error listing stack plans for stack configuration %s, err: %v", stackConfigurationId, err)
//	}
//
//	return len(stackPlans.Items) > 0, nil
//}

// UpdateContext updates the context for the TFE utility.
func (u *tfeUtil) UpdateContext(ctx context.Context) {
	if ctx != nil {
		u.ctx = ctx
	} else {
		u.ctx = context.Background()
	}
}

// UploadStackConfigFile uploads the stack configuration file to TFE and returns the stack configuration ID.
func (u *tfeUtil) UploadStackConfigFile(stackId string, configFileDirAbsPath string, client *tfe.Client) (string, error) {
	stackSource, err := client.StackConfigurations.CreateAndUpload(u.ctx, stackId, configFileDirAbsPath, &tfe.CreateStackConfigurationOptions{
		SpeculativeEnabled: tfe.Bool(false),
	})
	if err != nil || stackSource == nil {
		tflog.Error(u.ctx, fmt.Sprintf("Error getting stack source for stack configuration file upload for stack ID %s: %v", stackId, err))
		err = u.handleTfeClientResourceReadError(err)
		return "", fmt.Errorf("error getting stack source for stack configuration file upload for stack ID %s, err: %v", stackId, err)
	}

	// Read the stack to get the latest stack configuration ID after uploading the stack source.
	stackRead, err := client.Stacks.Read(u.ctx, stackId)
	if err != nil || stackRead == nil {
		tflog.Error(u.ctx, fmt.Sprintf("Error reading stack %s after uploading stack configuration file: %v", stackId, err))
		err = u.handleTfeClientResourceReadError(err)
		return "", fmt.Errorf("error reading stack %s after uploading stack configuration file, err: %v", stackId, err)
	}
	configurationListOptions := tfe.StackConfigurationListOptions{
		ListOptions: tfe.ListOptions{PageNumber: 1, PageSize: 1},
	}
	// List the stack configurations to get the latest one.
	stackConfigurationList, err := client.StackConfigurations.List(u.ctx, stackId, &configurationListOptions)
	if err != nil || stackConfigurationList == nil || len(stackConfigurationList.Items) == 0 {
		tflog.Error(u.ctx, fmt.Sprintf("Error listing stack configurations for stack ID %s after uploading stack configuration file: %v", stackId, err))
		err = u.handleTfeClientResourceReadError(err)
		return "", fmt.Errorf("error listing stack configurations for stack ID %s after uploading stack configuration file, err: %v", stackId, err)
	}

	stackRead.LatestStackConfiguration = stackConfigurationList.Items[0]
	tflog.Info(u.ctx, fmt.Sprintf("Successfully uploaded stack configuration file for stack ID %s, stack source ID %s, configuration %q", stackId, stackSource.ID, stackRead.LatestStackConfiguration.ID))
	return stackRead.LatestStackConfiguration.ID, nil
}

// WatchStackConfigurationUntilTerminalStatus waits for the stack configuration to reach a terminal state and returns its status.
func (u *tfeUtil) WatchStackConfigurationUntilTerminalStatus(stackConfigurationID string, client *tfe.Client) (tfe.StackConfigurationStatus, diag.Diagnostics) {
	var diags diag.Diagnostics
	var currentStatus string

	tflog.Info(u.ctx, "Starting to poll for terminal stack configuration status...")
	ctxWithTimeout, cancel := context.WithTimeout(u.ctx, u.convergenceTimeOut)
	defer cancel()

	statusCh := client.StackConfigurations.AwaitCompleted(ctxWithTimeout, stackConfigurationID)

	for result := range statusCh {
		if ctxWithTimeout.Err() != nil {
			// Timeout or cancellation
			tflog.Error(u.ctx, fmt.Sprintf("Polling cancelled or timed out for stack configuration %s: %v", stackConfigurationID, result.Error))
			diags.AddWarning("Polling timeout or cancelled", fmt.Sprintf("Polling stopped due to timeout or cancellation: %v", result.Error))
			break
		}

		if result.Error != nil {
			// API read or logic error
			tflog.Error(u.ctx, fmt.Sprintf("Polling error for stack configuration %s: %v", stackConfigurationID, result.Error))
			diags.AddError("Polling error", fmt.Sprintf("Polling error while watching stack configuration %s: %v", stackConfigurationID, result.Error))
			break
		}
		currentStatus, diags = u.handleCurrentConfigurationStatus(stackConfigurationID, tfe.StackConfigurationStatus(result.Status))

		// if a non-empty status is returned, we can break the loop
		if currentStatus != "" {
			tflog.Info(u.ctx, fmt.Sprintf("Stack configuration %s reached terminal status: %s", stackConfigurationID, currentStatus))
			return tfe.StackConfigurationStatus(currentStatus), diags
		}
	}

	// if after the loop we still have an empty status, we can assume that the stack configuration is still in progress or the channel was closed unexpectedly
	tflog.Warn(u.ctx, fmt.Sprintf("Stack configuration %s is still in progress or the channel was closed unexpectedly", stackConfigurationID))
	diags.AddWarning("Stack configuration still in progress", fmt.Sprintf("Stack configuration %s is still in progress or the channel was closed unexpectedly", stackConfigurationID))

	// make an additional read to get the current status
	stackConfiguration, err := client.StackConfigurations.Read(u.ctx, stackConfigurationID)
	if err != nil || stackConfiguration == nil {
		tflog.Error(u.ctx, fmt.Sprintf("Error reading stack configuration %s: %v", stackConfigurationID, err))
		err = u.handleTfeClientResourceReadError(err)
		diags.AddError("Stack configuration read error", fmt.Sprintf("Error reading stack configuration %s: %v", stackConfigurationID, err))
		return "", diags
	}

	return tfe.StackConfigurationStatus(stackConfiguration.Status), diags
}

// handleCurrentConfigurationStatus handles the current status of a stack configuration and returns a string representation of the status.
func (u *tfeUtil) handleCurrentConfigurationStatus(stackConfigurationID string, currentStatus tfe.StackConfigurationStatus) (string, diag.Diagnostics) {
	var diags diag.Diagnostics
	switch currentStatus {
	// case tfe.StackConfigurationStatusConverged:
	//	tflog.Info(u.ctx, fmt.Sprintf("Stack configuration %s has converged", stackConfigurationID))
	//	return currentStatus.String(), diags
	// case tfe.StackConfigurationStatusConverging:
	//	tflog.Debug(u.ctx, fmt.Sprintf("Stack configuration %s is converging", stackConfigurationID))
	//	return currentStatus.String(), diags
	case tfe.StackConfigurationStatusErrored:
		tflog.Error(u.ctx, fmt.Sprintf("Stack configuration %s errored", stackConfigurationID))
		diags.AddError("Stack configuration errored", fmt.Sprintf("Stack configuration %s entered error state", stackConfigurationID))
		return currentStatus.String(), diags
	case tfe.StackConfigurationStatusCanceled:
		tflog.Warn(u.ctx, fmt.Sprintf("Stack configuration %s was canceled", stackConfigurationID))
		diags.AddWarning("Stack configuration canceled", fmt.Sprintf("Stack configuration %s was canceled", stackConfigurationID))
		return currentStatus.String(), diags
	default:
		tflog.Debug(u.ctx, fmt.Sprintf("Stack configuration %s still in progress: %s", stackConfigurationID, currentStatus))
		return "", diags
	}
}

// handleHttpClientError handles common HTTP client errors when interacting with the TFE API.
func (u *tfeUtil) handleHttpClientError(resp *http.Response) error {
	if resp == nil {
		return cliErrs.ErrUnknownError
	}

	if resp.StatusCode == http.StatusUnauthorized {
		tflog.Error(u.ctx, "Unauthorized access to TFE API. Please check your TFE token and permissions.")
		return fmt.Errorf("you are not authorized to access this resource. please make sure your TFE token has permission to read this resource")
	}

	if resp.StatusCode == http.StatusForbidden {
		tflog.Error(u.ctx, "Forbidden access to TFE API. Please check your TFE token and permissions.")
		return fmt.Errorf("you do not have permission to access this resource. please make sure your TFE token has permission to read this resource")
	}

	if resp.StatusCode == http.StatusNotFound {
		tflog.Error(u.ctx, "Resource not found in TFE API. Please check the resource ID or name.")
		return fmt.Errorf("the requested resource was not found. please check the resource ID or name")
	}

	if resp.StatusCode >= http.StatusInternalServerError && resp.StatusCode < http.StatusNetworkAuthenticationRequired {
		tflog.Error(u.ctx, fmt.Sprintf("Server error occurred during API call with status code: %d", resp.StatusCode))
		return cliErrs.ErrServerError
	}

	return fmt.Errorf("received unexpected status code %d from TFE API. Please check the API documentation for more details", resp.StatusCode)
}

// handleTfeClientResourceReadError handles common errors when reading resources from TFE.
func (u *tfeUtil) handleTfeClientResourceReadError(err error) error {
	if err == nil {
		return nil // No error to handle
	}
	if errors.Is(err, tfe.ErrUnauthorized) {
		tflog.Error(u.ctx, "Unauthorized access to TFE API. Please check your token and permissions.")
		return fmt.Errorf("you are not authorized to access this resource: %w. please make sure your token has permissiont to read this resource", err)
	}

	if errors.Is(err, tfe.ErrResourceNotFound) {
		tflog.Error(u.ctx, "Resource not found in TFE API. Please check the resource ID or name.")
		return fmt.Errorf("the requested resource was not found: %w. please check the resource ID or name", err)
	}

	tflog.Error(u.ctx, fmt.Sprintf("An error occurred while reading the TFE resource: %v", err))
	return fmt.Errorf("an error occurred while reading the TFE resource: %w", err)
}

// pollConvergingConfigurationsForRunningStackPlans polls the TFE API to fetch running stack plans associated with a converging stack configuration.
// func pollConvergingConfigurationsForRunningStackPlans(ctx context.Context, convergingStackConfigurationId string, tfeClient *tfe.Client) <-chan PollResult { // nosonar
//	resultChan := make(chan PollResult)
//	stackPlanOpts := &tfe.StackPlansListOptions{
//		Status: tfe.StackPlansStatusFilterRunning,
//	}
//
//	go func() {
//		defer close(resultChan)
//
//		ticker := time.NewTicker(stackPlanPollInterval)
//		defer ticker.Stop()
//
//		consecutiveEmpty := 0
//
//		for {
//			select {
//			case <-ctx.Done():
//				resultChan <- PollResult{
//					Status: StatusTimeout,
//					Err:    fmt.Errorf("polling timed out or cancelled: %w", ctx.Err()),
//				}
//				return
//
//			case <-ticker.C:
//
//				stackPlans, err := tfeClient.StackPlans.ListByConfiguration(ctx, convergingStackConfigurationId, stackPlanOpts)
//				if err != nil {
//					// If there is an error while listing stack plans, we return an API error status.
//					resultChan <- PollResult{
//						Status: StatusAPIError,
//						Err:    fmt.Errorf("API error during polling: %w", err),
//					}
//					return
//				}
//
//				if len(stackPlans.Items) == 0 {
//					consecutiveEmpty++
//				} else {
//					consecutiveEmpty = 0
//				}
//
//				// If we have consecutive empty results for 3 attempts, we consider it a terminal state.
//				if consecutiveEmpty >= emptyPollCountThreshold {
//					resultChan <- PollResult{
//						Status: StatusStackPlanIsTerminal,
//						Err:    nil,
//					}
//					return
//				}
//
//				if len(stackPlans.Items) > 0 {
//					resultChan <- PollResult{
//						Status: StatusStackPlanInProgress,
//						Err:    nil,
//					}
//				} else {
//					// If there are no stack plans, we emmit an awaiting status 3 times before considering it a terminal state.
//					resultChan <- PollResult{
//						Status: StatusAwaiting,
//						Err:    nil,
//					}
//				}
//			}
//		}
//	}()
//
//	return resultChan
//
//}

func getDetailsFromDiagnosticData(diagnosticData models.StackDiagnostic) string {
	var details []string
	details = append(details, diagnosticData.Attributes.Detail)
	for _, d := range diagnosticData.Attributes.Diags {
		if d.Detail != "" {
			details = append(details, d.Detail)
		}
	}
	return strings.Join(details, "\n")
}

// fetchStackDeploymentsPage is a small helper to perform the HTTP GET for a given
// stack deployments page and decode the JSON into models.StackDeployments.
// If pageSize is 0, it will not be sent allowing server default.

func (u *tfeUtil) collectStackDeploymentResults(results <-chan stackPageResult, totalPages int) ([]models.StackDeploymentData, error) {
	// Collect and buffer page data as they arrive; preserve the first error
	pageBuffer := make(map[int][]models.StackDeploymentData, totalPages-1)
	var firstErr error
	for r := range results {
		if r.err != nil && firstErr == nil {
			firstErr = r.err
		}
		if r.data != nil {
			pageBuffer[r.page] = r.data
		}
	}

	if firstErr != nil {
		return nil, firstErr
	}

	// Aggregate in page order for stability
	var combined []models.StackDeploymentData
	for p := 2; p <= totalPages; p++ {
		if data, ok := pageBuffer[p]; ok {
			combined = append(combined, data...)
		}
	}
	return combined, nil
}

func (u *tfeUtil) fetchStackDeploymentsPage(url string, token string, pageNumber int, pageSize int, httpClient httpUtil.Client) (*models.StackDeployments, error) {

	// Prepare query parameters
	query := map[string]string{
		"page[number]": fmt.Sprintf("%d", pageNumber),
	}
	if pageSize > 0 {
		query["page[size]"] = fmt.Sprintf("%d", pageSize)
	}

	// Prepare headers
	headers := map[string]string{
		httpHeaders.Authorization: fmt.Sprintf("Bearer %s", token),
		httpHeaders.Accept:        "application/vnd.api+json",
	}

	// Perform the HTTP GET request
	resp, err := httpClient.Do(httpUtil.RequestOptions{
		Method:      http.MethodGet,
		URL:         url,
		Headers:     headers,
		QueryParams: query,
	})

	if err != nil {
		return nil, fmt.Errorf("request error: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code %d", resp.StatusCode)
	}

	var deployments models.StackDeployments
	if err := json.NewDecoder(resp.Body).Decode(&deployments); err != nil {
		return nil, fmt.Errorf("decode error: %w", err)
	}
	return &deployments, nil
}
