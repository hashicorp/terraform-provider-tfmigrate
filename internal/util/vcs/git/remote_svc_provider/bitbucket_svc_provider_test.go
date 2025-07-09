package remote_svc_provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"terraform-provider-tfmigrate/internal/constants"

	"github.com/stretchr/testify/mock"

	gitMocks "terraform-provider-tfmigrate/_mocks/util_mocks/vcs_mocks/git_mocks"
	cliErrs "terraform-provider-tfmigrate/internal/cli_errors"

	"github.com/stretchr/testify/require"
)

const (
	bitbucketServerErrorResponseBody = `{
		"error": {
			"message": "Server error occurred"
		}
	}`

	bitbucketUnexpectedStatusCodeBody = `{
		"error": {
			"message": "Unexpected status code"
		}
	}`

	bitbucketBadCredentialsBody = `{
		"error": {
			"message": "Invalid credentials"
		}
	}`

	bitbucketResourceProtectedBody = `{
		"error": {
			"message": "Access forbidden"
		}
	}`

	bitbucketRepoNotFoundBody = `{
		"error": {
			"message": "Repository not found"
		}
	}`
)

var (
	bitbucketServerErrorResponse          = getMockResponse(http.StatusGatewayTimeout, bitbucketServerErrorResponseBody)
	bitbucketUnexpectedStatusCodeResponse = getMockResponse(http.StatusUnprocessableEntity, bitbucketUnexpectedStatusCodeBody)
	bitbucketBadCredentialsResponse       = getMockResponse(http.StatusUnauthorized, bitbucketBadCredentialsBody)
	bitbucketResourceProtectedResponse    = getMockResponse(http.StatusForbidden, bitbucketResourceProtectedBody)
	bitbucketRepoNotFoundResponse         = getMockResponse(http.StatusNotFound, bitbucketRepoNotFoundBody)

	bitbucketValidTokenResponse = getMockResponse(http.StatusOK, `{"name": "tf-migrate"}`)
)

func TestBitbucketValidateToken(t *testing.T) {
	repoUrl := "git@bitbucket.org:hashicorp/tf-migrate.git"
	repoIdentifier := "hashicorp/tf-migrate"

	for name, tc := range map[string]struct {
		err     error
		suggest string
	}{
		"UnknownGitServiceProvider": {
			err:     cliErrs.ErrGitServiceProviderNotSupported,
			suggest: constants.SuggestUsingSupportedVcsProvider,
		},
		"ErrBitbucketTokenNotSet": {
			err:     cliErrs.ErrTfGitPatTokenNotSet,
			suggest: constants.SuggestSettingValidTokenValue,
		},
		"ErrBitbucketTokenEmpty": {
			err:     cliErrs.ErrTfGitPatTokenEmpty,
			suggest: constants.SuggestSettingValidTokenValue,
		},
		"ErrBitbucketTokenInvalid": {
			err:     cliErrs.ErrTfGitPatTokenInvalid,
			suggest: constants.SuggestSettingValidTokenValue,
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
		"ErrBitbucketTokenTypeNotSupported": {
			err:     cliErrs.ErrBitbucketTokenTypeNotSupported,
			suggest: constants.SuggestSettingValidTokenValue,
		},
		"ErrTokenDoesNotHaveAccessToRepo": {
			err:     cliErrs.ErrTokenDoesNotHaveWritePermission,
			suggest: constants.SuggestProvidingRepoWritePermissionToToken,
		},
		"ErrNilResponseFromBitbucketAPI": {
			err:     cliErrs.ErrUnknownError,
			suggest: constants.SuggestUnknownErrorSolution,
		},
		"Success": {},
	} {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			ctx := context.Background()
			git := gitMocks.NewMockGitUtil(t)
			bitbucketUtil := gitMocks.NewMockBitbucketUtil(t)

			b := &bitbucketSvcProvider{
				ctx:           ctx,
				git:           git,
				bitbucketUtil: bitbucketUtil,
			}

			func() {
				if name == "UnknownGitServiceProvider" {
					git.
						On("GetRemoteServiceProvider", mock.Anything).
						Return(&constants.UnknownGitServiceProvider)
					git.
						On("GetGitToken", mock.Anything, mock.Anything).
						Return("", cliErrs.ErrGitServiceProviderNotSupported)
					return
				}

				git.
					On("GetRemoteServiceProvider", mock.Anything).
					Return(&constants.Bitbucket)

				if name == "ErrBitbucketTokenNotSet" {
					git.
						On("GetGitToken", mock.Anything, mock.Anything).
						Return("", cliErrs.ErrTfGitPatTokenNotSet)
					return
				}

				if name == "ErrBitbucketTokenEmpty" {
					git.
						On("GetGitToken", mock.Anything, mock.Anything).
						Return("", cliErrs.ErrTfGitPatTokenEmpty)
					return
				}

				if name == "ErrBitbucketTokenInvalid" {
					git.
						On("GetGitToken", mock.Anything, mock.Anything).
						Return("", cliErrs.ErrTfGitPatTokenInvalid)
					return
				}

				git.
					On("GetGitToken", mock.Anything, mock.Anything).
					Return("bitbucket_repo_access_token", nil)

				git.
					On("GetOrgAndRepoName", mock.Anything).
					Return("hashicorp", "tf-migrate")

				if name == "UnknownErrorOccurred" {
					bitbucketUtil.
						On("CheckTokenTypeAndScopes", mock.Anything, mock.Anything, mock.Anything).
						Return("", "", nil, errors.New("unknown error occurred"))
					return
				}

				if name == "ErrServerError" {
					bitbucketUtil.
						On("CheckTokenTypeAndScopes", mock.Anything, mock.Anything, mock.Anything).
						Return("", "", bitbucketServerErrorResponse, nil)
					return
				}

				if name == "ErrUnexpectedStatusCode" {
					bitbucketUtil.
						On("CheckTokenTypeAndScopes", mock.Anything, mock.Anything, mock.Anything).
						Return("", "", bitbucketUnexpectedStatusCodeResponse, nil)
					return
				}

				if name == "ErrTokenExpired" {
					bitbucketUtil.
						On("CheckTokenTypeAndScopes", mock.Anything, mock.Anything, mock.Anything).
						Return("", "", bitbucketBadCredentialsResponse, nil)
					return
				}

				if name == "ErrTokenDoesNotHaveAccessToOrg" {
					bitbucketUtil.
						On("CheckTokenTypeAndScopes", mock.Anything, mock.Anything, mock.Anything).
						Return("", "", bitbucketResourceProtectedResponse, nil)
					return
				}

				if name == "ErrRepositoryNotFound" {
					bitbucketUtil.
						On("CheckTokenTypeAndScopes", mock.Anything, mock.Anything, mock.Anything).
						Return("", "", bitbucketRepoNotFoundResponse, nil)
					return
				}

				if name == "ErrNilResponseFromBitbucketAPI" {
					bitbucketUtil.
						On("CheckTokenTypeAndScopes", mock.Anything, mock.Anything, mock.Anything).
						Return("", "", nil, nil)
					return
				}

				if name == "ErrBitbucketTokenTypeNotSupported" {
					bitbucketUtil.
						On("CheckTokenTypeAndScopes", mock.Anything, mock.Anything, mock.Anything).
						Return("repository,pullrequest", "app_password", bitbucketValidTokenResponse, nil)
					return
				}

				if name == "ErrTokenDoesNotHaveAccessToRepo" {
					bitbucketUtil.
						On("CheckTokenTypeAndScopes", mock.Anything, mock.Anything, mock.Anything).
						Return("issues", "repo_access_token", bitbucketValidTokenResponse, nil)
					bitbucketUtil.
						On("ContainsScope", "issues", "repository:write").
						Return(false)
					bitbucketUtil.
						On("ContainsScope", "issues", "pullrequest:write").
						Return(false)
					return
				}

				// Success case
				bitbucketUtil.
					On("CheckTokenTypeAndScopes", mock.Anything, mock.Anything, mock.Anything).
					Return("repository,pullrequest", "repo_access_token", bitbucketValidTokenResponse, nil)
				bitbucketUtil.
					On("ContainsScope", "repository,pullrequest", "repository:write").
					Return(true)
				bitbucketUtil.
					On("ContainsScope", "repository,pullrequest", "pullrequest:write").
					Return(true)
			}()

			suggest, err := b.ValidateToken(repoUrl, repoIdentifier, "token")
			r.Equal(tc.suggest, suggest)
			if tc.err != nil {
				r.Error(err)
				r.Equal(tc.err.Error(), err.Error())
				return
			}
			r.NoError(err)
			r.Empty(suggest)
		})
	}
}

func TestBitbucketValidateBitbucketTokenRepoAccess(t *testing.T) {
	for name, tc := range map[string]struct {
		scopes             string
		tokenType          string
		response           *http.Response
		apiError           error
		expectedError      error
		expectedStatusCode int
	}{
		"CheckTokenTypeAndScopesError": {
			scopes:             "",
			tokenType:          "",
			response:           nil,
			apiError:           errors.New("API error"),
			expectedError:      errors.New("API error"),
			expectedStatusCode: 0,
		},
		"NilResponse": {
			scopes:             "",
			tokenType:          "",
			response:           nil,
			apiError:           nil,
			expectedError:      cliErrs.ErrUnknownError,
			expectedStatusCode: 0,
		},
		"ServerError": {
			scopes:             "",
			tokenType:          "",
			response:           bitbucketServerErrorResponse,
			apiError:           nil,
			expectedError:      cliErrs.ErrServerError,
			expectedStatusCode: http.StatusGatewayTimeout,
		},
		"UnauthorizedError": {
			scopes:             "",
			tokenType:          "",
			response:           bitbucketBadCredentialsResponse,
			apiError:           nil,
			expectedError:      cliErrs.ErrTokenExpired,
			expectedStatusCode: http.StatusUnauthorized,
		},
		"ForbiddenError": {
			scopes:             "",
			tokenType:          "",
			response:           bitbucketResourceProtectedResponse,
			apiError:           nil,
			expectedError:      cliErrs.ErrTokenDoesNotHaveAccessToOrg,
			expectedStatusCode: http.StatusForbidden,
		},
		"NotFoundError": {
			scopes:             "",
			tokenType:          "",
			response:           bitbucketRepoNotFoundResponse,
			apiError:           nil,
			expectedError:      cliErrs.ErrRepositoryNotFound,
			expectedStatusCode: http.StatusNotFound,
		},
		"UnexpectedStatusCode": {
			scopes:             "",
			tokenType:          "",
			response:           bitbucketUnexpectedStatusCodeResponse,
			apiError:           nil,
			expectedError:      cliErrs.ErrUnexpectedStatusCode,
			expectedStatusCode: http.StatusUnprocessableEntity,
		},
		"InvalidTokenType": {
			scopes:             "repository,pullrequest",
			tokenType:          "app_password",
			response:           bitbucketValidTokenResponse,
			apiError:           nil,
			expectedError:      cliErrs.ErrBitbucketTokenTypeNotSupported,
			expectedStatusCode: http.StatusOK,
		},
		"NoRequiredScopes": {
			scopes:             "issues",
			tokenType:          "repo_access_token",
			response:           bitbucketValidTokenResponse,
			apiError:           nil,
			expectedError:      cliErrs.ErrTokenDoesNotHaveWritePermission,
			expectedStatusCode: http.StatusOK,
		},
		"ValidTokenWithPullRequestWriteScope": {
			scopes:             "pullrequest:write",
			tokenType:          "repo_access_token",
			response:           bitbucketValidTokenResponse,
			apiError:           nil,
			expectedError:      nil,
			expectedStatusCode: http.StatusOK,
		},
		"ValidTokenWithRepositoryReadScope": {
			scopes:             "repository",
			tokenType:          "repo_access_token",
			response:           bitbucketValidTokenResponse,
			apiError:           nil,
			expectedError:      cliErrs.ErrTokenDoesNotHaveWritePermission,
			expectedStatusCode: http.StatusOK,
		},
		"ValidTokenWithRepositoryWriteScope": {
			scopes:             "repository:write",
			tokenType:          "repo_access_token",
			response:           bitbucketValidTokenResponse,
			apiError:           nil,
			expectedError:      cliErrs.ErrTokenDoesNotHavePrWritePermission,
			expectedStatusCode: http.StatusOK,
		},
		"ValidTokenWithAllRequiredScopes": {
			scopes:             "repository,pullrequest",
			tokenType:          "repo_access_token",
			response:           bitbucketValidTokenResponse,
			apiError:           nil,
			expectedError:      nil,
			expectedStatusCode: http.StatusOK,
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			ctx := context.Background()
			bitbucketUtil := gitMocks.NewMockBitbucketUtil(t)

			b := &bitbucketSvcProvider{
				ctx:           ctx,
				bitbucketUtil: bitbucketUtil,
			}

			workspace := "hashicorp"
			repoSlug := "tf-migrate"
			token := "test_token"

			bitbucketUtil.
				On("CheckTokenTypeAndScopes", workspace, repoSlug, token).
				Return(tc.scopes, tc.tokenType, tc.response, tc.apiError)

			// Mock ContainsScope call for scope validation
			if tc.response != nil && tc.response.StatusCode == http.StatusOK && tc.tokenType == "repo_access_token" {
				// Mock the calls for required scopes - implementation checks them sequentially and returns early
				hasPullRequestWrite := tc.scopes == "pullrequest:write" || tc.scopes == "repository,pullrequest"
				hasRepositoryWrite := tc.scopes == "repository:write" || tc.scopes == "repository,pullrequest"

				// Always mock the first scope check (repository:write)
				bitbucketUtil.
					On("ContainsScope", tc.scopes, "repository:write").
					Return(hasRepositoryWrite)

				// Always check pullrequest:write regardless of repository:write status
				bitbucketUtil.
					On("ContainsScope", tc.scopes, "pullrequest:write").
					Return(hasPullRequestWrite)
			}

			statusCode, err := b.validateBitbucketTokenRepoAccess(workspace, repoSlug, token)

			r.Equal(tc.expectedStatusCode, statusCode)
			if tc.expectedError != nil {
				r.Error(err)
				r.Equal(tc.expectedError.Error(), err.Error())
			} else {
				r.NoError(err)
			}
		})
	}
}
