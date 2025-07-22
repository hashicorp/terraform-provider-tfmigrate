package git

import (
	"context"
	"fmt"
	"net/http"
	"slices"

	"terraform-provider-tfmigrate/internal/constants"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Package level constants for Bitbucket operations.
const (
	BitbucketRepositoryAPIURL  = "https://api.bitbucket.org/2.0/repositories/%s/%s"
	BitbucketPullRequestAPIURL = "https://api.bitbucket.org/2.0/repositories/%s/%s/pullrequests"
	OauthScopesHeader          = "X-Oauth-Scopes"
	CredentialTypeHeader       = "X-Credential-Type"
	CreateRequestFailedLog     = "failed to create HTTP request"
	MakeRequestFailedLog       = "failed to make HTTP request"
	CreatePullRequestFailedLog = "Failed to create pull request"

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
	client *http.Client
	ctx    context.Context
}

type BitbucketUtil interface {
	CheckTokenTypeAndScopes(workspace, repoSlug, accessToken string) (string, string, *http.Response, error)
	ContainsScope(scopes, required string) bool
}

// NewBitbucketUtil creates a new instance of BitbucketUtil.
func NewBitbucketUtil(ctx context.Context) BitbucketUtil {
	return &bitbucketUtil{
		ctx: ctx,
	}
}

// GetRepository fetches the repository details hosted on Bitbucket.

func (b *bitbucketUtil) CheckTokenTypeAndScopes(workspace, repoSlug, accessToken string) (string, string, *http.Response, error) {
	// Local constants for strings used in this function
	const (
		bitbucketRepositoryAPIURL = "https://api.bitbucket.org/2.0/repositories/%s/%s"
	)

	url := fmt.Sprintf(bitbucketRepositoryAPIURL, workspace, repoSlug)
	req, err := http.NewRequestWithContext(b.ctx, http.MethodGet, url, nil)
	if err != nil {
		tflog.Error(b.ctx, CreateRequestFailedLog, map[string]any{
			"workspace":   workspace,
			"repoSlug":    repoSlug,
			"error":       err,
			"accessToken": accessToken,
		})
		return "", "", nil, fmt.Errorf(constants.ErrBitbucketCreateHTTPRequestUtil, err)
	}
	req.Header.Set(AuthorizationHeader, BearerPrefix+accessToken)
	req.Header.Set(AcceptHeader, ApplicationJSONType)

	client := b.client
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		tflog.Error(b.ctx, MakeRequestFailedLog, map[string]any{
			"workspace":   workspace,
			"repoSlug":    repoSlug,
			"error":       err,
			"accessToken": accessToken,
		})
		return "", "", nil, fmt.Errorf(constants.ErrBitbucketMakeHTTPRequestUtil, err)
	}
	defer resp.Body.Close()

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
	var result []string
	current := ""
	for _, r := range scopes {
		if r == ',' || r == ' ' {
			if current != "" {
				result = append(result, current)
				current = ""
			}
		} else {
			current += string(r)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}
