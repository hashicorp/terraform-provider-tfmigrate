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

type gitCommitPush struct {
	githubToken string
}

var (
	_ resource.Resource = &gitCommitPush{}
)

func NewGitCommitPushResource() resource.Resource {
	return &gitCommitPush{}
}

type GitCommitPushModel struct {
	DirectoryPath types.String `tfsdk:"directory_path"`
	CommitMessage types.String `tfsdk:"commit_message"`
	RemoteName    types.String `tfsdk:"remote_name"`
	BranchName    types.String `tfsdk:"branch_name"`
	Summary       types.String `tfsdk:"summary"`
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
			"remote_name": schema.StringAttribute{
				MarkdownDescription: "The name of the remote to push to e.g origin.",
				Required:            true,
			},
			"branch_name": schema.StringAttribute{
				MarkdownDescription: "The name of the remote branch to push to e.g. main.",
				Required:            true,
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
	dirPath := data.DirectoryPath.ValueString()
	_, err := os.Stat(dirPath)
	if err != nil {
		tflog.Error(ctx, "Error executing git actions: Specified Dir Path doess not exist")
		resp.Diagnostics.AddError("Error executing git actions: Specified Dir Path doess not exist", "")
		return
	}
	commitMessage := data.CommitMessage.ValueString()

	tflog.Info(ctx, "Executing Git Commit Push")
	commitHash, err := gitops.CreateCommit(dirPath, commitMessage)
	if err != nil {
		tflog.Error(ctx, "Error executing Git Commit Push: "+err.Error())
		resp.Diagnostics.AddError("Error executing Git Commit Push:", err.Error())
		return
	}

	err = gitops.PushCommit(dirPath, data.RemoteName.ValueString(), data.BranchName.ValueString(), r.githubToken, true)
	data.Summary = types.StringValue("Git Commit Push Completed with Commit Hash: " + commitHash)
	if err != nil {
		tflog.Error(ctx, "Error executing Git Commit Push: "+err.Error())
		resp.Diagnostics.AddError("Error executing Git Commit Push:", err.Error())
		return
	}

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
	resp.Diagnostics.AddWarning(UPDATE_ACTION_NOT_SUPPORTED, UPDATE_ACTION_NOT_SUPPORTED_DETAILED)
	data.Summary = types.StringValue(UPDATE_ACTION_NOT_SUPPORTED_DETAILED)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *gitCommitPush) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	tflog.Warn(ctx, "Destroy in the configs detected, But this resource does not support destroy operation.")
}

func (r *gitCommitPush) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	githubToken, ok := req.ProviderData.(string)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Github Token Found",
			fmt.Sprintf("Expected string, got %T.", req.ProviderData),
		)

		return
	}

	r.githubToken = githubToken
}
