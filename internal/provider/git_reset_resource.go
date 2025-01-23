// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"os"
	gitops "terraform-provider-tfmigrate/internal/helper"
	gitUtil "terraform-provider-tfmigrate/internal/util/vcs/git"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

type gitReset struct {
	gitOps gitops.GitOperations
}

var (
	_ resource.Resource = &gitReset{}
)

func NewGitResetResource() resource.Resource {
	return &gitReset{
		gitOps: gitops.NewGitOperations(hclog.L(), gitUtil.NewGitUtil(hclog.L())),
	}
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
		tflog.Error(ctx, DirPathDoesNotExist)
		resp.Diagnostics.AddError(DirPathDoesNotExist, fmt.Sprintf(DirPathDoesNotExistDetailed, dirPath))
		return
	}
	tflog.Info(ctx, "Executing Git Reset")
	err = r.gitOps.ResetToLastCommittedVersion(dirPath)
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
	resp.Diagnostics.AddWarning(UpdateActionNotSupported, UpdateActionNotSupportedDetailed)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *gitReset) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	tflog.Warn(ctx, DestroyActionNotSupported)
}
