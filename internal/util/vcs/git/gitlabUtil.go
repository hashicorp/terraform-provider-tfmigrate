package git

import (
	"context"
	"fmt"
	"os"

	cliErrs "terraform-provider-tfmigrate/internal/cli_errors"

	"github.com/hashicorp/terraform-plugin-log/tflog"
	gitlab "gitlab.com/gitlab-org/api/client-go"
)

type gitlabUtil struct {
	client *gitlab.Client
	ctx    context.Context
}

type GitlabUtil interface {
	GetProject(projectIdentifier string) (*gitlab.Project, *gitlab.Response, error)
}

var (
	gitlabPat TokenType = "gitlabToken"
)

// NewGitlabUtil creates a new instance of GitlabUtil.
func NewGitlabUtil(ctx context.Context) GitlabUtil {
	return &gitlabUtil{
		ctx: ctx,
	}
}

// GetProject fetches the repository details hosted on GitLab.
func (g *gitlabUtil) GetProject(projectIdentifier string) (*gitlab.Project, *gitlab.Response, error) {
	token, isSet := os.LookupEnv("TF_GIT_PAT_TOKEN")
	if !isSet {
		return nil, nil, cliErrs.ErrTfGitPatTokenNotSet
	}
	if token == "" {
		return nil, nil, cliErrs.ErrTfGitPatTokenEmpty
	}

	if g.client == nil {
		client, err := gitlab.NewClient(token)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create Gitlab client: %w", err)
		}
		g.client = client
	}

	repoDetails, response, err := g.client.Projects.GetProject(projectIdentifier, &gitlab.GetProjectOptions{})
	if err != nil {
		tflog.Error(g.ctx, "Failed to fetch project details", map[string]interface{}{
			"response": response,
			"error":    err,
		})
		return nil, response, err
	}

	tflog.Debug(g.ctx, "Fetched repository details", map[string]interface{}{
		"repoDetails": repoDetails,
	})
	return repoDetails, response, nil
}
