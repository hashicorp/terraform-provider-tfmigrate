package git

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"terraform-provider-tfmigrate/internal/util/net"
	"time"
)

// Package level constants for Bitbucket operations.
const (
	BitbucketRepositoryAPIURL  = "https://api.bitbucket.org/%s/repositories/%s/%s"
	BitbucketPullRequestAPIURL = "https://api.bitbucket.org/2.0/repositories/%s/%s/pullrequests"
	OauthScopesHeader          = "X-Oauth-Scopes"
	CredentialTypeHeader       = "X-Credential-Type"
	CreateRequestFailedLog     = "failed to create HTTP request"
	MakeRequestFailedLog       = "failed to make HTTP request"
	CreatePullRequestFailedLog = "Failed to create pull request"
	apiVersion                 = "2.0"

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
	httpClient net.HttpClient
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
		httpClient: net.NewHttpClient(30 * time.Second),
	}
}

// GetRepository fetches the repository details hosted on Bitbucket.

func (b *bitbucketUtil) CheckTokenTypeAndScopes(workspace, repoSlug, accessToken string) (string, string, *http.Response, error) {
	// Local constants for strings used in this function
	url := fmt.Sprintf(BitbucketRepositoryAPIURL, apiVersion, workspace, repoSlug)

	headers := map[string]string{
		AuthorizationHeader: BearerPrefix + accessToken,
		AcceptHeader:        ApplicationJSONType,
	}

	statusCode, responseBody, responseHeaders, err := b.httpClient.DoRequest(b.ctx, http.MethodGet, url, headers, nil)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to make HTTP request: %w", err)
	}

	// Create a response object with the actual response body for compatibility
	resp := &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(bytes.NewReader(responseBody)),
		Header:     responseHeaders,
	}

	// Extract scopes and token type from response headers
	scopes := responseHeaders.Get(OauthScopesHeader)
	tokenType := responseHeaders.Get(CredentialTypeHeader)

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
