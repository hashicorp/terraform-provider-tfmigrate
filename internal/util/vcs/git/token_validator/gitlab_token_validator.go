package token_validator

import (
	"context"
	"fmt"
	"net/http"

	"terraform-provider-tfmigrate/internal/util/vcs/git"

	"terraform-provider-tfmigrate/internal/constants"

	"github.com/hashicorp/go-hclog"
	gitlab "gitlab.com/gitlab-org/api/client-go"

	cliErrs "terraform-provider-tfmigrate/internal/cli_errors"
)

// gitlabTokenValidator implements GitlabTokenValidator.
type gitlabTokenValidator struct {
	ctx        context.Context
	git        git.GitUtil
	gitlabUtil git.GitlabUtil
	logger     hclog.Logger
}

// GitlabTokenValidator extends TokenValidator for GitLab-specific token validation.
type GitlabTokenValidator interface {
	TokenValidator
}

// ValidateToken validates the GitLab token and repository access.
func (g *gitlabTokenValidator) ValidateToken(repoUrl string, projectIdentifier string) (string, error) {
	_, err := g.git.GetGitToken(g.git.GetRemoteServiceProvider(repoUrl))
	if err != nil {
		suggestions, gitTokenErr := gitTokenErrorHandler(err, g.logger)
		return suggestions, gitTokenErr
	}
	statusCode, err := g.validateGitPatToken(g.git.GetRemoteServiceProvider(repoUrl), projectIdentifier)
	if err != nil {
		suggestions, gitTokenErr := gitTokenErrorHandler(err, g.logger, statusCode)
		return suggestions, gitTokenErr
	}

	return "", nil
}

// validateGitPatToken validates the GitLab PAT token.
func (g *gitlabTokenValidator) validateGitPatToken(gitServiceProvider *constants.GitServiceProvider, projectIdentifier string) (int, error) {
	if *gitServiceProvider == constants.GitLab {
		return g.validateGitlabTokenRepoAccess(projectIdentifier)
	}
	// Currently, only GitLab is supported.
	return 0, cliErrs.ErrGitSvcPvdNotSupported
}

// validateGitlabTokenRepoAccess validates the GitLab PAT token for repository access.
func (g *gitlabTokenValidator) validateGitlabTokenRepoAccess(projectIdentifier string) (int, error) {
	projectDetails, resp, err := g.gitlabUtil.GetProject(projectIdentifier)

	if err != nil {
		g.logger.Error(fmt.Sprintf("error fetching project details err: %v", err))
		return 0, err
	}

	if projectDetails == nil {
		return handleNonSuccessResponse(resp.Response, g.logger)
	}

	return http.StatusOK, g.handleGitlabSuccessResponse(projectDetails)
}

// handleGitlabSuccessResponse handles the success response.
func (g *gitlabTokenValidator) handleGitlabSuccessResponse(projectDetails *gitlab.Project) error {
	if projectDetails.Permissions.ProjectAccess.AccessLevel < gitlab.DeveloperPermissions {
		return cliErrs.ErrTokenDoesNotHaveWritePermission
	}
	if projectDetails.Permissions.ProjectAccess.AccessLevel < gitlab.ReporterPermissions {
		return cliErrs.ErrTokenDoesNotHaveReadPermission
	}

	return nil
}
