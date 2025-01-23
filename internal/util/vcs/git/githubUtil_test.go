package git

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"testing"

	cliErrs "terraform-provider-tfmigrate/internal/cli_errors"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	netMock "terraform-provider-tfmigrate/_mocks/net_mocks"

	"github.com/google/go-github/v66/github"
	"github.com/hashicorp/go-hclog"
)

const (
	badCredentialsBody = `{
  "message": "Bad credentials",
  "documentation_url": "https://docs.github.com/rest",
  "status": "401"
}`

	repoDoesNotExistBody = `{
  "message": "Not Found",
  "documentation_url": "https://docs.github.com/rest/repos/repos#get-a-repository",
  "status": "404"
}`

	resourceProtectedBySsoBody = `{
  "message": "Resource protected by organization SAML enforcement. You must grant your Personal Access token access to an organization within this enterprise.",
  "documentation_url": "https://docs.github.com/articles/authenticating-to-a-github-organization-with-saml-single-sign-on/",
  "status": "403"
}`

	repoDetailsReadOnly = `{
    "name": "test-repo",
    "owner": {
        "login": "test-owner"
    },
    "private": true,
    "permissions": {
        "admin": false,
        "maintain": false,
        "pull": true,
        "push": false,
        "triage": false
    }
}`

	repoDetailsReadWrite = `{
	"name": "test-repo",
	"owner": {
		"login": "test-owner"
	},
	"private": true,
	"permissions": {
		"admin": false,
		"maintain": false,
		"pull": true,
		"push": true,
		"triage": false
	}
}`
)

func TestGetRepository(t *testing.T) {
	var token string
	for name, tc := range map[string]struct {
		err        error
		statusCode int
		repoData   *github.Repository
		response   string
	}{
		"token not set": {
			err: cliErrs.ErrTfGitPatTokenNotSet,
		},
		"token empty": {
			err: cliErrs.ErrTfGitPatTokenEmpty,
		},
		"unknown error": {
			err: &url.Error{
				Op:  "Get",
				URL: "https://api.github.com/repos/test-owner/test-repo",
				Err: errors.New("unknown error"),
			},
		},
		"expired token": {
			statusCode: http.StatusUnauthorized,
			response:   badCredentialsBody,
		},
		"private repo": {
			statusCode: http.StatusNotFound,
			response:   repoDoesNotExistBody,
		},
		"token not authorized to access org": {
			statusCode: http.StatusForbidden,
			response:   resourceProtectedBySsoBody,
		},
		"success token has read-only permission": {
			statusCode: http.StatusOK,
			repoData: &github.Repository{
				Name:    github.String("test-repo"),
				Owner:   &github.User{Login: github.String("test-owner")},
				Private: github.Bool(true),
				Permissions: map[string]bool{
					"admin":    false,
					"maintain": false,
					"pull":     true,
					"push":     false,
					"triage":   false,
				},
			},
			response: repoDetailsReadOnly,
		},
		"success token has read-write permission": {
			statusCode: http.StatusOK,
			repoData: &github.Repository{
				Name:    github.String("test-repo"),
				Owner:   &github.User{Login: github.String("test-owner")},
				Private: github.Bool(true),
				Permissions: map[string]bool{
					"admin":    false,
					"maintain": false,
					"pull":     true,
					"push":     true,
					"triage":   false,
				},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			mockLogger := hclog.FromContext(ctx)
			mockHttpClient := getHttpClientWithMockRoundTripper()

			mockTransport, ok := mockHttpClient.Transport.(*netMock.MockRoundTripper)
			require.True(t, ok, "expected Transport to be of type *netMock.MockRoundTripper, but got %T", mockHttpClient.Transport)

			githubClient := github.NewClient(mockHttpClient)
			githubUtil := &githubUtil{
				client: githubClient,
				logger: mockLogger,
				ctx:    ctx,
			}
			token = os.Getenv("TF_GIT_PAT_TOKEN")
			r := require.New(t)

			// setting up the behavior of the mocks
			func() {
				if name == "token not set" {
					if err := os.Unsetenv("TF_GIT_PAT_TOKEN"); err != nil {
						t.Fatalf("error while unsetting environment variable: %v", err)
					}
					return
				}

				if name == "token empty" {
					if err := os.Setenv("TF_GIT_PAT_TOKEN", ""); err != nil {
						t.Fatalf("error while setting environment variable: %v", err)
					}
					return
				}

				if err := os.Setenv("TF_GIT_PAT_TOKEN", "ghp_aBp47xyKz45uXXf3JfDrg8w22EfFgY1ytpLM"); err != nil { // NOSONAR
					t.Fatalf("error while setting environment variable: %v", err)
				}

				if name == "unknown error" {
					mockTransport.
						On("RoundTrip", mock.AnythingOfType("*http.Request")).
						Return(nil, errors.New("unknown error"))
				}

				if name == "expired token" {
					mockTransport.
						On("RoundTrip", mock.AnythingOfType("*http.Request")).
						Return(getMockResponse(http.StatusUnauthorized, badCredentialsBody), nil)
				}

				if name == "private repo" {
					mockTransport.
						On("RoundTrip", mock.AnythingOfType("*http.Request")).
						Return(getMockResponse(http.StatusNotFound, repoDoesNotExistBody), nil)
				}

				if name == "token not authorized to access org" {
					mockTransport.
						On("RoundTrip", mock.AnythingOfType("*http.Request")).
						Return(getMockResponse(http.StatusForbidden, resourceProtectedBySsoBody), nil)
				}

				if name == "success token has read-only permission" {
					mockTransport.
						On("RoundTrip", mock.AnythingOfType("*http.Request")).
						Return(getMockResponse(http.StatusOK, repoDetailsReadOnly), nil)
				}

				if name == "success token has read-write permission" {
					mockTransport.
						On("RoundTrip", mock.AnythingOfType("*http.Request")).
						Return(getMockResponse(http.StatusOK, repoDetailsReadWrite), nil)
				}
			}()

			// act
			repoData, resp, err := githubUtil.GetRepository("test-owner", "test-repo")

			// read the response body if it is not nil
			buf := new(bytes.Buffer)
			if resp != nil && resp.Body != nil && name != "token not set" && name != "token empty" {
				if _, err = io.Copy(buf, resp.Body); err != nil {
					t.Fatalf("error while reading response body: %s", err)
				}

				// log the response body when the response is not OK
				if resp.StatusCode != http.StatusOK {
					t.Logf("response body: %s", buf.String())
				}

				// close the response body
				if err = resp.Body.Close(); err != nil {
					t.Fatalf("error while closing response body: %s", err)
				}
			}

			// assert
			if err != nil {
				r.Error(err)
				r.Equal(tc.err, err)
			} else if resp.StatusCode != http.StatusOK {
				r.Equal(tc.statusCode, resp.StatusCode)
				r.JSONEq(tc.response, buf.String())
			} else {
				r.NoError(err)
				r.Equal(http.StatusOK, resp.StatusCode)
				r.Equal(tc.repoData, repoData)
			}
		})

		t.Cleanup(func() {
			if err := os.Setenv("TF_GIT_PAT_TOKEN", token); err != nil {
				t.Fatalf("error while setting environment variable: %v", err)
			}
		})
	}

}

func getHttpClientWithMockRoundTripper() *http.Client {
	mockRoundTripper := new(netMock.MockRoundTripper)
	mockClient := &http.Client{Transport: mockRoundTripper}
	return mockClient
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
