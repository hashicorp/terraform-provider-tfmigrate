package git

import (
	"context"
	"fmt"
	"os"

	cliErrs "terraform-provider-tfmigrate/internal/cli_errors"

	"github.com/google/go-github/v66/github"
	"github.com/hashicorp/go-hclog"
)

type githubUtil struct {
	client *github.Client
	logger hclog.Logger
	ctx    context.Context
}

type TokenType string

type GithubUtil interface {
	GetRepository(owner string, repo string) (*github.Repository, *github.Response, error)
}

var (
	ClassicToken     TokenType = "classic"
	FineGrainedToken TokenType = "fine-grained"
	Unrecognized     TokenType = "unrecognized"
)

// NewGithubUtil creates a new instance of GithubUtil.
func NewGithubUtil(ctx context.Context, logger hclog.Logger) GithubUtil {
	return &githubUtil{
		ctx:    ctx,
		logger: logger,
	}
}

// GetRepository fetches the repository details hosted on GitHub.
func (g *githubUtil) GetRepository(owner string, repo string) (*github.Repository, *github.Response, error) {
	// Note: As of now, the token checking is redundant as the token is checked before this function is called.
	// However, it is kept here for the completeness of the code.

	token, isSet := os.LookupEnv("TF_GIT_PAT_TOKEN")
	if !isSet {
<<<<<<< HEAD
		return nil, nil, cliErrs.ErrTfGitPatTokenNotSet
	}

	if token == "" {
		return nil, nil, cliErrs.ErrTfGitPatTokenEmpty
=======
		return nil, nil, cliErrs.ErrGithubTokenNotSet
	}

	if token == "" {
		return nil, nil, cliErrs.ErrGithubTokenEmpty
>>>>>>> cffdcfe (Added interfaces to the git related libraries and added git PAT token validation)
	}

	if g.client == nil {
		g.client = github.NewClient(nil).WithAuthToken(token)
	}

	repoDetails, response, err := g.client.Repositories.Get(g.ctx, owner, repo)
	if repoDetails != nil {
		g.logger.Debug(fmt.Sprintf("Fetched repository details: %v", repoDetails))
		return repoDetails, response, err
	}
	g.logger.Error(fmt.Sprintf("Failed to fetch repository details. response: %v, err: %v", response, err))
	return nil, response, err
}
