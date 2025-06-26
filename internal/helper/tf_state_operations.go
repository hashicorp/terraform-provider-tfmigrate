// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0.

package gitops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"terraform-provider-tfmigrate/internal/constants"
	"terraform-provider-tfmigrate/internal/diagnostics"
	"terraform-provider-tfmigrate/internal/format"
	"terraform-provider-tfmigrate/internal/terraform/rpcapi"
	"terraform-provider-tfmigrate/internal/terraform/rpcapi/terraform1"
	"terraform-provider-tfmigrate/internal/terraform/rpcapi/terraform1/dependencies"
	"terraform-provider-tfmigrate/internal/terraform/rpcapi/terraform1/stacks"
	"time"

	sev "github.com/hashicorp/terraform/tfdiags"
)

// Order of execution:
// 1. StartTFRPCServer
// 2. OpenTerraformState
// 3. OpenSourceBundle
// 4. OpenStacksConfiguration
// 5. OpenDependencyLockFile
// 6. OpenProviderCache
// 7. MigrateTFState
// close open handles and stop the RPC server

type TFStateOperations interface {
	OpenSourceBundle(dotTFModulesPath string) (int64, diagnostics.Diagnostics, func(format.View))
	OpenStacksConfiguration(sourceBundleHandle int64, stackConfigPath string) (int64, diagnostics.Diagnostics, func(format.View))
	OpenDependencyLockFile(handle int64, dotTFLockFile string) (int64, diagnostics.Diagnostics, func(format.View))
	OpenProviderCache(dotTFProvidersPath string) (int64, diagnostics.Diagnostics, func(format.View))
	OpenTerraformState(tfStateFileDir string) (int64, diagnostics.Diagnostics, func(format.View))
	MigrateTFState(tfStateHandle int64, stackConfigHandle int64, dependencyLocksHandle int64, providerCacheHandle int64, resources map[string]string, modules map[string]string) (stacks.Stacks_MigrateTerraformStateClient, error)
	StartTFRPCServer() (StopTFRPCAPIServer func(), err error)
}

type tfStateOperations struct {
	ctx    context.Context
	client rpcapi.Client
}

func NewTFStateOperations(ctx context.Context, client rpcapi.Client) TFStateOperations {
	return &tfStateOperations{
		ctx:    ctx,
		client: client,
	}
}

// OpenSourceBundle opens a source bundle from the given path and returns a handle to it.
// dotTFModulesPath is the path to - ".terraform/modules/"
func (tf *tfStateOperations) OpenSourceBundle(dotTFModulesPath string) (int64, diagnostics.Diagnostics, func(format.View)) {
	response, err := tf.client.Dependencies().OpenSourceBundle(tf.ctx, &dependencies.OpenSourceBundle_Request{
		LocalPath: dotTFModulesPath,
	})
	if err != nil {
		var diags diagnostics.Diagnostics
		diags = diags.Append(diagnostics.Sourceless(
			sev.Error,
			"Failed to open source bundle",
			"Error returned by Terraform API: %s.", err.Error()))
		return -1, diags, nil
	}

	return response.SourceBundleHandle, nil, func(view format.View) {
		_, err := tf.client.Dependencies().CloseSourceBundle(context.Background(),
			&dependencies.CloseSourceBundle_Request{
				SourceBundleHandle: response.SourceBundleHandle,
			})
		if err != nil {
			// Since we opened the connection successfully, we should be able to
			// close it.
			var diags diagnostics.Diagnostics
			diags = diags.Append(diagnostics.Sourceless(sev.Error, "Failed to close source bundle handle", "The Terraform RPC API failed to close the configuration source bundle: %s.\n\nThis is a bug in the program, please report it!", err))
			view.Diagnostics(diags)
		}
	}
}

// OpenStacksConfiguration opens a stack configuration from the given path and returns a handle to it.
// stackConfigPath is the path to the directory where stack configs files are stored ie "_stacks_generated/*"
func (tf *tfStateOperations) OpenStacksConfiguration(sourceBundleHandle int64, stackConfigPath string) (int64, diagnostics.Diagnostics, func(format.View)) {
	response, err := tf.client.Stacks().OpenStackConfiguration(tf.ctx,
		&stacks.OpenStackConfiguration_Request{
			SourceBundleHandle: sourceBundleHandle,
			SourceAddress: &terraform1.SourceAddress{
				Source: stackConfigPath,
			},
		})
	if err != nil {
		var diags diagnostics.Diagnostics
		diags = diags.Append(diagnostics.Sourceless(
			sev.Error,
			"Failed to open configuration",
			"Error returned by Terraform API: %s.", err.Error()))
		return -1, diags, nil
	}

	var diags diagnostics.Diagnostics
	diags = diags.Append(response.Diagnostics)
	return response.StackConfigHandle, diags, func(view format.View) {
		_, err := tf.client.Stacks().CloseStackConfiguration(tf.ctx,
			&stacks.CloseStackConfiguration_Request{
				StackConfigHandle: response.StackConfigHandle,
			})
		if err != nil {
			// Since we opened the connection successfully, we should be able to
			// close it.
			var diags diagnostics.Diagnostics
			diags = diags.Append(diagnostics.Sourceless(sev.Error, "Failed to close stacks configuration handle", "The Terraform RPC API failed to close the stacks configuration: %s.\n\nThis is a bug in the program, please report it!", err))
			view.Diagnostics(diags)
		}
	}
}

// OpenDependencyLockFile opens a dependency lock file from the given path and returns a handle to it.
// dotTFLockFile is the path to the lock file - "./.terraform.lock.hcl"
func (tf *tfStateOperations) OpenDependencyLockFile(handle int64, dotTFLockFile string) (int64, diagnostics.Diagnostics, func(format.View)) {
	response, err := tf.client.Dependencies().OpenDependencyLockFile(tf.ctx, &dependencies.OpenDependencyLockFile_Request{
		SourceBundleHandle: handle,
		SourceAddress: &terraform1.SourceAddress{
			Source: dotTFLockFile,
		},
	})
	if err != nil {
		var diags diagnostics.Diagnostics
		diags = diags.Append(diagnostics.Sourceless(sev.Error, "Failed to open lock file", "This could be because `tfstacks providers lock` wasn't executed. The Terraform API returned the following error: %s.", err.Error()))
		return -1, diags, func(_ format.View) {}
	}

	var diags diagnostics.Diagnostics
	diags = diags.Append(response.Diagnostics)
	return response.DependencyLocksHandle, diags, func(view format.View) {
		_, err := tf.client.Dependencies().CloseDependencyLocks(context.Background(),
			&dependencies.CloseDependencyLocks_Request{
				DependencyLocksHandle: response.DependencyLocksHandle,
			})
		if err != nil {
			// Since we opened the connection successfully, we should be able to
			// close it.
			var diags diagnostics.Diagnostics
			diags = diags.Append(diagnostics.Sourceless(sev.Error, "Failed to close dependency locks handle", "The Terraform RPC API failed to close the dependency locks: %s.\n\nThis is a bug in the program, please report it!", err))
			view.Diagnostics(diags)
		}
	}
}

// OpenProviderCache opens a provider cache from the given path and returns a handle to it.
// dotTFProvidersPath is the path to the provider cache - "./.terraform/providers/"
func (tf *tfStateOperations) OpenProviderCache(dotTFProvidersPath string) (int64, diagnostics.Diagnostics, func(format.View)) {
	var diags diagnostics.Diagnostics

	response, err := tf.client.Dependencies().OpenProviderPluginCache(tf.ctx,
		&dependencies.OpenProviderPluginCache_Request{
			CacheDir: dotTFProvidersPath,
		})

	if err != nil {
		diags = diags.Append(diagnostics.Sourceless(sev.Error, "Failed to open provider cache", "This could be because `tfstacks providers lock` wasn't executed. The Terraform API returned the following error: %s.", err.Error()))
		return -1, diags, func(_ format.View) {}
	}

	return response.ProviderCacheHandle, diags, func(view format.View) {
		_, err := tf.client.Dependencies().CloseProviderPluginCache(context.Background(),
			&dependencies.CloseProviderPluginCache_Request{
				ProviderCacheHandle: response.ProviderCacheHandle,
			})
		if err != nil {
			// Since we opened the connection successfully, we should be able to
			// close it.
			var diags diagnostics.Diagnostics
			diags = diags.Append(diagnostics.Sourceless(sev.Error, "Failed to close provider cache handle", "The Terraform RPC API failed to close the provider cache: %s.\n\nThis is a bug in the program, please report it!", err))
			view.Diagnostics(diags)
		}
	}
}

// OpenTerraformState opens a Terraform state file from the given directory and returns a handle to it.
// tfStateFileDir is the path to the directory where the Terraform state file is located.
func (tf *tfStateOperations) OpenTerraformState(tfStateFileDir string) (int64, diagnostics.Diagnostics, func(format.View)) {
	var diags diagnostics.Diagnostics

	response, err := tf.client.Stacks().OpenTerraformState(tf.ctx,
		&stacks.OpenTerraformState_Request{
			State: &stacks.OpenTerraformState_Request_ConfigPath{
				ConfigPath: tfStateFileDir,
			},
		})
	if err != nil {
		diags = diags.Append(diagnostics.Sourceless(sev.Error, "Failed to open state", "The Terraform API returned the following error: %s.", err.Error()))
		return -1, diags, func(_ format.View) {}
	}
	diags = diags.Append(response.Diagnostics)
	return response.StateHandle, diags, func(view format.View) {
		_, err := tf.client.Stacks().CloseTerraformState(context.Background(),
			&stacks.CloseTerraformState_Request{
				StateHandle: response.StateHandle,
			})
		if err != nil {
			// Since we opened the connection successfully, we should be able to
			// close it.
			var diags diagnostics.Diagnostics
			diags = diags.Append(diagnostics.Sourceless(sev.Error, "Failed to close state handle", "The Terraform RPC API failed to close the state: %s.\n\nThis is a bug in the program, please report it!", err))
			view.Diagnostics(diags)
		}
	}
}

// MigrateTFState migrates the Terraform state using the provided handles and mappings.
// events emitted can be looped over events.Recv()
func (tf *tfStateOperations) MigrateTFState(tfStateHandle int64, stackConfigHandle int64, dependencyLocksHandle int64, providerCacheHandle int64, resources map[string]string, modules map[string]string) (stacks.Stacks_MigrateTerraformStateClient, error) {

	events, err := tf.client.Stacks().MigrateTerraformState(context.Background(),
		&stacks.MigrateTerraformState_Request{
			StateHandle:           tfStateHandle,
			ConfigHandle:          stackConfigHandle,
			DependencyLocksHandle: dependencyLocksHandle,
			ProviderCacheHandle:   providerCacheHandle,
			Mapping: &stacks.MigrateTerraformState_Request_Simple{
				Simple: &stacks.MigrateTerraformState_Request_Mapping{
					ResourceAddressMap: resources,
					ModuleAddressMap:   modules,
				},
			},
		})

	return events, err
}

// StartTFRPCServer starts the Terraform RPC API server and returns a function to stop it.
func (tf *tfStateOperations) StartTFRPCServer() (StopTFRPCAPIServer func(), err error) {
	cmd := exec.Command(fmt.Sprintf("TERRAFORM_RPCAPI_COOKIE=%s", constants.TerraformRPCAPICookie), "terraform", "rpcapi")

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	time.Sleep(2 * time.Second) // naive wait

	StopTFRPCAPIServer = func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}

	if err := cmd.Process.Signal(os.Signal(nil)); err != nil && !errors.Is(err, os.ErrInvalid) {
		StopTFRPCAPIServer()
		return nil, errors.New("rpcapi process exited unexpectedly")
	}

	return StopTFRPCAPIServer, nil
}
