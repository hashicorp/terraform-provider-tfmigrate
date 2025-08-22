package remote_svc_provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	cliErrs "terraform-provider-tfmigrate/internal/cli_errors"
	"terraform-provider-tfmigrate/internal/constants"
	"terraform-provider-tfmigrate/internal/util/net"
	"terraform-provider-tfmigrate/internal/util/vcs/git"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// bitbucketSvcProvider implements BitbucketSvcProvider.
type bitbucketSvcProvider struct {
	ctx           context.Context
	git           git.GitUtil
	bitbucketUtil git.BitbucketUtil
	httpClient    net.Client
}
type Response struct {
	*http.Response
}

// BitbucketSvcProvider extends RemoteVcsSvcProvider for Bitbucket-specific token validation.
type BitbucketSvcProvider interface {
	RemoteVcsSvcProvider
}

// NewBitbucketSvcProvider creates a new instance of BitbucketSvcProvider.
func NewBitbucketSvcProvider(ctx context.Context, gitUtil git.GitUtil, bitbucketUtil git.BitbucketUtil) BitbucketSvcProvider {
	return &bitbucketSvcProvider{
		ctx:           ctx,
		git:           gitUtil,
		bitbucketUtil: bitbucketUtil,
		httpClient:    net.NewClient(ctx),
	}
}

// ValidateToken checks if the Bitbucket token is set and valid.
func (b *bitbucketSvcProvider) ValidateToken(repoUrl string, repoIdentifier string, tokenFromProvider string) (string, error) {
	// do something with the token
	if _, err := b.git.GetGitToken(b.git.GetRemoteServiceProvider(repoUrl), tokenFromProvider); err != nil {
		return gitTokenErrorHandler(err)
	}
	orgName, repoName := b.git.GetOrgAndRepoName(repoIdentifier)

	if statusCode, err := b.validateBitbucketTokenRepoAccess(orgName, repoName, tokenFromProvider); err != nil {
		return gitTokenErrorHandler(err, statusCode)
	}
	return "", nil
}

// validateBitbucketTokenRepoAccess checks if the Bitbucket App Password has access to the repository and PR permissions.
func (b *bitbucketSvcProvider) validateBitbucketTokenRepoAccess(owner, repo, token string) (int, error) {
	scopes, tokenType, resp, err := b.bitbucketUtil.CheckTokenTypeAndScopes(owner, repo, token)
	if err != nil {
		tflog.Error(b.ctx, fmt.Sprintf("error checking token type and scopes: %v", err))
		return 0, err
	}
	if resp == nil {
		tflog.Error(b.ctx, "received nil response from Bitbucket API")
		return 0, cliErrs.ErrUnknownError
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		tflog.Error(b.ctx, fmt.Sprintf("error fetching repository details: %s", resp.Status))
		return handleNonSuccessResponseFromVcsApi(resp)
	}

	if tokenType != git.TokenTypeRepoAccessToken {
		return http.StatusOK, cliErrs.ErrBitbucketTokenTypeNotSupported
	}

	hasRepoWrite := b.bitbucketUtil.ContainsScope(scopes, git.ScopeRepositoryWrite)
	hasPrWrite := b.bitbucketUtil.ContainsScope(scopes, git.ScopePullRequestWrite)

	if !hasRepoWrite && !hasPrWrite {
		return http.StatusOK, cliErrs.ErrTokenDoesNotHaveWritePermission
	}

	if !hasPrWrite {
		return http.StatusOK, cliErrs.ErrTokenDoesNotHavePrWritePermission
	}
	return http.StatusOK, nil
}

// CreatePullRequest creates a pull request on the Bitbucket repository.
func (b *bitbucketSvcProvider) CreatePullRequest(params git.PullRequestParams) (string, error) {

	parts := strings.Split(params.RepoIdentifier, "/")
	if len(parts) != 2 {
		return "", fmt.Errorf(constants.ErrBitbucketInvalidRepoIdentifier)
	}
	owner := parts[0]
	repo := parts[1]

	url := fmt.Sprintf(git.BitbucketPullRequestAPIURL, git.ApiVersion, owner, repo)

	payload := map[string]any{
		"title": params.Title,
		"source": map[string]any{
			"branch": map[string]any{
				"name": params.FeatureBranch,
			},
		},
		"destination": map[string]any{
			"branch": map[string]any{
				"name": params.BaseBranch,
			},
		},
		"description":         params.Body,
		"close_source_branch": false,
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf(constants.ErrBitbucketMarshalPayload, err)
	}

	headers := map[string]string{
		git.AuthorizationHeader: git.BearerPrefix + params.GitPatToken,
		git.AcceptHeader:        git.ApplicationJSONType,
		git.ContentTypeHeader:   git.ApplicationJSONType,
	}

	postRequest := net.RequestOptions{
		Method:  http.MethodPost,
		URL:     url,
		Headers: headers,
		Body:    strings.NewReader(string(jsonPayload)),
	}

	resp, err := b.httpClient.Do(postRequest)
	if err != nil {
		return "", fmt.Errorf(constants.ErrBitbucketSendHTTPRequest, err)
	}

	defer resp.Body.Close()

	responseBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	responseBody := string(responseBytes)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		tflog.Error(b.ctx, git.CreatePullRequestFailedLog, map[string]any{
			"owner":      owner,
			"repo":       repo,
			"title":      params.Title,
			"statusCode": resp.StatusCode,
			"body":       responseBody,
		})
		return "", fmt.Errorf(constants.ErrBitbucketCreatePullRequestFailed, responseBody)
	}

	var result map[string]any
	if err := json.Unmarshal(responseBytes, &result); err != nil {
		return "", fmt.Errorf(constants.ErrBitbucketParseResponse, err)
	}

	if links, ok := result["links"].(map[string]any); ok {
		if html, ok := links["html"].(map[string]any); ok {
			if href, ok := html["href"].(string); ok {
				return href, nil
			}
		}
	}

	return "", fmt.Errorf(constants.ErrBitbucketExtractPullRequestURL)
}
