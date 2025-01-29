package remote_svc_provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"terraform-provider-tfmigrate/internal/util/vcs/git"

	"github.com/google/go-github/v66/github"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"golang.org/x/oauth2"

	cliErrs "terraform-provider-tfmigrate/internal/cli_errors"
)

// githubSvcProvider implements GithubSvcProvider.
type githubSvcProvider struct {
	ctx        context.Context
	git        git.GitUtil
	githubUtil git.GithubUtil
}

// GithubSvcProvider extends RemoteVcsSvcProvider for GitHub-specific token validation.
type GithubSvcProvider interface {
	RemoteVcsSvcProvider
}

// ValidateToken creates a new instance of GithubSvcProvider.
func (g *githubSvcProvider) ValidateToken(repoUrl string, repoIdentifier string) (string, error) {
	if _, err := g.git.GetGitToken(g.git.GetRemoteServiceProvider(repoUrl)); err != nil {
		suggestions, gitTokenErr := gitTokenErrorHandler(err)
		return suggestions, gitTokenErr
	}

	orgName, repoName := g.git.GetOrgAndRepoName(repoIdentifier)
	if statusCode, err := g.validateGithubTokenRepoAccess(orgName, repoName); err != nil {
		return gitTokenErrorHandler(err, statusCode)
	}

	return "", nil
}

// validateGithubTokenRepoAccess validates the github pat token.
func (g *githubSvcProvider) validateGithubTokenRepoAccess(owner string, repositoryName string) (int, error) {

	repoDetails, resp, err := g.githubUtil.GetRepository(owner, repositoryName)
	if err != nil {
		tflog.Error(g.ctx, fmt.Sprintf("error fetching repository details err: %v", err))
		return 0, err
	}

	if repoDetails == nil {
		return handleNonSuccessResponseFromVcsApi(resp.Response)
	}

	return http.StatusOK, g.handleGitHubSuccessResponse(repoDetails)
}

// handleGitHubSuccessResponse handles the success response.
func (g *githubSvcProvider) handleGitHubSuccessResponse(repoDetails *github.Repository) error {
	repoPermissions := repoDetails.GetPermissions()

	if len(repoPermissions) == 0 {
		return cliErrs.ErrResponsePermissionsNil
	}

	if ok, pullPermission := repoPermissions["pull"]; !ok || !pullPermission {
		return cliErrs.ErrTokenDoesNotHaveReadPermission
	}

	if ok, pushPermission := repoPermissions["push"]; !ok || !pushPermission {
		return cliErrs.ErrTokenDoesNotHaveWritePermission
	}

	return nil
}

// CreatePullRequest creates a pull request on the github repository.
func (g *githubSvcProvider) CreatePullRequest(params git.PullRequestParams) (string, error) {
	var err error
	var pr *github.PullRequest
	var resp *github.Response
	client := github.
		NewClient(oauth2.
			NewClient(g.ctx, oauth2.
				StaticTokenSource(&oauth2.Token{AccessToken: params.GitPatToken})))

	newPR := &github.NewPullRequest{
		Title: github.String(params.Title),
		Head:  github.String(params.FeatureBranch),
		Base:  github.String(params.BaseBranch),
		Body:  github.String(params.Body),
	}

	repoOwner := strings.Split(params.RepoIdentifier, "/")[0]
	repoName := strings.Split(params.RepoIdentifier, "/")[1]

	if pr, resp, err = client.PullRequests.Create(g.ctx, repoOwner, repoName, newPR); err != nil {
		tflog.Error(g.ctx, "Failed to create pull request", map[string]interface{}{"owner": repoOwner, "repo": repoName, "pull": newPR.GetTitle(), "error": err})
	}
	if resp.StatusCode != http.StatusCreated {
		err = fmt.Errorf("unexpected status code: %d, expected %d", resp.StatusCode, http.StatusCreated)
		tflog.Error(g.ctx, "Failed to create pull request due to unexpected status code", map[string]interface{}{"status": resp.StatusCode, "error": err})
	}
	return pr.GetHTMLURL(), err
}
