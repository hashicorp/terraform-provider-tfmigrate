package token_validator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/mock"

	gitMocks "terraform-provider-tfmigrate/_mocks/util_mocks/vcs_mocks/git_mocks"
	cliErrs "terraform-provider-tfmigrate/internal/cli_errors"
	"terraform-provider-tfmigrate/internal/constants"

	"github.com/google/go-github/v66/github"
	"github.com/stretchr/testify/require"
)

const (
	serverErrorResponseBody = `{
		"message": "Server error occurred",
		"documentation_url": "https://docs.github.com/rest",
		"status": 504
	}`

	UnexpectedStatusCodeBody = `{
		"message": "Unexpected status code",
		"documentation_url": "https://docs.github.com/rest",
		"status": 422
	}`

	badCredentialsBody = `{
		"message": "Bad credentials",
		"documentation_url": "https://docs.github.com/rest",
		"status": 401
	}`

	resourceProtectedBySsoBody = `{
		"message": "Resource protected by organization SAML enforcement. You must grant your Personal Access token access to an organization within this enterprise.",
		"documentation_url": "https://docs.github.com/articles/authenticating-to-a-github-organization-with-saml-single-sign-on/",
		"status": 403
	}`

	repoNotFoundBody = `{
		"message": "Not Found",
		"documentation_url": "https://docs.github.com/rest/repos/repos#get-a-repository",
		"status": 404
	}`
)

var (
	serverErrorResponse            = getMockResponse(http.StatusGatewayTimeout, serverErrorResponseBody)
	unexpectedStatusCodeResponse   = getMockResponse(http.StatusUnprocessableEntity, UnexpectedStatusCodeBody)
	badCredentialsResponse         = getMockResponse(http.StatusUnauthorized, badCredentialsBody)
	resourceProtectedBySsoResponse = getMockResponse(http.StatusForbidden, resourceProtectedBySsoBody)
	responsePermissionsNil         = &github.Repository{
		Name: github.String("test-repo"),
		Owner: &github.User{
			Login: github.String("test-owner"),
		},
		Private: github.Bool(true),
	}
	repoWithNoReadOrWritePermissions = &github.Repository{
		Name: github.String("test-repo"),
		Owner: &github.User{
			Login: github.String("test-owner"),
		},
		Private: github.Bool(true),
		Permissions: map[string]bool{
			"admin":    false,
			"maintain": false,
			"pull":     false,
			"push":     false,
			"triage":   false,
		},
	}
	repoWithReadPermissionOnly = &github.Repository{
		Name: github.String("test-repo"),
		Owner: &github.User{
			Login: github.String("test-owner"),
		},
		Private: github.Bool(true),
		Permissions: map[string]bool{
			"admin":    false,
			"maintain": false,
			"pull":     true,
			"push":     false,
			"triage":   false,
		},
	}
	repoWithReadAndWritePermissions = &github.Repository{
		Name: github.String("test-repo"),
		Owner: &github.User{
			Login: github.String("test-owner"),
		},
		Private: github.Bool(true),
		Permissions: map[string]bool{
			"admin":    false,
			"maintain": false,
			"pull":     true,
			"push":     true,
			"triage":   false,
		},
	}
)

func TestValidateToken(t *testing.T) {

	repoUrl := "git@github.com:hashicorp/tf-migrate.git"
	repoIdentifier := "hashicorp/tf-migrate"
	for name, tc := range map[string]struct {
		err     error
		suggest string
	}{
		"UnknownGitServiceProvider": {
			err:     cliErrs.ErrGitServiceProviderNotSupported,
			suggest: constants.SuggestUsingGithubOrGitlab,
		},
		"ErrGithubTokenNotSet": {
			err:     cliErrs.ErrTfGitPatTokenNotSet,
			suggest: constants.SuggestSettingClassicGitHubTokenValue,
		},
		"ErrGithubTokenEmpty": {
			err:     cliErrs.ErrTfGitPatTokenEmpty,
			suggest: constants.SuggestSettingClassicGitHubTokenValue,
		},
		"ErrGithubTokenFineGrained": {
			err:     cliErrs.ErrTfGitPatTokenFineGrained,
			suggest: constants.SuggestSettingClassicGitHubTokenValue,
		},
		"ErrGithubTokenUnrecognized": {
			err:     cliErrs.ErrTfGitPatTokenUnrecognized,
			suggest: constants.SuggestSettingClassicGitHubTokenValue,
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
		"ErrTokenExpired": {
			err:     cliErrs.ErrTokenExpired,
			suggest: constants.SuggestSettingUnexpiredToken,
		},
		"ErrTokenDoesNotHaveAccessToOrg": {
			err:     cliErrs.ErrTokenDoesNotHaveAccessToOrg,
			suggest: constants.SuggestProvidingAccessToToken,
		},
		"ErrRepositoryNotFound": {
			err:     cliErrs.ErrRepositoryNotFound,
			suggest: constants.SuggestValidatingRepoNameOrTokenDoesNotHaveAccessToRead,
		},
		"ErrResponsePermissionsNil": {
			err:     cliErrs.ErrResponsePermissionsNil,
			suggest: constants.SuggestCheckingApiDocs,
		},
		"ErrTokenDoesNotHaveReadPermission": {
			err:     cliErrs.ErrTokenDoesNotHaveReadPermission,
			suggest: constants.SuggestProvidingRepoReadPermissionToToken,
		},
		"ErrTokenDoesNotHaveWritePermission": {
			err:     cliErrs.ErrTokenDoesNotHaveWritePermission,
			suggest: constants.SuggestProvidingRepoWritePermissionToToken,
		},
		"Success": {},
	} {
		t.Run(name, func(t *testing.T) {

			r := require.New(t)
			ctx := context.Background()
			git := gitMocks.NewMockGitUtil(t)
			githubUtil := gitMocks.NewMockGithubUtil(t)

			g := &githubTokenValidator{
				ctx:        ctx,
				git:        git,
				githubUtil: githubUtil,
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
					Return(&constants.GitHub)

				if name == "ErrGithubTokenNotSet" {
					git.
						On("GetGitToken", mock.Anything).
						Return("", cliErrs.ErrTfGitPatTokenNotSet)
					return
				}

				if name == "ErrGithubTokenEmpty" {
					git.
						On("GetGitToken", mock.Anything).
						Return("", cliErrs.ErrTfGitPatTokenEmpty)
					return
				}

				if name == "ErrGithubTokenFineGrained" {
					git.
						On("GetGitToken", mock.Anything).
						Return("", cliErrs.ErrTfGitPatTokenFineGrained)
					return
				}

				if name == "ErrGithubTokenUnrecognized" {
					git.
						On("GetGitToken", mock.Anything).
						Return("", cliErrs.ErrTfGitPatTokenUnrecognized)
					return
				}

				git.
					On("GetGitToken", mock.Anything).
					Return("github_classic_test_token", nil)

				git.
					On("GetOrgAndRepoName", mock.Anything).
					Return("hashicorp", "tf-migrate")

				if name == "UnknownErrorOccurred" {
					githubUtil.
						On("GetRepository", mock.Anything, mock.Anything).
						Return(nil, nil, errors.New("unknown error occurred"))
					return
				}

				if name == "ErrServerError" {
					githubUtil.
						On("GetRepository", mock.Anything, mock.Anything).
						Return(nil, &github.Response{
							Response: serverErrorResponse,
						}, nil)
					return
				}

				if name == "ErrUnexpectedStatusCode" {
					githubUtil.
						On("GetRepository", mock.Anything, mock.Anything).
						Return(nil, &github.Response{
							Response: unexpectedStatusCodeResponse,
						}, nil)
					return
				}

				if name == "ErrTokenExpired" {
					githubUtil.
						On("GetRepository", mock.Anything, mock.Anything).
						Return(nil, &github.Response{
							Response: badCredentialsResponse,
						}, nil)
					return
				}

				if name == "ErrTokenDoesNotHaveAccessToOrg" {
					githubUtil.
						On("GetRepository", mock.Anything, mock.Anything).
						Return(nil, &github.Response{
							Response: resourceProtectedBySsoResponse,
						}, nil)
					return
				}

				if name == "ErrRepositoryNotFound" {
					githubUtil.
						On("GetRepository", mock.Anything, mock.Anything).
						Return(nil, &github.Response{
							Response: getMockResponse(http.StatusNotFound, repoNotFoundBody),
						}, nil)
				}

				if name == "ErrResponsePermissionsNil" {
					githubUtil.
						On("GetRepository", mock.Anything, mock.Anything).
						Return(responsePermissionsNil, nil, nil)
					return
				}

				if name == "ErrTokenDoesNotHaveReadPermission" {
					githubUtil.
						On("GetRepository", mock.Anything, mock.Anything).
						Return(repoWithNoReadOrWritePermissions, nil, nil)
					return
				}

				if name == "ErrTokenDoesNotHaveWritePermission" {
					githubUtil.
						On("GetRepository", mock.Anything, mock.Anything).
						Return(repoWithReadPermissionOnly, nil, nil)
					return
				}

				githubUtil.
					On("GetRepository", mock.Anything, mock.Anything).
					Return(repoWithReadAndWritePermissions, nil, nil)

			}()

			suggest, err := g.ValidateToken(repoUrl, repoIdentifier)
			r.Equal(tc.suggest, suggest)
			if err != nil {
				r.Error(err)
				r.Equal(tc.err, err)
				return
			}
			r.NoError(err)
			r.Empty(suggest)
		})
	}

}

func getMockResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
		Header: map[string][]string{
			"Content-Type": {"application/json"},
		},
	}
}
