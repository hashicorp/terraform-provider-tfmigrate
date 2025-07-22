// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0.

package helper

import (
	"context"
	"terraform-provider-tfmigrate/internal/terraform/rpcapi"
	"terraform-provider-tfmigrate/internal/terraform/rpcapi/terraform1"
	"terraform-provider-tfmigrate/internal/terraform/rpcapi/terraform1/dependencies"
	"terraform-provider-tfmigrate/internal/terraform/rpcapi/terraform1/stacks"
)

// Order of execution:
// Create the RPC Client
// 1. OpenTerraformState
// 2. OpenSourceBundle
// 3. OpenStacksConfiguration
// 4. OpenDependencyLockFile
// 5. OpenProviderCache
// 6. MigrateTFState
// close open handles and stop the RPC server

type TfStateOperations interface {
	OpenSourceBundle(dotTFModulesPath string) (int64, func() error, error)
	OpenStacksConfiguration(sourceBundleHandle int64, stackConfigPath string) (int64, func() error, error)
	OpenDependencyLockFile(handle int64, dotTFLockFile string) (int64, func() error, error)
	OpenProviderCache(dotTFProvidersPath string) (int64, func() error, error)
	OpenTerraformState(tfStateFileDir string) (int64, func() error, error)
	MigrateTFState(tfStateHandle int64, stackConfigHandle int64, dependencyLocksHandle int64, providerCacheHandle int64, resources map[string]string, modules map[string]string) (stacks.Stacks_MigrateTerraformStateClient, error)
}

type tfStateOperations struct {
	ctx    context.Context
	client rpcapi.Client
}

func NewTfStateOperations(ctx context.Context, client rpcapi.Client) TfStateOperations {
	return &tfStateOperations{
		ctx:    ctx,
		client: client,
	}
}

// OpenSourceBundle opens a source bundle from the given path and returns a handle to it.
func (tf *tfStateOperations) OpenSourceBundle(dotTFModulesPath string) (int64, func() error, error) {
	response, err := tf.client.Dependencies().OpenSourceBundle(tf.ctx, &dependencies.OpenSourceBundle_Request{
		LocalPath: dotTFModulesPath, // dotTFModulesPath is the path to - ".terraform/modules/"
	})
	if err != nil {
		return -1, nil, err
	}

	return response.SourceBundleHandle, func() error {
		_, err := tf.client.Dependencies().CloseSourceBundle(context.Background(),
			&dependencies.CloseSourceBundle_Request{
				SourceBundleHandle: response.SourceBundleHandle,
			})
		return err
	}, nil
}

// OpenStacksConfiguration opens a stack configuration from the given path and returns a handle to it.
func (tf *tfStateOperations) OpenStacksConfiguration(sourceBundleHandle int64, stackConfigPath string) (int64, func() error, error) {
	response, err := tf.client.Stacks().OpenStackConfiguration(tf.ctx,
		&stacks.OpenStackConfiguration_Request{
			SourceBundleHandle: sourceBundleHandle,
			SourceAddress: &terraform1.SourceAddress{
				Source: stackConfigPath, // stackConfigPath is the path to the directory where stack configs files are stored ie "_stacks_generated"
			},
		})

	if err != nil {
		return -1, nil, err
	}

	return response.StackConfigHandle, func() error {
		_, err := tf.client.Stacks().CloseStackConfiguration(tf.ctx,
			&stacks.CloseStackConfiguration_Request{
				StackConfigHandle: response.StackConfigHandle,
			})
		return err
	}, nil
}

// OpenDependencyLockFile opens a dependency lock file from the given path and returns a handle to it.
func (tf *tfStateOperations) OpenDependencyLockFile(handle int64, dotTFLockFile string) (int64, func() error, error) {
	response, err := tf.client.Dependencies().OpenDependencyLockFile(tf.ctx, &dependencies.OpenDependencyLockFile_Request{
		SourceBundleHandle: handle,
		SourceAddress: &terraform1.SourceAddress{
			Source: dotTFLockFile, // dotTFLockFile is the path to the lock file - "./.terraform.lock.hcl"
		},
	})

	if err != nil {
		return -1, nil, err
	}

	return response.DependencyLocksHandle, func() error {
		_, err := tf.client.Dependencies().CloseDependencyLocks(context.Background(),
			&dependencies.CloseDependencyLocks_Request{
				DependencyLocksHandle: response.DependencyLocksHandle,
			})
		return err
	}, nil
}

// OpenProviderCache opens a provider cache from the given path and returns a handle to it.
func (tf *tfStateOperations) OpenProviderCache(dotTFProvidersPath string) (int64, func() error, error) {
	response, err := tf.client.Dependencies().OpenProviderPluginCache(tf.ctx,
		&dependencies.OpenProviderPluginCache_Request{
			CacheDir: dotTFProvidersPath, // dotTFProvidersPath is the path to the provider cache - "./.terraform/providers/"
		})

	if err != nil {
		return -1, nil, err
	}

	return response.ProviderCacheHandle, func() error {
		_, err := tf.client.Dependencies().CloseProviderPluginCache(context.Background(),
			&dependencies.CloseProviderPluginCache_Request{
				ProviderCacheHandle: response.ProviderCacheHandle,
			})
		return err
	}, nil
}

// OpenTerraformState opens a Terraform state file from the given directory and returns a handle to it.
func (tf *tfStateOperations) OpenTerraformState(tfStateFileDir string) (int64, func() error, error) {

	response, err := tf.client.Stacks().OpenTerraformState(tf.ctx,
		&stacks.OpenTerraformState_Request{
			State: &stacks.OpenTerraformState_Request_ConfigPath{
				ConfigPath: tfStateFileDir, // tfStateFileDir is the path to the directory where the Terraform state file is located.
			},
		})

	if err != nil {
		return -1, nil, err
	}

	return response.StateHandle, func() error {
		_, err := tf.client.Stacks().CloseTerraformState(context.Background(),
			&stacks.CloseTerraformState_Request{
				StateHandle: response.StateHandle,
			})
		return err
	}, nil
}

// MigrateTFState migrates the Terraform state using the provided handles and mappings.
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

	// events emitted can be looped over events.Recv()
	return events, err
}
