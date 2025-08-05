package git

import (
	"context"
	"fmt"
	"os"

	cliErrs "terraform-provider-tfmigrate/internal/cli_errors"
	consts "terraform-provider-tfmigrate/internal/constants"

	"github.com/google/go-github/v66/github"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

type githubUtil struct {
	client *github.Client
	ctx    context.Context
}

type GithubUtil interface {
	GetRepository(owner string, repo string) (*github.Repository, *github.Response, error)
}

// NewGithubUtil creates a new instance of GithubUtil.
func NewGithubUtil(ctx context.Context) GithubUtil {
	return &githubUtil{
		ctx: ctx,
	}
}

// GetRepository fetches the repository details hosted on GitHub.
func (g *githubUtil) GetRepository(owner string, repo string) (*github.Repository, *github.Response, error) {
	// Note: As of now, the token checking is redundant as the token is checked before this function is called.
	// However, it is kept here for the completeness of the code.

	token, isSet := os.LookupEnv(consts.GitTokenEnvName)
	if !isSet {
		return nil, nil, fmt.Errorf(string(cliErrs.ErrTfGitPatTokenNotSet), consts.GitTokenEnvName)
	}

	if token == "" {
		return nil, nil, fmt.Errorf(string(cliErrs.ErrTfGitPatTokenEmpty), consts.GitTokenEnvName)
	}

	if g.client == nil {
		g.client = github.NewClient(nil).WithAuthToken(token)
	}

	repoDetails, response, err := g.client.Repositories.Get(g.ctx, owner, repo)
	if repoDetails != nil {
		tflog.Debug(g.ctx, fmt.Sprintf("Fetched repository details: %v", repoDetails))
		return repoDetails, response, err
	}
	tflog.Error(g.ctx, fmt.Sprintf("Failed to fetch repository details. response: %v, err: %v", response, err))
	return nil, response, err
}
