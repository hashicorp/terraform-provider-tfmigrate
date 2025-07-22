// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"io"
	stateOps "terraform-provider-tfmigrate/internal/helper"
	"terraform-provider-tfmigrate/internal/terraform/rpcapi"
	"terraform-provider-tfmigrate/internal/terraform/rpcapi/terraform1/stacks"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"google.golang.org/protobuf/encoding/protojson"
)

var (
	jsonOpts = protojson.MarshalOptions{
		Multiline: true,
	}
)

type stackMigrate struct {
	stateOps stateOps.TFStateOperations
}

var (
	_ resource.Resource = &stackMigrate{}
)

func NewStackMigrateResource() resource.Resource {
	return &stackMigrate{}
}

type StackMigrateModel struct {
	// TBA
}

func (r *stackMigrate) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	// TBA
}

func (r *stackMigrate) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	// TBA
}

func (r *stackMigrate) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// Sample RPC call flow, to be integrated with - terraform-provider-tfmigrate/pull/166

	client, err := rpcapi.Connect(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to connect to RPC API",
			"An error occurred while trying to connect to the RPC API: "+err.Error())
		return
	}
	defer client.Stop()

	r.stateOps = stateOps.NewTFStateOperations(ctx, client)

	tfStateHandle, closeTFState, err := r.stateOps.OpenTerraformState(".")
	if err != nil {
		resp.Diagnostics.AddError("Unable to open Terraform state",
			"An error occurred while trying to open the Terraform state: "+err.Error())
		return
	}

	sourceBundleHandle, closeSourceBundle, err := r.stateOps.OpenSourceBundle("./.terraform/modules")
	if err != nil {
		resp.Diagnostics.AddError("Unable to open source bundle",
			"An error occurred while trying to open the source bundle: "+err.Error())
		return
	}

	stackConfigHandle, closeConfig, err := r.stateOps.OpenStacksConfiguration(sourceBundleHandle, "./_stacks_generated")
	if err != nil {
		resp.Diagnostics.AddError("Unable to open stacks configuration",
			"An error occurred while trying to open the stacks configuration: "+err.Error())
		return
	}

	dependencyLocksHandle, closeLock, err := r.stateOps.OpenDependencyLockFile(sourceBundleHandle, "./.terraform.lock.hcl")
	if err != nil {
		resp.Diagnostics.AddError("Unable to open dependency lock file",
			"An error occurred while trying to open the dependency lock file: "+err.Error())
		return
	}

	providerCacheHandle, closeProviderCache, err := r.stateOps.OpenProviderCache("./.terraform/providers/")
	if err != nil {
		resp.Diagnostics.AddError("Unable to open provider cache",
			"An error occurred while trying to open the provider cache: "+err.Error())
		return
	}

	events, err := r.stateOps.MigrateTFState(
		tfStateHandle,
		stackConfigHandle,
		dependencyLocksHandle,
		providerCacheHandle,
		map[string]string{}, // resources map, to be filled as needed
		map[string]string{}, // modules map, to be filled as needed
	)
	if err != nil {
		resp.Diagnostics.AddError("Unable to migrate Terraform state",
			"An error occurred while trying to migrate the Terraform state: "+err.Error())
		return
	}

	for {
		item, err := events.Recv()
		if err == io.EOF {
			break // all done
		} else if err != nil {
			resp.Diagnostics.AddError("Error receiving migration events",
				"An error occurred while receiving migration events: "+err.Error())
			return
		}

		switch result := item.Result.(type) {
		case *stacks.MigrateTerraformState_Event_AppliedChange:
			for _, change := range result.AppliedChange.Descriptions {
				fmt.Println(jsonOpts.Format(change)) // Handle
			}
		case *stacks.MigrateTerraformState_Event_Diagnostic:
			// Handle diagnostics
		}
	}

	// Close all handles, refactor to use defer if required
	if err := closeTFState(); err != nil {
		resp.Diagnostics.AddError("Error closing Terraform state handle",
			"An error occurred while closing the Terraform state handle: "+err.Error())
		return
	}
	if err := closeSourceBundle(); err != nil {
		resp.Diagnostics.AddError("Error closing source bundle handle",
			"An error occurred while closing the source bundle handle: "+err.Error())
		return
	}
	if err := closeConfig(); err != nil {
		resp.Diagnostics.AddError("Error closing stacks configuration handle",
			"An error occurred while closing the stacks configuration handle: "+err.Error())
		return
	}
	if err := closeLock(); err != nil {
		resp.Diagnostics.AddError("Error closing dependency lock file handle",
			"An error occurred while closing the dependency lock file handle: "+err.Error())
		return
	}
	if err := closeProviderCache(); err != nil {
		resp.Diagnostics.AddError("Error closing provider cache handle",
			"An error occurred while closing the provider cache handle: "+err.Error())
		return
	}

}

func (r *stackMigrate) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// TBA
}

func (r *stackMigrate) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// TBA
}

func (r *stackMigrate) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// TBA
}
