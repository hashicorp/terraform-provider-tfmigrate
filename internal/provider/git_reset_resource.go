// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"os"
	"terraform-provider-tfmigrate/internal/gitops"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

type gitReset struct {
}

var (
	_ resource.Resource = &gitReset{}
)

func NewGitResetResource() resource.Resource {
	return &gitReset{}
}

type GitResetModel struct {
	DirectoryPath types.String `tfsdk:"directory_path"`
}

func (r *gitReset) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_git_reset"
}

func (r *gitReset) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Git Reset Resource: This resource is used to execute git reset command in the said directory.",
		Attributes: map[string]schema.Attribute{
			"directory_path": schema.StringAttribute{
				MarkdownDescription: "The directory path where git reset needs to be executed.",
				Required:            true,
			},
		},
	}
}

func (r *gitReset) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {

	var data GitResetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	dirPath := data.DirectoryPath.ValueString()
	_, err := os.Stat(dirPath)
	if err != nil {
		tflog.Error(ctx, DIR_PATH_DOES_NOT_EXIST)
		resp.Diagnostics.AddError(DIR_PATH_DOES_NOT_EXIST, fmt.Sprintf(DIR_PATH_DOES_NOT_EXIST_DETAILED, dirPath))
		return
	}
	tflog.Info(ctx, "Executing Git Reset")
	err = gitops.ResetToLastCommittedVersion(dirPath)
	if err != nil {
		tflog.Error(ctx, "Error executing Git Reset: "+err.Error())
		resp.Diagnostics.AddError("Error executing Git Reset:", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *gitReset) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
}

func (r *gitReset) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data GitResetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.AddWarning(UPDATE_ACTION_NOT_SUPPORTED, UPDATE_ACTION_NOT_SUPPORTED_DETAILED)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *gitReset) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	tflog.Warn(ctx, DESTROY_ACTION_NOT_SUPPORTED)
}
