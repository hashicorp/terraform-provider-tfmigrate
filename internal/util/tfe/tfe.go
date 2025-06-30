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

	"github.com/hashicorp/go-slug"
	"github.com/hashicorp/go-tfe"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/tidwall/gjson"
)

type TfeUtil interface {
	AwaitStackConfigurationCompletion(stackConfigurationId string, client *tfe.Client) (tfe.StackConfigurationStatus, error)
	CalculateStackSourceBundleHash(stackConfigFileAbsPath string) (string, error)
	NewClient(config *tfe.Config) (*tfe.Client, error)
	ReadProjectByName(organizationName, projectName string, client *tfe.Client) (*tfe.Project, error)
	ReadStackByName(organizationName, projectId string, stackName string, client *tfe.Client) (*tfe.Stack, error)
	ReadTfeToken(tfeRemoteHostName string) (string, error)
	RedTfeOrgByName(organizationName string, client *tfe.Client) (*tfe.Organization, error)
	UploadStackConfigFile(stackId string, configFileDirAbsPath string, client *tfe.Client) (string, error)
}

type tfeUtil struct {
	ctx context.Context
}

// NewTfeUtil creates a new instance of TfeUtil.
func NewTfeUtil(ctx context.Context) TfeUtil {
	return &tfeUtil{
		ctx: ctx,
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

// ReadTfeToken from terraform login credentials.
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

// RedTfeOrgByName retrieves the organization ID by its name.
func (u *tfeUtil) RedTfeOrgByName(organizationName string, client *tfe.Client) (*tfe.Organization, error) {
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
	stacks, err := client.Stacks.List(u.ctx, organizationName, &tfe.StackListOptions{
		ProjectID:    projectId,
		SearchByName: stackName,
		Include:      []tfe.StackIncludeOpt{tfe.StackIncludeLatestStackConfiguration, tfe.StackIncludeStackDiagnostics},
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
	return stack, nil
}

// UploadStackConfigFile uploads a stack configuration file to the specified stack.
func (u *tfeUtil) UploadStackConfigFile(stackId string, configFileDirAbsPath string, client *tfe.Client) (string, error) {
	stackSource, err := client.StackSources.CreateAndUpload(u.ctx, stackId, configFileDirAbsPath, &tfe.CreateStackSourceOptions{
		SpeculativeEnabled: tfe.Bool(false),
	})
	if err != nil {
		tflog.Error(u.ctx, fmt.Sprintf("Error getting stack source for stack configuration file upload for stack ID %s: %v", stackId, err))
		return "", fmt.Errorf("error getting stack source for stack configuration file upload for stack ID %s, err: %v", stackId, err)
	}

	tflog.Debug(u.ctx, fmt.Sprintf("Succssfully retrieved stack source for stack configuration file upload for stack ID %s with source ID %s", stackId, stackSource.ID))
	tflog.Info(u.ctx, fmt.Sprintf("Successfully uploaded stack configuration file for stack ID %s, current configuration id %s", stackId, stackSource.StackConfiguration.ID))

	awaitCompleted := client.StackConfigurations.AwaitCompleted(u.ctx, stackSource.StackConfiguration.ID)
	for result := range awaitCompleted {
		result.Status = stackSource.StackConfiguration.Status

	}

	return stackSource.StackConfiguration.ID, nil
}

// - Error while waiting for the stack configuration to complete.
func (u *tfeUtil) AwaitStackConfigurationCompletion(stackConfigurationId string, client *tfe.Client) (tfe.StackConfigurationStatus, error) {

	// Read the current stack configuration status
	stackConfiguration, err := client.StackConfigurations.Read(u.ctx, stackConfigurationId)
	if err != nil {
		tflog.Error(u.ctx, fmt.Sprintf("Error reading stack configuration %s: %v", stackConfigurationId, err))
		err = u.handleTfeClientResourceReadError(err)
		return tfe.StackConfigurationStatusPending, fmt.Errorf("error reading stack configuration %s, err: %v", stackConfigurationId, err)
	}

	if stackConfiguration == nil {
		tflog.Error(u.ctx, fmt.Sprintf("Stack configuration %s not found", stackConfigurationId))
		return tfe.StackConfigurationStatusPending, fmt.Errorf("stack configuration %s not found", stackConfigurationId)
	}

	mostRecentConfigurationStatus := tfe.StackConfigurationStatus(stackConfiguration.Status)
	tflog.Info(u.ctx, fmt.Sprintf("Successfully retrieved stack configuration %s with current status %s", stackConfigurationId, mostRecentConfigurationStatus))
	tflog.Debug(u.ctx, "Beginning to poll for stack configuration completion status...")

	ctxWithTimeout, cancel := context.WithTimeout(u.ctx, 5*time.Minute)
	defer cancel()
	statusCh := client.StackConfigurations.AwaitCompleted(ctxWithTimeout, stackConfigurationId)

	for {
		select {
		case <-ctxWithTimeout.Done():
			tflog.Error(u.ctx, fmt.Sprintf("Timeout or cancellation while waiting for stack configuration %s to complete, err: %v", stackConfigurationId, ctxWithTimeout.Err()))
			return mostRecentConfigurationStatus, fmt.Errorf("timeout or cancellation while waiting for stack configuration %s to complete, err: %v", stackConfigurationId, ctxWithTimeout.Err())

		case result, ok := <-statusCh:
			if !ok {
				tflog.Error(u.ctx, fmt.Sprintf("Channel closed while waiting for stack configuration %s to complete", stackConfigurationId))
				return mostRecentConfigurationStatus, fmt.Errorf("channel closed while waiting for stack configuration %s to complete", stackConfigurationId)
			}

			if result.Error != nil {
				tflog.Error(u.ctx, fmt.Sprintf("Error while waiting for stack configuration %s to complete: %v", stackConfigurationId, result.Error))
				return mostRecentConfigurationStatus, fmt.Errorf("error while waiting for stack configuration %s to complete: %v", stackConfigurationId, result.Error)
			}

			// Fixme: Need to revisit this logic to handle all possible statuses when
			//        we do workspace state to stack state conversion and upload.

			/*
				Note:
				      At the time of this implementation, importing the state of a deployment within a stack configuration
				      is not supported.

				      Therefore, this implementation assumes that once the stack configuration file is uploaded, the stack
				      will eventually reach one of the following terminal states:
				        - "converged"
				        - "errored"
				        - "canceled"

				      The "converging" state is not currently handled, but support for it can be added in the future.
				      When the stack is in an intermediate or transitional state such as "pending", "preparing",
				      "enqueueing", or "queued", the system will continue polling for up to 5 minutes (or until the
				      context is canceled) to monitor the transition to a terminal state ("converged", "errored", or
				      "canceled").

				      This polling behavior assumes that the user will take any necessary action — such as approving or
				      canceling the rollout of the deployments — within 5 minutes of uploading the stack configuration.
				      This expectation is critical to ensure the timely completion of migration process.

			*/

			switch tfe.StackConfigurationStatus(result.Status) {
			// case tfe.StackConfigurationStatusConverging:
			//	tflog.Info(u.ctx, fmt.Sprintf("Stack configuration %s is converging", stackConfigurationId))
			case tfe.StackConfigurationStatusConverged:
				tflog.Info(u.ctx, fmt.Sprintf("Stack configuration %s has converged successfully", stackConfigurationId))
				return tfe.StackConfigurationStatusConverged, nil
			case tfe.StackConfigurationStatusErrored:
				tflog.Error(u.ctx, fmt.Sprintf("Stack configuration %s has errored, err %v", stackConfigurationId, result.Error))
				return tfe.StackConfigurationStatusErrored, fmt.Errorf("stack configuration %s has errored", stackConfigurationId)
			case tfe.StackConfigurationStatusCanceled:
				tflog.Warn(u.ctx, fmt.Sprintf("Stack configuration %s has been canceled", stackConfigurationId))
				return tfe.StackConfigurationStatusCanceled, fmt.Errorf("stack configuration %s has been canceled", stackConfigurationId)
			default:
				tflog.Debug(u.ctx, fmt.Sprintf("Stack configuration %s is still in progress with status %s ...", stackConfigurationId, result.Status))
				mostRecentConfigurationStatus = tfe.StackConfigurationStatus(result.Status)
				continue
			}
		}
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
