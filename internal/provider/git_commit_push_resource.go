// Copyright IBM Corp. 2024, 2025
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"os"
	"terraform-provider-tfmigrate/internal/gitops"
	gitUtil "terraform-provider-tfmigrate/internal/util/vcs/git"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

const (
	DefaultCommitMessage = "[skip ci] Migrating to TFC"
)

type gitCommitPush struct {
	gitPatToken     string
	gitOps          gitops.GitOperations
	allowCommitPush bool
}

var (
	_ resource.Resource = &gitCommitPush{}
)

func NewGitCommitPushResource() resource.Resource {
	return &gitCommitPush{
		gitOps: gitops.NewGitOperations(context.Background(), gitUtil.NewGitUtil(context.Background())),
	}
}

type GitCommitPushModel struct {
	DirectoryPath types.String `tfsdk:"directory_path"`
	CommitMessage types.String `tfsdk:"commit_message"`
	EnablePush    types.Bool   `tfsdk:"enable_push"`
	RemoteName    types.String `tfsdk:"remote_name"`
	BranchName    types.String `tfsdk:"branch_name"`
	Summary       types.String `tfsdk:"summary"`
	CommitHash    types.String `tfsdk:"commit_hash"`
}

func (r *gitCommitPush) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_git_commit_push"
}

func (r *gitCommitPush) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Git Commmit Push Resource: This resource is used to execute git commit and git push commands in the said directory.",
		Attributes: map[string]schema.Attribute{
			"directory_path": schema.StringAttribute{
				MarkdownDescription: "The directory path where Git Commit needs to be executed.",
				Required:            true,
			},
			"commit_message": schema.StringAttribute{
				MarkdownDescription: "The commit message that needs to be used for the commit.",
				Required:            true,
			},
			"enable_push": schema.BoolAttribute{
				MarkdownDescription: "Enable Push to remote branch after commit.",
				Computed:            true,
			},
			"remote_name": schema.StringAttribute{
				MarkdownDescription: "The name of the remote to push to e.g origin.",
				Required:            true,
			},
			"branch_name": schema.StringAttribute{
				MarkdownDescription: "The name of the remote branch to push to e.g. main.",
				Required:            true,
			},
			"commit_hash": schema.StringAttribute{
				MarkdownDescription: "The commit hash of the commit.",
				Computed:            true,
			},
			"summary": schema.StringAttribute{
				MarkdownDescription: "Summary of the Git Commit and Push Resource.",
				Computed:            true,
			},
		},
	}
}

func (r *gitCommitPush) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data GitCommitPushModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !r.allowCommitPush {
		tflog.Debug(ctx, "Git Commit Push is not allowed in the current configuration.")
		data.EnablePush = types.BoolValue(false)
		data.Summary = types.StringValue("Git commit and push is disabled")
		data.CommitHash = types.StringValue("")
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
		return
	}

	data.EnablePush = types.BoolValue(r.allowCommitPush)
	dirPath := data.DirectoryPath.ValueString()
	_, err := os.Stat(dirPath)
	if err != nil {
		tflog.Error(ctx, "Error executing git actions: Specified Dir Path doess not exist")
		resp.Diagnostics.AddError("Error executing git actions: Specified Dir Path doess not exist", "")
		return
	}
	commitMessage := data.CommitMessage.ValueString()
	if commitMessage == "" {
		commitMessage = DefaultCommitMessage
	}

	tflog.Info(ctx, "Executing Git Commit")
	commitHash, err := r.gitOps.CreateCommit(dirPath, commitMessage)
	if err != nil {
		tflog.Error(ctx, "Error executing Git Commit "+err.Error())
		resp.Diagnostics.AddError("Error executing Git Commit", err.Error())
		return
	}
	summary := "Git Commit with Commit Hash " + commitHash + " Completed."
	data.CommitHash = types.StringValue(commitHash)
	data.Summary = types.StringValue(summary)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)

	if data.EnablePush.ValueBool() {
		createPushParams := gitUtil.PushCommitParams{
			RepoPath:    dirPath,
			RemoteName:  data.RemoteName.ValueString(),
			BranchName:  data.BranchName.ValueString(),
			GitPatToken: r.gitPatToken,
			Force:       true,
		}
		// check if the branch exists in the remote and local
		branchExists, err := r.gitOps.BranchExists(createPushParams.RemoteName, createPushParams.BranchName)
		if err != nil {
			tflog.Error(ctx, "Error checking branch existence: "+err.Error())
			resp.Diagnostics.AddError("Error checking branch existence: ", err.Error())
			return
		}
		if !branchExists {
			tflog.Info(ctx, "Current Branch does not exist in remote")
			resp.Diagnostics.AddError("Current Branch does not exist in remote", "Please check the branch and try again.")
			return
		}
		err = r.gitOps.PushCommit(createPushParams)
		if err != nil {
			tflog.Error(ctx, "Error executing Git Push: "+err.Error())
			resp.Diagnostics.AddError(fmt.Sprintf("Error executing Git Push: %s", err.Error()), err.Error())
			return
		}
		summary += "and Pushed"
	}

	data.Summary = types.StringValue(summary)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *gitCommitPush) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
}

func (r *gitCommitPush) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data GitCommitPushModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.AddWarning(UpdateActionNotSupported, UpdateActionNotSupportedDetailed)
	data.Summary = types.StringValue(UpdateActionNotSupportedDetailed)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *gitCommitPush) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	tflog.Warn(ctx, "Destroy in the configs detected, But this resource does not support destroy operation.")
}

func (r *gitCommitPush) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerResourceData, ok := req.ProviderData.(ProviderResourceData)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected TF_GIT_PAT_TOKEN Found",
			fmt.Sprintf("providerResourceData from context is %v.", providerResourceData),
		)

		return
	}
	r.gitPatToken = providerResourceData.GitPatToken
	r.allowCommitPush = providerResourceData.AllowCommitPush
}
