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

	netMock "terraform-provider-tfmigrate/_mocks/util_mocks/net_mocks"
	cliErrs "terraform-provider-tfmigrate/internal/cli_errors"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/hashicorp/go-hclog"
	gitlab "gitlab.com/gitlab-org/api/client-go"
)

const (
	gitlabBadTokenBody = `{"message":"401 Unauthorized"}`
	gitlabNotFoundBody = `{"message":"404 Project Not Found"}`
	gitlabSuccessBody  = `{
		"id": 123,
		"name": "test-project",
		"path_with_namespace": "test-owner/test-project",
		"visibility": "private"
	}`
)

func TestGetProject(t *testing.T) {
	var token string
	for name, tc := range map[string]struct {
		err        error
		statusCode int
		project    *gitlab.Project
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
				URL: "https://gitlab.com/api/v4/projects/test-owner%2Ftest-project",
				Err: errors.New("unknown error"),
			},
		},
		"success": {
			statusCode: http.StatusOK,
			project: &gitlab.Project{
				ID:                123,
				Name:              "test-project",
				PathWithNamespace: "test-owner/test-project",
				Visibility:        gitlab.PrivateVisibility,
			},
			response: gitlabSuccessBody,
		},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			mockLogger := hclog.FromContext(ctx)
			mockHttpClient := getHttpClientWithMockRoundTripper()
			mockTransport := mockHttpClient.Transport.(*netMock.MockRoundTripper)
			gitlabClient, _ := gitlab.NewClient("", gitlab.WithHTTPClient(mockHttpClient))
			gitlabUtil := &gitlabUtil{
				client: gitlabClient,
				logger: mockLogger,
				ctx:    ctx,
			}
			token = os.Getenv("TF_GIT_PAT_TOKEN")
			r := require.New(t)

			// Set up the mocks
			func() {
				if name == "token not set" {
					_ = os.Unsetenv("TF_GIT_PAT_TOKEN")
					return
				}

				if name == "token empty" {
					_ = os.Setenv("TF_GIT_PAT_TOKEN", "")
					return
				}

				_ = os.Setenv("TF_GIT_PAT_TOKEN", "glpat-abc123")

				switch name {
				case "unknown error":
					mockTransport.On("RoundTrip", mock.AnythingOfType("*http.Request")).Return(nil, errors.New("unknown error"))
				case "success":
					mockTransport.On("RoundTrip", mock.AnythingOfType("*http.Request")).Return(getMockResponse(http.StatusOK, gitlabSuccessBody), nil)
				}
			}()

			// Act
			project, resp, err := gitlabUtil.GetProject("test-owner/test-project")

			// Read response body if applicable
			buf := new(bytes.Buffer)
			if resp != nil && resp.Body != nil && name != "token not set" && name != "token empty" {
				_, _ = io.Copy(buf, resp.Body)
				_ = resp.Body.Close()
			}

			// Assert
			if err != nil {
				r.Error(err)
				r.Equal(tc.err, err)
			} else if resp.StatusCode != http.StatusOK {
				r.Equal(tc.statusCode, resp.StatusCode)
				r.JSONEq(tc.response, buf.String())
			} else {
				r.NoError(err)
				r.Equal(http.StatusOK, resp.StatusCode)
				r.Equal(tc.project, project)
			}
		})

		t.Cleanup(func() {
			_ = os.Setenv("TF_GIT_PAT_TOKEN", token)
		})
	}
}
