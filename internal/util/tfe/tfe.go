package tfe

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"

	"github.com/hashicorp/go-slug"
	"github.com/hashicorp/go-tfe"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/tidwall/gjson"
)

var (
	configConvergenceTimeout = 5 * time.Minute // configConvergenceTimeout defines the maximum time to wait for a stack configuration to converge.
	stackPlanPollInterval    = 5 * time.Second // stackPlanPollInterval defines the interval at which to poll for stack plans.
	emptyPollCountThreshold  = 6               // emptyPollCountThreshold defines the number of consecutive empty results before considering the stack plan polling as terminal.
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
	CalculateStackSourceBundleHash(stackConfigFileAbsPath string) (string, error)
	HandleConvergingStatus(currentConfigurationId string, client *tfe.Client) string
	NewClient(config *tfe.Config) (*tfe.Client, error)
	ReadOrgByName(organizationName string, client *tfe.Client) (*tfe.Organization, error)
	ReadProjectByName(organizationName, projectName string, client *tfe.Client) (*tfe.Project, error)
	ReadStackByName(organizationName, projectId string, stackName string, client *tfe.Client) (*tfe.Stack, error)
	ReadTfeToken(tfeRemoteHostName string) (string, error)
	StackConfigurationHasRunningPlan(stackConfigurationId string, client *tfe.Client) (bool, error)
	UpdateContext(ctx context.Context)
	UploadStackConfigFile(stackId string, configFileDirAbsPath string, client *tfe.Client) (string, error)
	WatchStackConfigurationUntilTerminalStatus(stackConfigurationId string, client *tfe.Client) (tfe.StackConfigurationStatus, diag.Diagnostics)
}

type tfeUtil struct {
	convergenceTimeOut time.Duration   // convergenceTimeOut defines the maximum time to wait for a stack configuration to converge.
	ctx                context.Context // ctx is the context for TFE operations, allowing for cancellation and timeouts.
}

// NewTfeUtil creates a new instance of TfeUtil.
func NewTfeUtil(ctx context.Context) TfeUtil {
	return &tfeUtil{
		convergenceTimeOut: configConvergenceTimeout,
		ctx:                ctx,
	}
}

// CalculateStackSourceBundleHash calculates the bundle hash for a stack source configuration file.
func (u *tfeUtil) CalculateStackSourceBundleHash(stackConfigFileAbsPath string) (string, error) {

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

// NewClient creates a new TFE client with the provided configuration.
func (u *tfeUtil) NewClient(config *tfe.Config) (*tfe.Client, error) {
	client, err := tfe.NewClient(config)
	if err != nil {
		tflog.Error(context.Background(), fmt.Sprintf("Error creating TFE client: %v", err))
		return nil, fmt.Errorf("error creating TFE client: %v", err)
	}
	return client, nil
}

// HandleConvergingStatus handles the converging status of a stack configuration.
func (u *tfeUtil) HandleConvergingStatus(currentConfigurationId string, client *tfe.Client) string {
	tflog.Debug(u.ctx, fmt.Sprintf("Handling converging status for stack configuration ID: %s", currentConfigurationId))
	ctx, cancel := context.WithTimeout(u.ctx, 5*time.Minute)
	defer cancel()
	resultChan := pollConvergingConfigurationsForRunningStackPlans(ctx, currentConfigurationId, client)
	for result := range resultChan {
		if ctx.Err() != nil {
			tflog.Warn(u.ctx, fmt.Sprintf("Polling cancelled or timed out for stack configuration %s: %v", currentConfigurationId, ctx.Err()))
			break
		}

		if result.Err != nil {
			tflog.Error(u.ctx, fmt.Sprintf("Error polling stack plans for stack configuration %s: %v", currentConfigurationId, result.Err))
			break
		}

		if result.Status == StatusStackPlanInProgress || result.Status == StatusAwaiting {
			tflog.Info(u.ctx, fmt.Sprintf("The current stack configuration %s is still in progress or awaiting approval, check ui", currentConfigurationId))
			tflog.Debug(u.ctx, fmt.Sprintf("Continuing to poll stack plans for stack configuration %s", currentConfigurationId))
			continue
		}
	}

	// get the current stack configuration
	stackConfiguration, err := client.StackConfigurations.Read(u.ctx, currentConfigurationId)
	if err != nil || stackConfiguration == nil {
		tflog.Error(u.ctx, fmt.Sprintf("Error reading stack configuration %s, err: %v", currentConfigurationId, err))
		return ""
	}

	return stackConfiguration.Status
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

	latestStackConfigId := ""
	if stack.LatestStackConfiguration == nil || stack.LatestStackConfiguration.ID == "" {
		tflog.Warn(u.ctx, fmt.Sprintf("Stack %s does not have a latest stack configuration", stack.Name))
		stack.LatestStackConfiguration = &tfe.StackConfiguration{}
		return stack, nil
	}

	latestStackConfigId = stack.LatestStackConfiguration.ID
	tflog.Debug(u.ctx, fmt.Sprintf("Latest stack configuration ID for stack %s is %s", stack.Name, latestStackConfigId))
	tflog.Debug(u.ctx, fmt.Sprintf(`Retrieved stack %s with ID %s, latest stack configuration ID %s`, stack.Name, stack.ID, latestStackConfigId))
	latestStackConfiguration, err := client.StackConfigurations.Read(u.ctx, latestStackConfigId)
	if err != nil || latestStackConfiguration == nil {
		tflog.Error(u.ctx, fmt.Sprintf("Error reading latest stack configuration %s for stack %s: %v", latestStackConfigId, stack.Name, err))
		err = u.handleTfeClientResourceReadError(err)
		return nil, fmt.Errorf("error reading latest stack configuration %s for stack %s, err: %v", latestStackConfigId, stack.Name, err)
	}

	tflog.Debug(u.ctx, fmt.Sprintf("Successfully retrieved latest stack configuration %s for stack %s", latestStackConfiguration.ID, stack.Name))
	stack.LatestStackConfiguration = latestStackConfiguration

	return stack, nil
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

// StackConfigurationHasRunningPlan checks if the stack configuration has any running plans.
func (u *tfeUtil) StackConfigurationHasRunningPlan(stackConfigurationId string, client *tfe.Client) (bool, error) {
	stackPlanOpts := &tfe.StackPlansListOptions{
		Status: tfe.StackPlansStatusFilterRunning,
	}
	stackPlans, err := client.StackPlans.ListByConfiguration(u.ctx, stackConfigurationId, stackPlanOpts)
	if err != nil {
		tflog.Error(u.ctx, fmt.Sprintf("Error listing stack plans for stack configuration %s: %v", stackConfigurationId, err))
		return false, fmt.Errorf("error listing stack plans for stack configuration %s, err: %v", stackConfigurationId, err)
	}

	return len(stackPlans.Items) > 0, nil
}

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
	stackSource, err := client.StackSources.CreateAndUpload(u.ctx, stackId, configFileDirAbsPath, &tfe.CreateStackSourceOptions{
		SpeculativeEnabled: tfe.Bool(false),
	})
	if err != nil || stackSource == nil {
		tflog.Error(u.ctx, fmt.Sprintf("Error getting stack source for stack configuration file upload for stack ID %s: %v", stackId, err))
		return "", fmt.Errorf("error getting stack source for stack configuration file upload for stack ID %s, err: %v", stackId, err)
	}

	// Read the stack to get the latest stack configuration ID after uploading the stack source.
	stackRead, err := client.Stacks.Read(u.ctx, stackId, nil)
	if err != nil || stackRead == nil || stackRead.LatestStackConfiguration == nil {
		tflog.Error(u.ctx, fmt.Sprintf("Error reading stack %s after uploading stack configuration file: %v", stackId, err))
		return "", fmt.Errorf("error reading stack %s after uploading stack configuration file, err: %v", stackId, err)
	}
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
		diags.AddError("Stack configuration read error", fmt.Sprintf("Error reading stack configuration %s: %v", stackConfigurationID, err))
		return "", diags
	}

	return tfe.StackConfigurationStatus(stackConfiguration.Status), diags
}

func (u *tfeUtil) handleCurrentConfigurationStatus(stackConfigurationID string, currentStatus tfe.StackConfigurationStatus) (string, diag.Diagnostics) {
	var diags diag.Diagnostics
	switch currentStatus {
	case tfe.StackConfigurationStatusConverged:
		tflog.Info(u.ctx, fmt.Sprintf("Stack configuration %s has converged", stackConfigurationID))
		return currentStatus.String(), diags
	case tfe.StackConfigurationStatusConverging:
		tflog.Debug(u.ctx, fmt.Sprintf("Stack configuration %s is converging", stackConfigurationID))
		return currentStatus.String(), diags
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

// handleTfeClientResourceReadError handles common errors when reading resources from TFE.
func (u *tfeUtil) handleTfeClientResourceReadError(err error) error {
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
func pollConvergingConfigurationsForRunningStackPlans(ctx context.Context, convergingStackConfigurationId string, tfeClient *tfe.Client) <-chan PollResult { // nosonar
	resultChan := make(chan PollResult)
	stackPlanOpts := &tfe.StackPlansListOptions{
		Status: tfe.StackPlansStatusFilterRunning,
	}

	go func() {
		defer close(resultChan)

		ticker := time.NewTicker(stackPlanPollInterval)
		defer ticker.Stop()

		consecutiveEmpty := 0

		for {
			select {
			case <-ctx.Done():
				resultChan <- PollResult{
					Status: StatusTimeout,
					Err:    fmt.Errorf("polling timed out or cancelled: %w", ctx.Err()),
				}
				return

			case <-ticker.C:

				stackPlans, err := tfeClient.StackPlans.ListByConfiguration(ctx, convergingStackConfigurationId, stackPlanOpts)
				if err != nil {
					// If there is an error while listing stack plans, we return an API error status.
					resultChan <- PollResult{
						Status: StatusAPIError,
						Err:    fmt.Errorf("API error during polling: %w", err),
					}
					return
				}

				if len(stackPlans.Items) == 0 {
					consecutiveEmpty++
				} else {
					consecutiveEmpty = 0
				}

				// If we have consecutive empty results for 3 attempts, we consider it a terminal state.
				if consecutiveEmpty >= emptyPollCountThreshold {
					resultChan <- PollResult{
						Status: StatusStackPlanIsTerminal,
						Err:    nil,
					}
					return
				}

				if len(stackPlans.Items) > 0 {
					resultChan <- PollResult{
						Status: StatusStackPlanInProgress,
						Err:    nil,
					}
				} else {
					// If there are no stack plans, we emmit an awaiting status 3 times before considering it a terminal state.
					resultChan <- PollResult{
						Status: StatusAwaiting,
						Err:    nil,
					}
				}
			}
		}
	}()

	return resultChan

}
