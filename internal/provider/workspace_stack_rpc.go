package provider

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/go-tfe"
	"github.com/hashicorp/terraform-migrate-utility/rpcapi"
	"github.com/hashicorp/terraform-migrate-utility/rpcapi/terraform1/stacks"
	_ "github.com/hashicorp/terraform-migrate-utility/rpcapi/tfstackdata1"
	"github.com/hashicorp/terraform-migrate-utility/rpcapi/tfstacksagent1"
	stateOps "github.com/hashicorp/terraform-migrate-utility/stateops"
	_ "github.com/hashicorp/terraform-migrate-utility/tfstateutil"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

func (r *stackMigrationResource) convertWorkspaceStateAndUpload(ctx context.Context, workspaceId string, uploadUrl string) diag.Diagnostics {
	var diags diag.Diagnostics

	// get the current working directory
	currentWorkingDir, err := os.Getwd()
	if err != nil {
		diags.AddError("Failed to get current working directory", fmt.Sprintf("Error getting current working directory: %v", err))
		return diags
	}

	tflog.Info(ctx, fmt.Sprintf("Current working directory: %s", currentWorkingDir))
	// get the raw Terraform state data from the TFE workspace
	tflog.Info(ctx, fmt.Sprintf("Downloading workspace state: %s", workspaceId))

	rawTerraformState, rawStateDiags := r.getRawStateData(ctx, workspaceId)
	if diags.HasError() {
		diags.Append(rawStateDiags...)
	}

	if rawTerraformState == nil || len(rawTerraformState) == 0 {
		diags.AddError("No state data found", fmt.Sprintf("Workspace %s has no state data to convert", workspaceId))
		return diags
	}

	tflog.Info(ctx, fmt.Sprintf("Downloaded workspace state: %s", workspaceId))

	// start the RPC client
	client, err := rpcapi.NewTerraformRpcClient(ctx)
	if err != nil {
		diags.AddError("Failed to create RPC client", fmt.Sprintf("Error creating RPC client: %v", err))
		return diags
	}
	defer client.Stop()

	stateOpsHandler := stateOps.NewTFStateOperations(ctx, client)
	stateConversionRequest := stateOps.WorkspaceToStackStateConversionRequest{
		Ctx:                         ctx,
		Client:                      client,
		CurrentWorkingDir:           currentWorkingDir,
		RawStateData:                rawTerraformState,
		StateOpsHandler:             stateOpsHandler,
		StackSourceBundleAbsPath:    r.stackSourceBundleAbsPath,
		TerraformConfigFilesAbsPath: r.terraformConfigDirAbsPath,
	}

	// If the state is fully modular, use the module address map, otherwise use the absolute resource address map
	if r.isStateModular {
		stateConversionRequest.ModuleAddressMap = r.workspaceToStackMap
	} else {
		stateConversionRequest.AbsoluteResourceAddressMap = r.workspaceToStackMap
	}

	stackState, err := convertTerraformWorkspaceToStackData(stateConversionRequest)
	if err != nil {
		diags.AddError("Failed to convert workspace state to stack state", fmt.Sprintf("Error converting workspace state: %v", err))
	}

	// Marshal to protobuf binary
	data, err := proto.Marshal(stackState)
	if err != nil {
		diags.AddError("Failed to marshal stack state", fmt.Sprintf("Error marshaling stack state: %v", err))
		return diags
	}

	// Write the data to a temporary file
	tempFilePath, err := writeFileToTempDir(data, workspaceId)
	if err != nil {
		diags.AddError("Failed to write stack state to temporary file", fmt.Sprintf("Error writing stack state to temporary file: %v", err))
		return diags
	}

	defer func() {
		// Clean up the temporary file after upload
		if err := os.Remove(tempFilePath); err != nil {
			tflog.Error(ctx, fmt.Sprintf("Failed to remove temporary file %s: %v", tempFilePath, err))
		}
	}()

	// Upload the stack state file to the TFE workspace
	if uploadDiags := r.uploadStackStateFile(ctx, tempFilePath, uploadUrl); uploadDiags.HasError() {
		diags.Append(uploadDiags...)
	}
	tflog.Info(ctx, fmt.Sprintf("Uploaded stack state file for workspace %s", workspaceId))

	return diags

}

func (r *stackMigrationResource) getRawStateData(ctx context.Context, workspaceId string) ([]byte, diag.Diagnostics) {
	var diags diag.Diagnostics

	defer func() {
		// Unlock the workspace
		if _, err := r.tfeClient.Workspaces.Unlock(ctx, workspaceId); err != nil {
			tflog.Error(ctx, "Failed to unlock workspace")
		}
	}()

	reason := "Preparing to convert workspace state to stack state"
	lockOptions := tfe.WorkspaceLockOptions{Reason: &reason}
	if _, err := r.tfeClient.Workspaces.Lock(ctx, workspaceId, lockOptions); err != nil {
		diags.AddError("Failed to lock workspace", fmt.Sprintf("Error locking workspace %s: %v", workspaceId, err))
		return nil, diags
	}
	tflog.Info(ctx, fmt.Sprintf("Locking workspace %s", workspaceId))

	currentStateVersion, err := r.tfeClient.StateVersions.ReadCurrent(ctx, workspaceId)
	if err != nil {
		diags.AddError("Failed to read current state version", fmt.Sprintf("Error reading current state version for workspace %s: %v", workspaceId, err))
		return nil, diags
	}
	if currentStateVersion == nil {
		diags.AddError("No current state version found", fmt.Sprintf("Workspace %s has no current state version", workspaceId))
		return nil, diags
	}
	tflog.Info(ctx, fmt.Sprintf("Current state version ID for workspace %s: %s", workspaceId, currentStateVersion.ID))

	rawStateData, err := r.tfeClient.StateVersions.Download(ctx, currentStateVersion.DownloadURL)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Failed to download state version for workspace %s: %v", workspaceId, err))
	}
	tflog.Info(ctx, fmt.Sprintf("Downloaded state version for workspace %s", workspaceId))

	return rawStateData, diags
}

func writeFileToTempDir(data []byte, workspaceId string) (string, error) {

	// Build a file path in the system temp dir
	tempDir := os.TempDir()
	fileName := workspaceId + "_stack_state.tfstackstate"
	fullPath := filepath.Join(tempDir, fileName)

	// Write the data to the file
	err := os.WriteFile(fullPath, data, os.ModePerm)
	if err != nil {
		return "", fmt.Errorf("failed to write file to temp dir: %w", err)
	}

	return fullPath, nil
}

func convertTerraformWorkspaceToStackData(stateConversionRequest stateOps.WorkspaceToStackStateConversionRequest) (*tfstacksagent1.StackState, error) { // NOSONAR
	// Ensure the RPC client is stopped after the conversion is done
	defer stateConversionRequest.Client.Stop()

	// Get the path for the stack modules cache directory
	stackModuleCacheDir := filepath.Join(stateConversionRequest.StackSourceBundleAbsPath, ".terraform/modules/")

	// Get the path for the Terraform provider cache directory
	terraformProviderCachePath := filepath.Join(stateConversionRequest.TerraformConfigFilesAbsPath, ".terraform/providers")

	// Get the relative path for the stack modules directory
	stackConfigRelativePath, err := filepath.Rel(stateConversionRequest.CurrentWorkingDir, stateConversionRequest.StackSourceBundleAbsPath)
	if err != nil {
		return nil, fmt.Errorf("error getting relative path for stack modules: %w", err)
	} else {
		if stackConfigRelativePath == "." {
			stackConfigRelativePath = "./"
		} else if !strings.HasPrefix(stackConfigRelativePath, "../") {
			stackConfigRelativePath = "./" + stackConfigRelativePath
		}
	}

	// Get the relative path for the Terraform dependency lock file
	terraformDependencyLockRelativePath, err := filepath.Rel(stateConversionRequest.CurrentWorkingDir, filepath.Join(stateConversionRequest.TerraformConfigFilesAbsPath, ".terraform.lock.hcl"))
	if err != nil {
		return nil, fmt.Errorf("error getting relative path for dependency lock file: %w", err)
	} else {
		if terraformDependencyLockRelativePath == "." {
			terraformDependencyLockRelativePath = "./"
		} else if !strings.HasPrefix(terraformDependencyLockRelativePath, "../") {
			terraformDependencyLockRelativePath = "./" + terraformDependencyLockRelativePath
		}
	}

	// Open raw Terraform state
	terraformStateHandle, closeTFState, err := stateConversionRequest.StateOpsHandler.OpenTerraformStateRaw(stateConversionRequest.RawStateData)
	if err != nil {
		return nil, fmt.Errorf("error opening Terraform state: %w", err)
	}
	defer func() {
		if err := closeTFState(); err != nil {
			tflog.Error(stateConversionRequest.Ctx, fmt.Sprintf("Failed to close Terraform state: %v", err))
		}
	}()

	// Open stack source bundle modules cache directory
	sourceBundleHandle, closeSourceBundle, err := stateConversionRequest.StateOpsHandler.OpenSourceBundle(stackModuleCacheDir)
	if err != nil {
		return nil, fmt.Errorf("error opening source bundle: %w", err)
	}
	defer func() {
		if err := closeSourceBundle(); err != nil {
			tflog.Error(stateConversionRequest.Ctx, fmt.Sprintf("Failed to close source bundle: %v", err))
		}
	}()

	// Open stack configuration
	// This is the relative path from the current working directory to the stack configuration directory
	stackConfigHandle, closeStackConfig, err := stateConversionRequest.StateOpsHandler.OpenStacksConfiguration(sourceBundleHandle, stackConfigRelativePath)
	if err != nil {
		return nil, fmt.Errorf("error opening stack configuration: %w", err)
	}
	defer func() {
		if err := closeStackConfig(); err != nil {
			tflog.Error(stateConversionRequest.Ctx, fmt.Sprintf("Failed to close stack configuration: %v", err))
		}
	}()

	// Open the Terraform dependency lock file
	// This is the relative path from the current working directory to the lock file
	dependencyLocksHandle, closeDependencyLocks, err := stateConversionRequest.StateOpsHandler.OpenDependencyLockFile(sourceBundleHandle, terraformDependencyLockRelativePath)
	if err != nil {
		return nil, fmt.Errorf("error opening dependency lock file: %w", err)
	}
	defer func() {
		if err := closeDependencyLocks(); err != nil {
			tflog.Error(stateConversionRequest.Ctx, fmt.Sprintf("Failed to close dependency lock file: %v", err))
		}
	}()

	// Open the Terraform provider cache directory
	providerCacheHandle, closeProviderCache, err := stateConversionRequest.StateOpsHandler.OpenProviderCache(terraformProviderCachePath)
	if err != nil {
		return nil, fmt.Errorf("error opening provider cache: %w", err)
	}
	defer func() {
		if err := closeProviderCache(); err != nil {
			tflog.Error(stateConversionRequest.Ctx, fmt.Sprintf("Failed to close provider cache: %v", err))
		}
	}()

	// Perform the migration of the Terraform state to stack state
	// using the absolute resource address map or module address map
	if stateConversionRequest.AbsoluteResourceAddressMap == nil && stateConversionRequest.ModuleAddressMap == nil {
		return nil, fmt.Errorf("no resource or module address map provided for migration")
	}

	events, err := stateConversionRequest.StateOpsHandler.MigrateTFState(
		terraformStateHandle,
		stackConfigHandle,
		dependencyLocksHandle,
		providerCacheHandle,
		stateConversionRequest.AbsoluteResourceAddressMap,
		stateConversionRequest.ModuleAddressMap,
	)
	if err != nil {
		return nil, fmt.Errorf("error migrating Terraform state: %w", err)
	}

	stackState := &tfstacksagent1.StackState{
		FormatVersion: 1,
		Raw:           make(map[string]*anypb.Any),
		Descriptions:  make(map[string]*stacks.AppliedChange_ChangeDescription),
	}

	for {
		item, err := events.Recv()
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("error receiving event: %w", err)
		}

		// Handle different event types
		switch result := item.Result.(type) {
		case *stacks.MigrateTerraformState_Event_AppliedChange:
			for _, raw := range result.AppliedChange.Raw {
				stackState.Raw[raw.Key] = raw.Value
			}

			for _, change := range result.AppliedChange.Descriptions {
				stackState.Descriptions[change.Key] = change
			}
		case *stacks.MigrateTerraformState_Event_Diagnostic:
			return nil, fmt.Errorf("diagnostic: %T", result.Diagnostic.Detail)
		default:
			return nil, fmt.Errorf("received event: %T", result)
		}
	}

	return stackState, nil
}

func (r *stackMigrationResource) uploadStackStateFile(ctx context.Context, filePath string, uploadUrl string) diag.Diagnostics {
	var diags diag.Diagnostics
	// Open the file
	file, err := os.Open(filePath)
	if err != nil {
		diags.AddError("Failed to open stack state file", fmt.Sprintf("Error opening stack state file %s: %v", filePath, err))
		return diags
	}
	defer file.Close()

	if err := r.tfeClient.StackSources.UploadTarGzip(ctx, uploadUrl, file); err != nil {
		diags.AddError("Failed to upload stack state file", fmt.Sprintf("Error uploading stack state file to %s: %v", uploadUrl, err))
		return diags
	}

	return diags
}
