// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	gitops "terraform-provider-tfmigrate/internal/helper"
	gitUtil "terraform-provider-tfmigrate/internal/util/vcs/git"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

type githubPr struct {
	gitPatToken string
	gitOps      gitops.GitOperations
}

var (
	_ resource.Resource = &githubPr{}
)

func NewGithubPrResource() resource.Resource {
	return &githubPr{
		gitOps: gitops.NewGitOperations(context.Background(), gitUtil.NewGitUtil(context.Background())),
	}
}

type GithubPrModel struct {
	RepoIdentifier types.String `tfsdk:"repo_identifier"`
	PrTitle        types.String `tfsdk:"pr_title"`
	PrBody         types.String `tfsdk:"pr_body"`
	SourceBranch   types.String `tfsdk:"source_branch"`
	DestinBranch   types.String `tfsdk:"destin_branch"`
	Summary        types.String `tfsdk:"summary"`
	PrUrl          types.String `tfsdk:"pull_request_url"`
}

func (r *githubPr) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_github_pr"
}

func (r *githubPr) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Github Pr Resource: This resource is used to create PR on github.",
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

func (r *githubPr) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data GithubPrModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
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

	tflog.Info(ctx, "Executing Git Commit")

	prURL, err := r.gitOps.CreatePullRequest(createPrParams)
	if err != nil {
		tflog.Error(ctx, "Error creating PR: "+err.Error())
		resp.Diagnostics.AddError("Error creating PR: ", err.Error())
		return
	}

	data.Summary = types.StringValue("Git PR Created: " + prURL)
	data.PrUrl = types.StringValue(prURL)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *githubPr) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {}

func (r *githubPr) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data GithubPrModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.AddWarning(UpdateActionNotSupported, UpdateActionNotSupportedDetailed)
	data.Summary = types.StringValue(UpdateActionNotSupported)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *githubPr) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	tflog.Warn(ctx, DestroyActionNotSupported)
}

func (r *githubPr) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerResourceData, ok := req.ProviderData.(ProviderResourceData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Git PAT Token Found",
			fmt.Sprintf("providerResourceData from context is %v.", providerResourceData),
		)
		return
	}

	r.gitPatToken = providerResourceData.GitPatToken
}
