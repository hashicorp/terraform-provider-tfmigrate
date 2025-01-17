package token_validator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"

	gitMocks "terraform-provider-tfmigrate/_mocks/util_mocks/git_mocks"
	cliErrs "terraform-provider-tfmigrate/internal/cli_errors"
	"terraform-provider-tfmigrate/internal/constants"

	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	gitlab "gitlab.com/gitlab-org/api/client-go"
)

func TestValidateToken_gitlab(t *testing.T) {
	// t.Skip("skipping test")
	repoUrl := "https://gitlab.com/demo-group614536/sample-gitlab-project"
	projectIdentifier := "demo-group614536/sample-gitlab-project"
	for name, tc := range map[string]struct {
		err     error
		suggest string
	}{
		"UnknownGitServiceProvider": {
			err:     cliErrs.ErrGitServiceProviderNotSupported,
			suggest: constants.SuggestUsingGithubOrGitlab,
		},
		"UnknownErrorOccurred": {
			err:     cliErrs.ErrUnknownError,
			suggest: constants.SuggestUnknownErrorSolution,
		},
		"ErrServerError": {
			err:     fmt.Errorf("server error occurred during API call with status code: %d", http.StatusGatewayTimeout),
			suggest: constants.SuggestServerErrorSolution,
		},
		"ErrUnexpectedStatusCode": {
			err:     cliErrs.ErrUnexpectedStatusCode,
			suggest: constants.SuggestCheckingApiDocs,
		},
		"ErrRepositoryNotFound": {
			err:     cliErrs.ErrRepositoryNotFound,
			suggest: constants.SuggestValidatingRepoNameOrTokenDoesNotHaveAccessToRead,
		},
		"Success": {},
	} {
		t.Run(name, func(t *testing.T) {

			r := require.New(t)
			ctx := context.Background()
			logger := hclog.FromContext(ctx)
			git := gitMocks.NewMockGitUtil(t)
			gitlabUtil := gitMocks.NewMockGitlabUtil(t)

			g := &gitlabTokenValidator{
				ctx:        ctx,
				git:        git,
				gitlabUtil: gitlabUtil,
				logger:     logger,
			}

			func() {
				if name == "UnknownGitServiceProvider" {
					git.
						On("GetRemoteServiceProvider", mock.Anything).
						Return(&constants.UnknownGitServiceProvider)
					git.
						On("GetGitToken", mock.Anything).
						Return("", cliErrs.ErrGitServiceProviderNotSupported)
					return
				}

				git.
					On("GetRemoteServiceProvider", mock.Anything).
					Return(&constants.GitLab)

				git.
					On("GetGitToken", mock.Anything).
					Return("gitlab_test_token", nil)

				if name == "UnknownErrorOccurred" {
					gitlabUtil.
						On("GetProject", mock.Anything).
						Return(nil, nil, errors.New("unknown error occurred"))
					return
				}

				if name == "ErrServerError" {
					gitlabUtil.
						On("GetProject", mock.Anything).
						Return(nil, getMockGitlabResponse(http.StatusGatewayTimeout, "server error"), nil)
					return
				}

				if name == "ErrUnexpectedStatusCode" {
					gitlabUtil.
						On("GetProject", mock.Anything).
						Return(nil, getMockGitlabResponse(http.StatusUnprocessableEntity, "unprocessable entity"), nil)
					return
				}

				if name == "ErrRepositoryNotFound" {
					gitlabUtil.
						On("GetProject", mock.Anything).
						Return(nil, getMockGitlabResponse(http.StatusNotFound, "repository not found"), nil)
				}

				// Success case
				gitlabUtil.
					On("GetProject", mock.Anything).
					Return(&gitlab.Project{
						ID:          1234,
						Permissions: &gitlab.Permissions{ProjectAccess: &gitlab.ProjectAccess{AccessLevel: gitlab.DeveloperPermissions}},
					}, nil, nil)

			}()

			suggestions, err := g.ValidateToken(repoUrl, projectIdentifier)
			if tc.err != nil {
				r.Equal(tc.err.Error(), err.Error())
				r.Equal(tc.suggest, suggestions)
			} else {
				r.NoError(err)
				r.Empty(suggestions)
			}
		})
	}
}

func getMockGitlabResponse(statusCode int, body string) *gitlab.Response {
	return &gitlab.Response{
		Response: &http.Response{
			StatusCode: statusCode,
			Body:       io.NopCloser(bytes.NewBufferString(body)),
			Header: map[string][]string{
				"Content-Type": {"application/json"},
			},
		},
	}
}
