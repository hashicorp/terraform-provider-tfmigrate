// Copyright IBM Corp. 2024, 2025
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"terraform-provider-tfmigrate/internal/gitops"
	gitUtil "terraform-provider-tfmigrate/internal/util/vcs/git"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

const (
	MigrationFeatureBranchPrefix = `hcp-migrate-`
)

type gitPr struct {
	gitPatToken string
	gitOps      gitops.GitOperations
	createPr    bool
}

var (
	_ resource.Resource = &gitPr{}
)

func NewGitPrResource() resource.Resource {
	return &gitPr{
		gitOps: gitops.NewGitOperations(context.Background(), gitUtil.NewGitUtil(context.Background())),
	}
}

type GitPrModel struct {
	RepoIdentifier types.String `tfsdk:"repo_identifier"`
	PrTitle        types.String `tfsdk:"pr_title"`
	PrBody         types.String `tfsdk:"pr_body"`
	SourceBranch   types.String `tfsdk:"source_branch"`
	DestinBranch   types.String `tfsdk:"destin_branch"`
	Summary        types.String `tfsdk:"summary"`
	PrUrl          types.String `tfsdk:"pull_request_url"`
}

func (r *gitPr) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_git_pr"
}

func (r *gitPr) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Git Pr Resource: This resource is used to create PR on VCS Service provider.",
		Attributes: map[string]schema.Attribute{
			"repo_identifier": schema.StringAttribute{
				MarkdownDescription: "The identifier of the repository in the format `owner/repo`.",
				Required:            true,
			},
			"pr_title": schema.StringAttribute{
				MarkdownDescription: "The PR title.",
				Required:            true,
			},
			"pr_body": schema.StringAttribute{
				MarkdownDescription: "Content of the PR Body.",
				Required:            true,
			},
			"source_branch": schema.StringAttribute{
				MarkdownDescription: "The feature branch from which the PR will be created.",
				Required:            true,
			},
			"destin_branch": schema.StringAttribute{
				MarkdownDescription: "The Base branch into which the PR will be merged into.",
				Required:            true,
			},
			"pull_request_url": schema.StringAttribute{
				MarkdownDescription: "The URL of the Pull Request created.",
				Computed:            true,
			},
			"summary": schema.StringAttribute{
				MarkdownDescription: "Summary of the Git Commit Resource.",
				Computed:            true,
			},
		},
	}
}

func (r *gitPr) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data GitPrModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !r.createPr {
		tflog.Debug(ctx, "Create PR is not enabled")
		data.Summary = types.StringValue("PR creation is disabled")
		data.PrUrl = types.StringValue("")
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
		return
	}

	createPrParams := gitUtil.PullRequestParams{
		RepoIdentifier: data.RepoIdentifier.ValueString(),
		BaseBranch:     data.DestinBranch.ValueString(),
		FeatureBranch:  data.SourceBranch.ValueString(),
		Title:          data.PrTitle.ValueString(),
		Body:           data.PrBody.ValueString(),
		GitPatToken:    r.gitPatToken,
	}
	// Call the helper function in the Create method
	if err := r.validateAndSetBranches(ctx, &createPrParams.BaseBranch, &createPrParams.FeatureBranch); err != nil {
		tflog.Error(ctx, "Error validating branches: "+err.Error())
		resp.Diagnostics.AddError(fmt.Sprintf("Error validating branches: %s", err.Error()), err.Error())
		return
	}
	tflog.Info(ctx, "Executing GitHub PR Creation")

	prURL, err := r.gitOps.CreatePullRequest(createPrParams)
	if err != nil {
		tflog.Error(ctx, "Error creating PR: "+err.Error())
		resp.Diagnostics.AddError(fmt.Sprintf("Error creating PR: %s", err.Error()), err.Error())
		return
	}

	data.Summary = types.StringValue("Git PR Created: " + prURL)
	data.PrUrl = types.StringValue(prURL)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *gitPr) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {}

func (r *gitPr) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data GitPrModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.AddWarning(UpdateActionNotSupported, UpdateActionNotSupportedDetailed)
	data.Summary = types.StringValue(UpdateActionNotSupported)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *gitPr) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	tflog.Warn(ctx, DestroyActionNotSupported)
}

func (r *gitPr) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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
	r.createPr = providerResourceData.CreatePr
}

// validateAndSetBranches validates and sets the source and destination branches if they are empty.
func (r *gitPr) validateAndSetBranches(ctx context.Context, baseBranch, featureBranch *string) error {
	// Check if the destination branch is empty
	// If empty, use the default base branch of the repo
	if *baseBranch == "" {
		// Take the default branch of the current repo
		tflog.Debug(ctx, "Destination branch is empty, using default base branch of the repo")
		defaultBaseBranch, err := r.gitOps.GetDefaultBaseBranch()
		if err != nil {
			return fmt.Errorf("error fetching default base branch: %w", err)
		}
		*baseBranch = defaultBaseBranch
	}

	// Check if the source branch is empty
	// If empty, use the current branch of the repo
	if *featureBranch == "" {
		// Take the current branch of the current repo
		tflog.Debug(ctx, "Source branch is empty, therefore using current branch of the repo")
		defaultFeatureBranch, err := r.gitOps.GetCurrentBranch()
		if err != nil {
			return fmt.Errorf("error fetching default feature branch: %w", err)
		}
		*featureBranch = defaultFeatureBranch

		// check if the source branch (current) is present in the remote
		remoteName, err := r.gitOps.GetRemoteName()
		if err != nil {
			return fmt.Errorf("error fetching remote name: %w", err)
		}
		branchExists, err := r.gitOps.BranchExists(*featureBranch, remoteName)
		if err != nil {
			return fmt.Errorf("error checking if branch exists: %w", err)
		}
		if !branchExists {
			return fmt.Errorf("feature branch does not exist in the remote: %s", *featureBranch)
		}
	}
	// now check bothe branches are same, if same then return error
	if *baseBranch == *featureBranch {
		return fmt.Errorf("source and destination branches cannot be same: %s", *baseBranch)
	}
	return nil
}
