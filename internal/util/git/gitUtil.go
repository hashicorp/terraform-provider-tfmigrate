package git

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	consts "terraform-provider-tfmigrate/internal/constants"

	cliErrs "terraform-provider-tfmigrate/internal/cli_errors"

	gitlab "gitlab.com/gitlab-org/api/client-go"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/google/go-github/v45/github"
	"github.com/hashicorp/go-hclog"
)

const (
	githubClassicTokenPrefix     = `ghp_`
	githubFineGrainedTokenPrefix = `github_pat_`
	gitlabTokenPrefix            = `glpat-`
)

type GitUserConfig struct {
	Name  string
	Email string
}

type gitUtil struct {
	logger hclog.Logger
}

// GitUtil interface to mock Git operations.
type GitUtil interface {
	Add(worktree *git.Worktree, glob string) (plumbing.Hash, error)
	Branches(repo *git.Repository) (storer.ReferenceIter, error)
	Checkout(w *git.Worktree, options *git.CheckoutOptions) error
	Commit(worktree *git.Worktree, msg string, options *git.CommitOptions) (plumbing.Hash, error)
	CommitObject(repo *git.Repository, hash plumbing.Hash) (*object.Commit, error)
	ConfigScoped(repo *git.Repository, scope config.Scope) (*config.Config, error)
	CreatePR(ctx context.Context, client *github.Client, owner string, repo string, pull *github.NewPullRequest) (*github.PullRequest, error)
	GetGitToken(gitServiceProvider *consts.GitServiceProvider) (string, error)
	GetOrgAndRepoName(repoIdentifier string) (string, string)
	GetRemoteServiceProvider(remoteURL string) *consts.GitServiceProvider
	GetRepoIdentifier(remoteURL string) string
	GlobalGitConfig() (GitUserConfig, error)
	Head(repo *git.Repository) (*plumbing.Reference, error)
	OpenRepository(repoPath string) (*git.Repository, error)
	PlainOpenWithOptions(path string, options *git.PlainOpenOptions) (*git.Repository, error)
	Push(repo *git.Repository, options *git.PushOptions) error
	Remotes(repo *git.Repository) ([]*git.Remote, error)
	RemoveReference(storer.ReferenceStorer, plumbing.ReferenceName) error
	Reset(worktree *git.Worktree, options *git.ResetOptions) error
	Status(worktree *git.Worktree) (git.Status, error)
	Worktree(repo *git.Repository) (*git.Worktree, error)
	NewGitLabClient(gitlabToken string) (*gitlab.Client, error)
	CreateGitlabMergeRequest(projectPath string, mrOptions *gitlab.CreateMergeRequestOptions, gitLabNewClient *gitlab.Client, gitlabToken string) (*gitlab.MergeRequest, error)
}

// NewGitUtil creates a new instance of GitUtil.
func NewGitUtil(logger hclog.Logger) GitUtil {
	return &gitUtil{logger}
}

func (g *gitUtil) PlainOpenWithOptions(path string, options *git.PlainOpenOptions) (*git.Repository, error) {
	repo, err := git.PlainOpenWithOptions(path, options)
	if err != nil {
		g.logger.Error("Failed to open repository", "path", path, "error", err)
	}
	return repo, err
}

func (g *gitUtil) Head(repo *git.Repository) (*plumbing.Reference, error) {
	head, err := repo.Head()
	if err != nil {
		g.logger.Error("Failed to get repository head", "error", err)
	}
	return head, err
}

func (g *gitUtil) Worktree(repo *git.Repository) (*git.Worktree, error) {
	worktree, err := repo.Worktree()
	if err != nil {
		g.logger.Error("Failed to get repository worktree", "error", err)
	}
	return worktree, err
}

func (g *gitUtil) Reset(worktree *git.Worktree, options *git.ResetOptions) error {
	err := worktree.Reset(options)
	if err != nil {
		g.logger.Error("Failed to reset worktree", "options", options, "error", err)
	}
	return err
}

func (g *gitUtil) CommitObject(repo *git.Repository, hash plumbing.Hash) (*object.Commit, error) {
	commit, err := repo.CommitObject(hash)
	if err != nil {
		g.logger.Error("Failed to get commit object", "hash", hash, "error", err)
	}
	return commit, err
}

func (g *gitUtil) Branches(repo *git.Repository) (storer.ReferenceIter, error) {
	branches, err := repo.Branches()
	if err != nil {
		g.logger.Error("Failed to get repository branches", "error", err)
	}
	return branches, err
}

func (g *gitUtil) Checkout(worktree *git.Worktree, options *git.CheckoutOptions) error {
	err := worktree.Checkout(options)
	if err != nil {
		g.logger.Error("Failed to checkout worktree", "options", options, "error", err)
	}
	return err
}

func (g *gitUtil) RemoveReference(storer storer.ReferenceStorer, ref plumbing.ReferenceName) error {
	err := storer.RemoveReference(ref)
	if err != nil {
		g.logger.Error("Failed to remove reference", "reference", ref, "error", err)
	}
	return err
}

func (g *gitUtil) Add(worktree *git.Worktree, glob string) (plumbing.Hash, error) {
	hash, err := worktree.Add(glob)
	if err != nil {
		g.logger.Error("Failed to add to worktree", "glob", glob, "error", err)
	}
	return hash, err
}

func (g *gitUtil) Commit(worktree *git.Worktree, msg string, options *git.CommitOptions) (plumbing.Hash, error) {
	hash, err := worktree.Commit(msg, options)
	if err != nil {
		g.logger.Error("Failed to commit worktree", "message", msg, "options", options, "error", err)
	}
	return hash, err
}

func (g *gitUtil) Status(worktree *git.Worktree) (git.Status, error) {
	status, err := worktree.Status()
	if err != nil {
		g.logger.Error("Failed to get worktree status", "error", err)
	}
	return status, err
}

func (g *gitUtil) Push(repo *git.Repository, o *git.PushOptions) error {
	err := repo.Push(o)
	if err != nil {
		g.logger.Error("Failed to push to repository", "options", o, "error", err)
	}
	return err
}

func (g *gitUtil) Remotes(repo *git.Repository) ([]*git.Remote, error) {
	remotes, err := repo.Remotes()
	if err != nil {
		g.logger.Error("Failed to get repository remotes", "error", err)
	}
	return remotes, err
}

func (g *gitUtil) ConfigScoped(repo *git.Repository, scope config.Scope) (*config.Config, error) {
	configSc, err := repo.ConfigScoped(scope)
	if err != nil {
		g.logger.Error("Failed to get scoped config", "scope", scope, "error", err)
	}
	return configSc, err
}

func (g *gitUtil) CreatePR(ctx context.Context, client *github.Client, owner string, repo string, pull *github.NewPullRequest) (*github.PullRequest, error) {
	pr, resp, err := client.PullRequests.Create(ctx, owner, repo, pull)
	if err != nil {
		g.logger.Error(fmt.Sprintf("Failed to create pull request for repo '%s/%s' with title '%s': %v", owner, repo, pull.GetTitle(), err))
		return nil, err
	}

	// Validate the response status code.
	if resp.StatusCode != http.StatusCreated {
		err := fmt.Errorf("unexpected status code: %d, expected %d", resp.StatusCode, http.StatusCreated)
		g.logger.Error(fmt.Sprintf("Failed to create pull request for repo '%s/%s' with title '%s' due to unexpected status code %d: %v", owner, repo, pull.GetTitle(), resp.StatusCode, err))
		return nil, err
	}

	return pr, nil
}

// GlobalGitConfig retrieves a global Git configuration value.
func (g *gitUtil) GlobalGitConfig() (GitUserConfig, error) {
	repo, err := g.OpenRepository(".")
	if err != nil {
		return GitUserConfig{}, err
	}

	cfg, err := g.ConfigScoped(repo, config.GlobalScope)
	if err != nil {
		return GitUserConfig{}, err
	}
	return GitUserConfig{
		Name:  cfg.User.Name,
		Email: cfg.User.Email,
	}, nil
}

// OpenRepository is a helper function to open a repository.
func (g *gitUtil) OpenRepository(repoPath string) (*git.Repository, error) {
	repo, err := g.PlainOpenWithOptions(repoPath, &git.PlainOpenOptions{
		DetectDotGit: true,
	})
	return repo, err
}

// GetGitToken returns the GitHub token.
func (g *gitUtil) GetGitToken(gitServiceProvider *consts.GitServiceProvider) (string, error) {
	if gitServiceProvider == nil || *gitServiceProvider == consts.UnknownGitServiceProvider {
		return "", cliErrs.ErrGitServiceProviderNotSupported
	}

	gitPatToken, isSet := os.LookupEnv("TF_GIT_PAT_TOKEN")

	if !isSet {
		return "", cliErrs.ErrGithubTokenNotSet
	}
	if gitPatToken == "" {
		return "", cliErrs.ErrGithubTokenEmpty
	}

	switch *gitServiceProvider {
	case consts.GitHub:
		return getGithubPatToken(gitPatToken)
	case consts.GitLab:
		g.logger.Info(fmt.Sprintf("Fetched GitLab token set: %s", gitPatToken))
		return g.getGitlabPatToken(gitPatToken)
	}

	return "", cliErrs.ErrGitServiceProviderNotSupported
}

// getGithubPatToken returns the GitHub PAT token.
func getGithubPatToken(gitPatToken string) (string, error) {

	tokenType := getTokenType(gitPatToken)

	if tokenType == ClassicToken {
		return gitPatToken, nil
	}

	if tokenType == FineGrainedToken {
		return "", cliErrs.ErrGithubTokenFineGrained
	}

	return "", cliErrs.ErrGithubTokenUnrecognized
}

// getGitlabPatToken returns the GitLab PAT token.
func (g *gitUtil) getGitlabPatToken(gitPatToken string) (string, error) {
	tokenType := getTokenType(gitPatToken)
	g.logger.Info(fmt.Sprintf(" GitLab token type: %s", tokenType))

	if tokenType == gitlabPat {
		g.logger.Info(fmt.Sprintf(" GitLab token: %s", gitPatToken))
		return gitPatToken, nil
	}

	return "", cliErrs.ErrGitlabTokenInvalid
}

// GetRepoIdentifier gets the repo identifier.
// In case of GitHub, the repo identifier is in the format "owner/repo".
// In case of GitLab, the repo identifier is in the format "group/repo".
func (g *gitUtil) GetRepoIdentifier(remoteURL string) string {

	var repoIdentifier string
	gitSvcProvider := g.GetRemoteServiceProvider(remoteURL)
	switch *gitSvcProvider {
	case consts.GitHub:
		repoIdentifier = g.getRepoIdentifierFromRemoteURl(remoteURL, consts.GitHub)
	case consts.GitLab:
		repoIdentifier = g.getRepoIdentifierFromRemoteURl(remoteURL, consts.GitLab)
	default:
		return ""
	}
	return strings.TrimSpace(repoIdentifier)
}

// getRepoIdentifierFromRemoteURl gets the repo identifier from the remote URL.
func (g *gitUtil) getRepoIdentifierFromRemoteURl(remoteURL string, gitSvcProvider consts.GitServiceProvider) string {
	var repoIdentifier string
	if strings.HasPrefix(remoteURL, "git@") {
		repoIdentifier = strings.Split(remoteURL, string(gitSvcProvider)+":")[1]
	} else {
		repoIdentifier = strings.Split(remoteURL, string(gitSvcProvider)+"/")[1]
	}
	return strings.TrimSuffix(repoIdentifier, ".git")
}

// GetOrgAndRepoName gets the org and repo name.
func (g *gitUtil) GetOrgAndRepoName(repoIdentifier string) (string, string) {
	orgAndRepo := strings.Split(repoIdentifier, "/")
	return orgAndRepo[0], orgAndRepo[1]
}

// GetRemoteServiceProvider gets the remote service provider.
func (g *gitUtil) GetRemoteServiceProvider(remoteURL string) *consts.GitServiceProvider {
	if strings.Contains(remoteURL, string(consts.GitHub)) {
		return &consts.GitHub
	}
	if strings.Contains(remoteURL, string(consts.GitLab)) {
		return &consts.GitLab
	}
	return &consts.UnknownGitServiceProvider
}

// NewGitLabClient creates a new GitLab client.
func (g *gitUtil) NewGitLabClient(gitlabToken string) (*gitlab.Client, error) {
	gitLabNewClient, err := gitlab.NewClient(gitlabToken)
	return gitLabNewClient, err
}

// CreateGitlabMergeRequest creates a merge request in GitLab.
func (g *gitUtil) CreateGitlabMergeRequest(projectPath string, mrOptions *gitlab.CreateMergeRequestOptions, gitLabNewClient *gitlab.Client, gitlabToken string) (*gitlab.MergeRequest, error) {

	mr, resp, err := gitLabNewClient.MergeRequests.CreateMergeRequest(projectPath, mrOptions)
	if err != nil {
		g.logger.Error(fmt.Sprintf("Failed to create merge request for project '%s' with title '%s': %v", projectPath, *mrOptions.Title, err))
		return nil, err

	}
	if resp.StatusCode != http.StatusCreated {
		err := fmt.Errorf("unexpected status code: %d, expected %d", resp.StatusCode, http.StatusCreated)
		g.logger.Error(fmt.Sprintf("Failed to create merge request for project '%s' with title '%s' due to unexpected status code %d:", projectPath, *mrOptions.Title, resp.StatusCode))
		return nil, err
	}
	return mr, nil

}

// getTokenType returns the type of GitHub token.
func getTokenType(gitPatToken string) TokenType {
	switch {
	case strings.HasPrefix(gitPatToken, githubClassicTokenPrefix):
		return ClassicToken
	case strings.HasPrefix(gitPatToken, githubFineGrainedTokenPrefix):
		return FineGrainedToken
	case strings.HasPrefix(gitPatToken, gitlabTokenPrefix):
		return gitlabPat
	default:
		return Unrecognized
	}
}
