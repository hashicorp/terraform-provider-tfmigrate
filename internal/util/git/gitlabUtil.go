package git

import (
	"context"
	"fmt"
	"os"

	cliErrs "terraform-provider-tfmigrate/internal/cli_errors"

	"github.com/hashicorp/go-hclog"
	gitlab "gitlab.com/gitlab-org/api/client-go"
)

type gitlabUtil struct {
	client *gitlab.Client
	ctx    context.Context
	logger hclog.Logger
}

type GitlabUtil interface {
	GetProject(projectIdentifier string) (*gitlab.Project, *gitlab.Response, error)
}

var (
	gitlabPat TokenType = "gitlabToken"
)

// NewGitlabUtil creates a new instance of GitlabUtil.
func NewGitlabUtil(ctx context.Context, logger hclog.Logger) GitlabUtil {
	return &gitlabUtil{
		ctx:    ctx,
		logger: logger,
	}
}

// GetProject fetches the repository details hosted on GitLab.
func (g *gitlabUtil) GetProject(projectIdentifier string) (*gitlab.Project, *gitlab.Response, error) {
	token, isSet := os.LookupEnv("TF_GIT_PAT_TOKEN")
	if !isSet {
		return nil, nil, cliErrs.ErrGitlabTokenNotSet
	}
	if token == "" {
		return nil, nil, cliErrs.ErrGitlabTokenEmpty
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
		g.logger.Error(fmt.Sprintf("Failed to fetch project details. response: %v, err: %v", response, err))
		return nil, response, err
	}

	g.logger.Debug(fmt.Sprintf("Fetched repository details: %v", repoDetails))
	return repoDetails, response, nil
}
