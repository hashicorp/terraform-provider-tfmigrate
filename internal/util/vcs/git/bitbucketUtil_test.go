package git

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	netMock "terraform-provider-tfmigrate/_mocks/net_mocks"
)

const (
	badCredentialsBodyBitbucket = `{
  "type": "error",
  "error": {
    "message": "Access token expired. Use your refresh token to obtain a new access token."
  }
}`

	repoDoesNotExistBodyBitbucket = `{
  "type": "error",
  "error": {
    "message": "Repository test-workspace/test-repo not found"
  }
}`

	resourceProtectedBySsoBodyBitbucket = `{
  "type": "error",
  "error": {
    "message": "Access denied. You must have write access to this repository."
  }
}`

	repoDetailsReadOnlyBitbucket = `{
    "name": "test-repo",
    "full_name": "test-workspace/test-repo",
    "private": true
}`

	repoDetailsReadWriteBitbucket = `{
    "name": "test-repo",
    "full_name": "test-workspace/test-repo",
    "private": true
}`
)

func TestNewBitbucketUtil(t *testing.T) {
	ctx := context.Background()

	util := NewBitbucketUtil(ctx)

	require.NotNil(t, util)
	require.IsType(t, &bitbucketUtil{}, util)
}

func TestCheckTokenTypeAndScopes(t *testing.T) {
	for name, tc := range map[string]struct {
		err        error
		statusCode int
		response   string
		scopes     string
		tokenType  string
	}{
		"unknown error": {
			err: errors.New("failed to make HTTP request"),
		},
		"expired token": {
			statusCode: http.StatusUnauthorized,
			response:   badCredentialsBodyBitbucket,
			scopes:     "",
			tokenType:  "",
		},
		"private repo": {
			statusCode: http.StatusNotFound,
			response:   repoDoesNotExistBodyBitbucket,
			scopes:     "",
			tokenType:  "",
		},
		"token not authorized to access org": {
			statusCode: http.StatusForbidden,
			response:   resourceProtectedBySsoBodyBitbucket,
			scopes:     "",
			tokenType:  "",
		},
		"success token has repository and pullrequest scopes": {
			statusCode: http.StatusOK,
			response:   repoDetailsReadOnlyBitbucket,
			scopes:     "repository pullrequest",
			tokenType:  "repo_access_token",
		},
		"success token has write scopes": {
			statusCode: http.StatusOK,
			response:   repoDetailsReadWriteBitbucket,
			scopes:     "repository:write pullrequest:write",
			tokenType:  "repo_access_token",
		},
		"success with missing scopes header": {
			statusCode: http.StatusOK,
			response:   repoDetailsReadOnlyBitbucket,
			scopes:     "",
			tokenType:  "repo_access_token",
		},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			mockHttpClient := getHttpClientWithMockRoundTripper()
			mockTransport := mockHttpClient.Transport.(*netMock.MockRoundTripper)
			bitbucketUtil := &bitbucketUtil{
				client: mockHttpClient,
				ctx:    ctx,
			}
			r := require.New(t)

			// setting up the behavior of the mocks
			func() {
				if name == "unknown error" {
					mockTransport.
						On("RoundTrip", mock.AnythingOfType("*http.Request")).
						Return(nil, errors.New("unknown error"))
				}

				if name == "expired token" {
					mockTransport.
						On("RoundTrip", mock.AnythingOfType("*http.Request")).
						Return(getBitbucketMockResponse(http.StatusUnauthorized, badCredentialsBodyBitbucket, "", ""), nil)
				}

				if name == "private repo" {
					mockTransport.
						On("RoundTrip", mock.AnythingOfType("*http.Request")).
						Return(getBitbucketMockResponse(http.StatusNotFound, repoDoesNotExistBodyBitbucket, "", ""), nil)
				}

				if name == "token not authorized to access org" {
					mockTransport.
						On("RoundTrip", mock.AnythingOfType("*http.Request")).
						Return(getBitbucketMockResponse(http.StatusForbidden, resourceProtectedBySsoBodyBitbucket, "", ""), nil)
				}

				if name == "success token has repository and pullrequest scopes" {
					mockTransport.
						On("RoundTrip", mock.AnythingOfType("*http.Request")).
						Return(getBitbucketMockResponse(http.StatusOK, repoDetailsReadOnlyBitbucket, "repository pullrequest", "repo_access_token"), nil)
				}

				if name == "success token has write scopes" {
					mockTransport.
						On("RoundTrip", mock.AnythingOfType("*http.Request")).
						Return(getBitbucketMockResponse(http.StatusOK, repoDetailsReadWriteBitbucket, "repository:write pullrequest:write", "repo_access_token"), nil)
				}

				if name == "success with missing scopes header" {
					mockTransport.
						On("RoundTrip", mock.AnythingOfType("*http.Request")).
						Return(getBitbucketMockResponse(http.StatusOK, repoDetailsReadOnlyBitbucket, "", "repo_access_token"), nil)
				}
			}()

			// act
			scopes, tokenType, resp, err := bitbucketUtil.CheckTokenTypeAndScopes("test-workspace", "test-repo", "test_token_123456789")

			// read the response body if it is not nil
			buf := new(bytes.Buffer)
			if resp != nil && resp.Body != nil {
				if _, err := io.Copy(buf, resp.Body); err != nil {
					t.Fatalf("error while reading response body: %s", err)
				}

				// log the response body when the response is not OK
				if resp.StatusCode != http.StatusOK {
					t.Logf("response body: %s", buf.String())
				}

				// close the response body
				if err := resp.Body.Close(); err != nil {
					t.Fatalf("error while closing response body: %s", err)
				}
			}

			// assert
			if err != nil {
				r.Error(err)
				if name == "unknown error" {
					r.Contains(err.Error(), tc.err.Error())
				} else {
					r.Equal(tc.err, err)
				}
			} else if resp != nil && resp.StatusCode != http.StatusOK {
				r.Equal(tc.statusCode, resp.StatusCode)
				r.JSONEq(tc.response, buf.String())
			} else {
				r.NoError(err)
				if resp != nil {
					r.Equal(http.StatusOK, resp.StatusCode)
				}
				r.Equal(tc.scopes, scopes)
				r.Equal(tc.tokenType, tokenType)
			}
		})
	}
}

func TestBitbucketUtil_ContainsScope(t *testing.T) {
	tests := []struct {
		name          string
		scopes        string
		requiredScope string
		expected      bool
	}{
		{
			name:          "scope found in comma-separated list",
			scopes:        "repository,pullrequest,issues",
			requiredScope: "pullrequest",
			expected:      true,
		},
		{
			name:          "scope found in space-separated list",
			scopes:        "repository pullrequest issues",
			requiredScope: "pullrequest",
			expected:      true,
		},
		{
			name:          "scope found in mixed-separated list",
			scopes:        "repository, pullrequest issues",
			requiredScope: "pullrequest",
			expected:      true,
		},
		{
			name:          "scope not found",
			scopes:        "repository,issues",
			requiredScope: "pullrequest",
			expected:      false,
		},
		{
			name:          "empty scopes",
			scopes:        "",
			requiredScope: "pullrequest",
			expected:      false,
		},
		{
			name:          "empty required scope",
			scopes:        "repository,pullrequest",
			requiredScope: "",
			expected:      false,
		},
		{
			name:          "exact match single scope",
			scopes:        "pullrequest",
			requiredScope: "pullrequest",
			expected:      true,
		},
		{
			name:          "partial match should not work",
			scopes:        "pullrequest:write",
			requiredScope: "pullrequest",
			expected:      false,
		},
		{
			name:          "scope with write permission",
			scopes:        "repository,pullrequest:write,issues",
			requiredScope: "pullrequest:write",
			expected:      true,
		},
		{
			name:          "multiple spaces and commas",
			scopes:        "repository,  pullrequest  , issues",
			requiredScope: "pullrequest",
			expected:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			util := &bitbucketUtil{
				ctx: ctx,
			}

			result := util.ContainsScope(tt.scopes, tt.requiredScope)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestSplitByCommaOrSpace(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "comma-separated",
			input:    "repository,pullrequest,issues",
			expected: []string{"repository", "pullrequest", "issues"},
		},
		{
			name:     "space-separated",
			input:    "repository pullrequest issues",
			expected: []string{"repository", "pullrequest", "issues"},
		},
		{
			name:     "mixed separators",
			input:    "repository, pullrequest issues",
			expected: []string{"repository", "pullrequest", "issues"},
		},
		{
			name:     "multiple spaces and commas",
			input:    "repository,  pullrequest  , issues",
			expected: []string{"repository", "pullrequest", "issues"},
		},
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "single scope",
			input:    "repository",
			expected: []string{"repository"},
		},
		{
			name:     "only separators",
			input:    ", , ",
			expected: nil,
		},
		{
			name:     "leading and trailing separators",
			input:    " ,repository,pullrequest, ",
			expected: []string{"repository", "pullrequest"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitByCommaOrSpace(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func getBitbucketMockResponse(statusCode int, body string, scopes string, tokenType string) *http.Response {
	headers := map[string][]string{
		ContentTypeHeader: {ApplicationJSONType},
	}

	if scopes != "" {
		headers[OauthScopesHeader] = []string{scopes}
	}

	if tokenType != "" {
		headers[CredentialTypeHeader] = []string{tokenType}
	}

	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
		Header:     headers,
	}
}
