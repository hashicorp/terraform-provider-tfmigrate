package git

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	netUtil "terraform-provider-tfmigrate/internal/util/net"
)

// Package level constants for Bitbucket operations.
const (
	BitbucketRepositoryAPIURL  = "https://api.bitbucket.org/%s/repositories/%s/%s"
	BitbucketPullRequestAPIURL = "https://api.bitbucket.org/%s/repositories/%s/%s/pullrequests"
	OauthScopesHeader          = "X-Oauth-Scopes"
	CredentialTypeHeader       = "X-Credential-Type"
	CreateRequestFailedLog     = "failed to create HTTP request"
	MakeRequestFailedLog       = "failed to make HTTP request"
	CreatePullRequestFailedLog = "Failed to create pull request"
	ApiVersion                 = "2.0"

	// HTTP headers.
	AuthorizationHeader = "Authorization"
	AcceptHeader        = "Accept"
	ContentTypeHeader   = "Content-Type"
	ApplicationJSONType = "application/json"
	BearerPrefix        = "Bearer "

	// Bitbucket scopes.
	ScopePullRequestWrite    = "pullrequest:write"
	ScopeRepositoryWrite     = "repository:write"
	TokenTypeRepoAccessToken = "repo_access_token"
)

type bitbucketUtil struct {
	httpClient netUtil.Client
	ctx        context.Context
}

type BitbucketUtil interface {
	CheckTokenTypeAndScopes(workspace, repoSlug, accessToken string) (string, string, *http.Response, error)
	ContainsScope(scopes, required string) bool
}

// NewBitbucketUtil creates a new instance of BitbucketUtil.
func NewBitbucketUtil(ctx context.Context) BitbucketUtil {
	return &bitbucketUtil{
		ctx:        ctx,
		httpClient: netUtil.NewClient(),
	}
}

// GetRepository fetches the repository details hosted on Bitbucket.

func (b *bitbucketUtil) CheckTokenTypeAndScopes(workspace, repoSlug, accessToken string) (string, string, *http.Response, error) {
	// Local constants for strings used in this function
	url := fmt.Sprintf(BitbucketRepositoryAPIURL, ApiVersion, workspace, repoSlug)

	headers := map[string]string{
		AuthorizationHeader: BearerPrefix + accessToken,
		AcceptHeader:        ApplicationJSONType,
	}

	resp, err := b.httpClient.Do(b.ctx, netUtil.RequestOptions{
		Method:  http.MethodGet,
		URL:     url,
		Headers: headers,
	})
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to make HTTP request: %w", err)
	}

	// Extract scopes and token type from response headers
	scopes := resp.Header.Get(OauthScopesHeader)
	tokenType := resp.Header.Get(CredentialTypeHeader)

	return scopes, tokenType, resp, nil
}

// ContainsScope checks if a scope is present in a comma or space separated list.
func (b *bitbucketUtil) ContainsScope(scopes, requiredScope string) bool {
	return slices.Contains(splitByCommaOrSpace(scopes), requiredScope)
}

// splitByCommaOrSpace splits a string by comma or space.
func splitByCommaOrSpace(scopes string) []string {
	result := strings.FieldsFunc(scopes, func(r rune) bool {
		return r == ',' || r == ' '
	})
	if len(result) == 0 {
		return nil
	}
	return result
}
