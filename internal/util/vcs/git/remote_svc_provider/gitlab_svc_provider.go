package remote_svc_provider

import (
	"context"
	"fmt"
	"net/http"

	"terraform-provider-tfmigrate/internal/util/vcs/git"

	"github.com/hashicorp/terraform-plugin-log/tflog"
	gitlab "gitlab.com/gitlab-org/api/client-go"

	cliErrs "terraform-provider-tfmigrate/internal/cli_errors"
)

// gitlabSvcProvider implements GitlabSvcProvider.
type gitlabSvcProvider struct {
	ctx        context.Context
	git        git.GitUtil
	gitlabUtil git.GitlabUtil
}

// GitlabSvcProvider extends RemoteVcsSvcProvider for GitLab-specific token validation.
type GitlabSvcProvider interface {
	RemoteVcsSvcProvider
}

// ValidateToken validates the GitLab token and repository access.
func (g *gitlabSvcProvider) ValidateToken(repoUrl string, projectIdentifier string, tokenFromProvider string) (string, error) {
	if _, err := g.git.GetGitToken(g.git.GetRemoteServiceProvider(repoUrl), tokenFromProvider); err != nil {
		return gitTokenErrorHandler(err)
	}

	if statusCode, err := g.validateGitlabTokenRepoAccess(projectIdentifier); err != nil {
		return gitTokenErrorHandler(err, statusCode)
	}

	return "", nil
}

// validateGitlabTokenRepoAccess validates the GitLab PAT token for repository access.
func (g *gitlabSvcProvider) validateGitlabTokenRepoAccess(projectIdentifier string) (int, error) {
	projectDetails, resp, err := g.gitlabUtil.GetProject(projectIdentifier)

	if err != nil {
		tflog.Error(g.ctx, fmt.Sprintf("error fetching project details err: %v", err))
		return 0, err
	}

	if projectDetails == nil {
		return handleNonSuccessResponseFromVcsApi(resp.Response)
	}

	return http.StatusOK, g.handleGitlabSuccessResponse(projectDetails)
}

// handleGitlabSuccessResponse handles the success response.
func (g *gitlabSvcProvider) handleGitlabSuccessResponse(projectDetails *gitlab.Project) error {
	var gitlabAccessLevel gitlab.AccessLevelValue
	gitlabPermissions := projectDetails.Permissions

	if gitlabPermissions != nil {
		if gitlabPermissions.ProjectAccess != nil {
			gitlabAccessLevel = gitlabPermissions.ProjectAccess.AccessLevel
		} else if gitlabPermissions.GroupAccess != nil {
			gitlabAccessLevel = gitlabPermissions.GroupAccess.AccessLevel
		}
	}

	if gitlabAccessLevel < gitlab.ReporterPermissions {
		return cliErrs.ErrTokenDoesNotHaveReadPermission
	}

	if gitlabAccessLevel < gitlab.DeveloperPermissions {
		return cliErrs.ErrTokenDoesNotHaveWritePermission
	}

	return nil
}

// CreatePullRequest creates a merge request in GitLab.
func (g *gitlabSvcProvider) CreatePullRequest(params git.PullRequestParams) (string, error) {
	var mr *gitlab.MergeRequest
	var resp *gitlab.Response

	gitLabNewClient, err := g.git.NewGitLabClient(params.GitPatToken)
	if err != nil || gitLabNewClient == nil {
		tflog.Error(g.ctx, "Failed to create GitLab client", map[string]interface{}{"error": err})
	}

	mrOptions := &gitlab.CreateMergeRequestOptions{
		SourceBranch: &params.FeatureBranch,
		TargetBranch: &params.BaseBranch,
		Title:        &params.Title,
		Description:  &params.Body,
	}

	if mr, resp, err = gitLabNewClient.MergeRequests.CreateMergeRequest(params.RepoIdentifier, mrOptions); err != nil {
		tflog.Error(g.ctx, fmt.Sprintf("Failed to create merge request for project '%s' with title '%s'", params.RepoIdentifier, *mrOptions.Title), map[string]interface{}{"error": err})
	}
	if resp.StatusCode != http.StatusCreated {
		err := fmt.Errorf("unexpected status code: %d, expected %d", resp.StatusCode, http.StatusCreated)
		tflog.Error(g.ctx, fmt.Sprintf("Failed to create merge request for project '%s' with title '%s' due to unexpected status code %d", params.RepoIdentifier, *mrOptions.Title, resp.StatusCode), map[string]interface{}{"error": err})
	}
	return mr.WebURL, nil
}
