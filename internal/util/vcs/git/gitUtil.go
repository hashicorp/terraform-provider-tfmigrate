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
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

const (
	githubClassicTokenPrefix     = `ghp_`
	githubFineGrainedTokenPrefix = `github_pat_`
	gitlabTokenPrefix            = `glpat-`
)

var err error

type GitUserConfig struct {
	Name  string
	Email string
}

type gitUtil struct {
	ctx context.Context
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
func NewGitUtil(ctx context.Context) GitUtil {
	return &gitUtil{
		ctx: ctx,
	}
}

func (g *gitUtil) PlainOpenWithOptions(path string, options *git.PlainOpenOptions) (*git.Repository, error) {
	var repo *git.Repository
	if repo, err = git.PlainOpenWithOptions(path, options); err != nil {
		tflog.Error(context.Background(), "Failed to open repository", map[string]interface{}{"path": path, "error": err})
	}
	return repo, err
}

func (g *gitUtil) Head(repo *git.Repository) (*plumbing.Reference, error) {
	var head *plumbing.Reference
	if head, err = repo.Head(); err != nil {
		tflog.Error(context.Background(), "Failed to get repository head", map[string]interface{}{"error": err})
	}
	return head, err
}

func (g *gitUtil) Worktree(repo *git.Repository) (*git.Worktree, error) {
	var worktree *git.Worktree
	if worktree, err = repo.Worktree(); err != nil {
		tflog.Error(context.Background(), "Failed to get repository worktree", map[string]interface{}{"error": err})
	}
	return worktree, err
}

func (g *gitUtil) Reset(worktree *git.Worktree, options *git.ResetOptions) error {
	if err = worktree.Reset(options); err != nil {
		tflog.Error(context.Background(), "Failed to reset worktree", map[string]interface{}{"options": options, "error": err})
	}
	return err
}

func (g *gitUtil) CommitObject(repo *git.Repository, hash plumbing.Hash) (*object.Commit, error) {
	var commit *object.Commit
	if commit, err = repo.CommitObject(hash); err != nil {
		tflog.Error(context.Background(), "Failed to get commit object", map[string]interface{}{"hash": hash, "error": err})
	}
	return commit, err
}

func (g *gitUtil) Branches(repo *git.Repository) (storer.ReferenceIter, error) {
	var branches storer.ReferenceIter
	if branches, err = repo.Branches(); err != nil {
		tflog.Error(context.Background(), "Failed to get repository branches", map[string]interface{}{"error": err})
	}
	return branches, err
}

func (g *gitUtil) Checkout(worktree *git.Worktree, options *git.CheckoutOptions) error {
	if err = worktree.Checkout(options); err != nil {
		tflog.Error(context.Background(), "Failed to checkout worktree", map[string]interface{}{"options": options, "error": err})
	}
	return err
}

func (g *gitUtil) RemoveReference(storer storer.ReferenceStorer, ref plumbing.ReferenceName) error {
	if err = storer.RemoveReference(ref); err != nil {
		tflog.Error(context.Background(), "Failed to remove reference", map[string]interface{}{"ref": ref, "error": err})
	}
	return err
}

func (g *gitUtil) Add(worktree *git.Worktree, glob string) (plumbing.Hash, error) {
	var hash plumbing.Hash
	if hash, err = worktree.Add(glob); err != nil {
		tflog.Error(context.Background(), "Failed to add to worktree", map[string]interface{}{"glob": glob, "error": err})
	}
	return hash, err
}

func (g *gitUtil) Commit(worktree *git.Worktree, msg string, options *git.CommitOptions) (plumbing.Hash, error) {
	var hash plumbing.Hash
	if hash, err = worktree.Commit(msg, options); err != nil {
		tflog.Error(context.Background(), "Failed to commit worktree", map[string]interface{}{"message": msg, "options": options, "error": err})
	}
	return hash, err
}

func (g *gitUtil) Status(worktree *git.Worktree) (git.Status, error) {
	var status git.Status
	if status, err = worktree.Status(); err != nil {
		tflog.Error(context.Background(), "Failed to get worktree status", map[string]interface{}{"error": err})
	}
	return status, err
}

func (g *gitUtil) Push(repo *git.Repository, o *git.PushOptions) error {
	if err = repo.Push(o); err != nil {
		tflog.Error(context.Background(), "Failed to push to repository", map[string]interface{}{"options": o, "error": err})
	}
	return err
}

func (g *gitUtil) Remotes(repo *git.Repository) ([]*git.Remote, error) {
	var remotes []*git.Remote
	if remotes, err = repo.Remotes(); err != nil {
		tflog.Error(context.Background(), "Failed to get repository remotes", map[string]interface{}{"error": err})
	}
	return remotes, err
}

func (g *gitUtil) ConfigScoped(repo *git.Repository, scope config.Scope) (*config.Config, error) {
	var configSc *config.Config
	if configSc, err = repo.ConfigScoped(scope); err != nil {
		tflog.Error(context.Background(), "Failed to get scoped config", map[string]interface{}{"scope": scope, "error": err})
	}
	return configSc, err
}

func (g *gitUtil) CreatePR(ctx context.Context, client *github.Client, owner string, repo string, pull *github.NewPullRequest) (*github.PullRequest, error) {
	var pr *github.PullRequest
	var resp *github.Response
	if pr, resp, err = client.PullRequests.Create(ctx, owner, repo, pull); err != nil {
		tflog.Error(ctx, "Failed to create pull request", map[string]interface{}{"owner": owner, "repo": repo, "pull": pull, "error": err})
	}
	if resp.StatusCode != http.StatusCreated {
		err = fmt.Errorf("unexpected status code: %d, expected %d", resp.StatusCode, http.StatusCreated)
		tflog.Error(ctx, "Failed to create pull request due to unexpected status code", map[string]interface{}{"status": resp.StatusCode, "error": err})
	}
	return pr, err
}

func (g *gitUtil) NewGitLabClient(gitlabToken string) (*gitlab.Client, error) {
	var gitLabNewClient *gitlab.Client
	if gitLabNewClient, err = gitlab.NewClient(gitlabToken); err != nil {
		tflog.Error(context.Background(), "Failed to create GitLab client", map[string]interface{}{"error": err})
	}
	return gitLabNewClient, err
}

func (g *gitUtil) CreateGitlabMergeRequest(projectPath string, mrOptions *gitlab.CreateMergeRequestOptions, gitLabNewClient *gitlab.Client, gitlabToken string) (*gitlab.MergeRequest, error) {
	var mr *gitlab.MergeRequest
	var resp *gitlab.Response
	if mr, resp, err = gitLabNewClient.MergeRequests.CreateMergeRequest(projectPath, mrOptions); err != nil {
		tflog.Error(context.Background(), fmt.Sprintf("Failed to create merge request for project '%s' with title '%s'", projectPath, *mrOptions.Title), map[string]interface{}{"error": err})
		return nil, err
	}
	if resp.StatusCode != http.StatusCreated {
		err := fmt.Errorf("unexpected status code: %d, expected %d", resp.StatusCode, http.StatusCreated)
		tflog.Error(context.Background(), fmt.Sprintf("Failed to create merge request for project '%s' with title '%s' due to unexpected status code %d", projectPath, *mrOptions.Title, resp.StatusCode), map[string]interface{}{"error": err})
		return nil, err
	}
	return mr, nil
}

func (g *gitUtil) GlobalGitConfig() (GitUserConfig, error) {
	var repo *git.Repository
	var cfg *config.Config
	if repo, err = g.OpenRepository("."); err != nil {
		return GitUserConfig{}, err
	}
	if cfg, err = g.ConfigScoped(repo, config.GlobalScope); err != nil {
		return GitUserConfig{}, err
	}
	return GitUserConfig{
		Name:  cfg.User.Name,
		Email: cfg.User.Email,
	}, nil
}

func (g *gitUtil) OpenRepository(repoPath string) (*git.Repository, error) {
	var repo *git.Repository
	if repo, err = g.PlainOpenWithOptions(repoPath, &git.PlainOpenOptions{
		DetectDotGit: true,
	}); err != nil {
		tflog.Error(context.Background(), "Failed to open repository", map[string]interface{}{"repoPath": repoPath, "error": err})
	}
	return repo, err
}

// GetGitToken returns the GitHub token.
func (g *gitUtil) GetGitToken(gitServiceProvider *consts.GitServiceProvider) (string, error) {
	if gitServiceProvider == nil || *gitServiceProvider == consts.UnknownGitServiceProvider {
		return "", cliErrs.ErrGitServiceProviderNotSupported
	}

	gitPatToken, isSet := os.LookupEnv("TF_GIT_PAT_TOKEN")

	if !isSet {
		return "", cliErrs.ErrTfGitPatTokenNotSet
	}
	if gitPatToken == "" {
		return "", cliErrs.ErrTfGitPatTokenEmpty
	}

	switch *gitServiceProvider {
	case consts.GitHub:
		return getGithubPatToken(gitPatToken)
	case consts.GitLab:
		tflog.Info(context.Background(), fmt.Sprintf("Fetched GitLab token set: %s", gitPatToken))
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
		return "", cliErrs.ErrTfGitPatTokenFineGrained
	}

	return "", cliErrs.ErrTfGitPatTokenUnrecognized
}

// getGitlabPatToken returns the GitLab PAT token.
func (g *gitUtil) getGitlabPatToken(gitPatToken string) (string, error) {
	tokenType := getTokenType(gitPatToken)

	if tokenType == gitlabPat {
		return gitPatToken, nil
	}

	return "", cliErrs.ErrTfGitPatTokenInvalid
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
