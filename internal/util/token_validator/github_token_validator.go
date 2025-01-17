package token_validator

import (
	"context"
	"fmt"
	"net/http"

	"terraform-provider-tfmigrate/internal/util/git"

	"github.com/google/go-github/v66/github"
	"github.com/hashicorp/go-hclog"

	cliErrs "terraform-provider-tfmigrate/internal/cli_errors"
	"terraform-provider-tfmigrate/internal/constants"
)

// githubTokenValidator implements GithubTokenValidator.
type githubTokenValidator struct {
	ctx        context.Context
	git        git.GitUtil
	githubUtil git.GithubUtil
	logger     hclog.Logger
}

// GithubTokenValidator extends TokenValidator for GitHub-specific token validation.
type GithubTokenValidator interface {
	TokenValidator
}

// ValidateToken creates a new instance of GithubTokenValidator.
func (g *githubTokenValidator) ValidateToken(repoUrl string, repoIdentifier string) (string, error) {
	_, err := g.git.GetGitToken(g.git.GetRemoteServiceProvider(repoUrl))
	if err != nil {
		suggestions, gitTokenErr := gitTokenErrorHandler(err, g.logger)
		return suggestions, gitTokenErr
	}

	orgName, repoName := g.git.GetOrgAndRepoName(repoIdentifier)
	statusCode, err := g.validateGitPatToken(g.git.GetRemoteServiceProvider(repoUrl), orgName, repoName)
	if err != nil {
		suggestions, gitTokenErr := gitTokenErrorHandler(err, g.logger, statusCode)
		return suggestions, gitTokenErr
	}

	return "", nil
}

// validateGitPatToken validates the git pat token.
func (g *githubTokenValidator) validateGitPatToken(gitServiceProvider *constants.GitServiceProvider, owner string, repositoryName string) (int, error) {
	if *gitServiceProvider == constants.GitHub {
		return g.validateGithubTokenRepoAccess(owner, repositoryName)
	}
	// Currently, only GitHub is supported.
	// This check is for future-proofing to avoid any panic.
	return 0, cliErrs.ErrGitSvcPvdNotSupported
}

// validateGithubTokenRepoAccess validates the github pat token.
func (g *githubTokenValidator) validateGithubTokenRepoAccess(owner string, repositoryName string) (int, error) {

	repoDetails, resp, err := g.githubUtil.GetRepository(owner, repositoryName)
	if err != nil {
		g.logger.Error(fmt.Sprintf("error fetching repository details err: %v", err))
		return 0, err
	}

	if repoDetails == nil {
		return handleNonSuccessResponse(resp.Response, g.logger)
	}

	return http.StatusOK, g.handleGitHubSuccessResponse(repoDetails)
}

// handleGitHubSuccessResponse handles the success response.
func (g *githubTokenValidator) handleGitHubSuccessResponse(repoDetails *github.Repository) error {
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
