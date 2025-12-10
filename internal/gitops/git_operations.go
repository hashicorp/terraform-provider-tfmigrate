// Copyright IBM Corp. 2024, 2025
// SPDX-License-Identifier: MPL-2.0.

package gitops

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"terraform-provider-tfmigrate/internal/util/vcs/git/remote_svc_provider"
	"time"

	consts "terraform-provider-tfmigrate/internal/constants"
	gitUtil "terraform-provider-tfmigrate/internal/util/vcs/git"

	git "github.com/go-git/go-git/v5"
	transportHttp "github.com/go-git/go-git/v5/plumbing/transport/http"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var err error

type gitOperations struct {
	ctx     context.Context
	gitUtil gitUtil.GitUtil
}

type GitOperations interface {
	BranchExists(remote string, branch string) (bool, error)
	GetRemoteName() (string, error)
	GetRemoteURL(remoteName string) (string, error)
	ResetToLastCommittedVersion(repoPath string) error
	ListBranches(repoPath string) ([]string, error)
	DeleteLocalBranch(repoPath, branchName string) error
	CreateCommit(repoPath, message string) (string, error)
	PushCommit(pushCommitParams gitUtil.PushCommitParams) error
	CreatePullRequest(params gitUtil.PullRequestParams) (string, error)
	GetRepoIdentifier(repoUrl string) string
	GetRemoteServiceProvider(remoteURL string) *consts.GitServiceProvider
	GetCurrentBranch() (string, error)
	IsGitRepo() (bool, error)
	IsGitRoot() (bool, error)
	IsGitTreeClean() (bool, error)
	IsSSHUrl(repoUrl string) bool
	IsSupportedVCSProvider(repoUrl string) bool
	GetDefaultBaseBranch() (string, error)
}

// NewGitOperations creates a new instance of GitOperations.
func NewGitOperations(ctx context.Context, gitUtil gitUtil.GitUtil) GitOperations {
	return &gitOperations{
		ctx:     ctx,
		gitUtil: gitUtil, // use the passed-in gitUtil instead of creating a new one
	}
}

// GetRemoteName returns the remote name.
func (gitOps *gitOperations) GetRemoteName() (string, error) {
	// run git remote command to get the remote name.
	cmd := exec.Command("git", "remote")
	out, err := cmd.Output()
	if err != nil {
		errorMessage := fmt.Sprintf("error getting remote name: %s", string(out))
		return gitOps.logAndReturnErr(errorMessage, err)
	}

	// get the remote name from the output.
	remoteName := strings.TrimSpace(string(out))
	if remoteName == "" {
		return "", errors.New(strings.ToLower(consts.ErrNoRemoteSet))
	}

	return remoteName, nil
}

// GetRemoteURL returns the remote URL.
func (gitOps *gitOperations) GetRemoteURL(remoteName string) (string, error) {
	cmd := exec.Command("git", "remote", "get-url", remoteName)
	out, err := cmd.Output()
	if err != nil {
		errorMessage := fmt.Sprintf("error getting remote url: %s", string(out))
		return gitOps.logAndReturnErr(errorMessage, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// ResetToLastCommittedVersion resets the workspace to last commit version.
func (gitOps *gitOperations) ResetToLastCommittedVersion(repoPath string) error {
	repo, err := gitOps.gitUtil.OpenRepository(repoPath)
	if err != nil {
		return err
	}

	ref, err := gitOps.gitUtil.Head(repo)
	if err != nil {
		return err
	}

	commit, err := gitOps.gitUtil.CommitObject(repo, ref.Hash())
	if err != nil {
		return err
	}

	worktree, err := gitOps.gitUtil.Worktree(repo)
	if err != nil {
		return err
	}

	if err = gitOps.gitUtil.Reset(worktree, &git.ResetOptions{
		Mode:   git.HardReset,
		Commit: commit.Hash,
	}); err != nil {
		return err
	}
	return nil
}

// ListBranches list branches in a repository.
func (gitOps *gitOperations) ListBranches(repoPath string) ([]string, error) {
	var branches []string

	repo, err := gitOps.gitUtil.OpenRepository(repoPath)
	if err != nil {
		return nil, err
	}

	branchesIter, err := gitOps.gitUtil.Branches(repo)
	if err != nil {
		return nil, err
	}

	if err = branchesIter.ForEach(func(ref *plumbing.Reference) error {
		branches = append(branches, ref.Name().String())
		return nil
	}); err != nil {
		return nil, err
	}

	return branches, nil
}

// DeleteLocalBranch delete a local branch in a repository.
func (gitOps *gitOperations) DeleteLocalBranch(repoPath, branchName string) error {
	repo, err := gitOps.gitUtil.OpenRepository(repoPath)
	if err != nil {
		return err
	}

	// Check if the branch exists.
	branches, err := gitOps.ListBranches(repoPath)
	if err != nil {
		return err
	}

	// Check if the branch already exists.
	if !slices.Contains(branches, "refs/heads/"+branchName) {
		return fmt.Errorf("the branch %s does not exist", branchName)
	}

	// Check if the branch to delete is currently checked out.
	headRef, err := gitOps.gitUtil.Head(repo)
	if err != nil {
		return err
	}
	if headRef.Name().Short() == branchName {
		return fmt.Errorf("cannot delete the currently checked out branch '%s'", branchName)
	}

	// Delete the branch.
	if err = gitOps.gitUtil.RemoveReference(repo.Storer, plumbing.NewBranchReferenceName(branchName)); err != nil {
		return err
	}
	return nil
}

// CreateCommit creates a commit in the repository.
func (gitOps *gitOperations) CreateCommit(repoPath, message string) (string, error) {
	if len(message) > 255 {
		return "", fmt.Errorf("commit message too long: must be 255 characters or less")
	}

	repo, err := gitOps.gitUtil.OpenRepository(repoPath)
	if err != nil {
		return "", err
	}

	worktree, err := gitOps.gitUtil.Worktree(repo)
	if err != nil {
		return "", err
	}

	// Check if there are changes to commit.
	status, err := gitOps.gitUtil.Status(worktree)
	if err != nil {
		return "", err
	}
	if status.IsClean() {
		return "", nil
	}

	// Add all changes to the staging area.
	_, err = gitOps.gitUtil.Add(worktree, ".")
	if err != nil {
		return "", err
	}

	// Retrieve the author name and email from the Git config.
	author, err := gitOps.gitUtil.GlobalGitConfig()
	if err != nil {
		return "", err
	}

	// Commit the changes.
	commit, err := gitOps.gitUtil.Commit(worktree, message, &git.CommitOptions{
		Author: &object.Signature{
			Name:  strings.TrimSpace(author.Name),
			Email: strings.TrimSpace(author.Email),
			When:  time.Now(),
		},
	})
	if err != nil {
		return "", err
	}

	return commit.String(), nil
}

// PushCommit determines the type of remote URL and pushes the commit accordingly.
func (gitOps *gitOperations) PushCommit(params gitUtil.PushCommitParams) error {
	repo, err := gitOps.gitUtil.OpenRepository(params.RepoPath)
	if err != nil {
		return err
	}

	remoteURL, err := gitOps.GetRemoteURL(params.RemoteName)
	if err != nil {
		return err
	}

	isHTTP := strings.HasPrefix(remoteURL, "http://") || strings.HasPrefix(remoteURL, "https://")
	isSSH := strings.HasPrefix(remoteURL, "ssh://") || strings.Contains(remoteURL, "@")

	switch {
	case isHTTP:
		return gitOps.pushForHTTPUrl(repo, params.RemoteName, params.BranchName, params.GitPatToken, params.Force)
	case isSSH:
		return gitOps.pushForSshUrl(params.RemoteName, params.BranchName)
	default:
		return fmt.Errorf("unsupported remote URL type: %s", remoteURL)
	}
}

// pushForHTTPUrl pushes the commit to the remote repository using HTTP(S) URL.
func (gitOps *gitOperations) pushForHTTPUrl(repo *git.Repository, remoteName, branchName, token string, force bool) error {
	worktree, err := gitOps.gitUtil.Worktree(repo)
	if err != nil {
		return err
	}

	if err := gitOps.gitUtil.Checkout(worktree, &git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branchName),
	}); err != nil {
		return err
	}

	author, err := gitOps.gitUtil.GlobalGitConfig()
	if err != nil {
		return err
	}

	err = gitOps.gitUtil.Push(repo, &git.PushOptions{
		RemoteName: remoteName,
		Auth: &transportHttp.BasicAuth{
			Username: author.Name,
			Password: token,
		},
		Force: force,
	})

	if errors.Is(err, git.NoErrAlreadyUpToDate) {
		tflog.Info(gitOps.ctx, "Everything is up-to-date")
		return nil
	}
	return err
}

// pushForSshUrl pushes the commit to the remote repository using SSH URL.
func (gitOps *gitOperations) pushForSshUrl(remoteName, branchName string) error {
	out, err := exec.Command("git", "push", "-u", remoteName, branchName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to push the commit: %s", out)
	}
	tflog.Info(context.Background(), "Pushed the commit to remote", map[string]interface{}{
		"remote": remoteName, "branch": branchName,
	})
	return nil
}

// CreatePullRequest creates a pull request or merge request based on the serviceProvider (GitHub or GitLab).
func (gitOps *gitOperations) CreatePullRequest(pullRequestParams gitUtil.PullRequestParams) (string, error) {

	// Validate the repository identifier.
	if len(strings.Split(pullRequestParams.RepoIdentifier, "/")) != 2 {
		return "", fmt.Errorf(strings.ToLower(consts.InvalidRepositoryIdentifier), pullRequestParams.RepoIdentifier)
	}

	var remoteServiceProvider *consts.GitServiceProvider
	if remoteServiceProvider, err = gitOps.getVcsProviderName(); err != nil {
		return "", err
	}

	remoteVcsSvcProvider, err := remote_svc_provider.NewRemoteSvcProviderFactory(gitOps.ctx).NewRemoteVcsSvcProvider(remoteServiceProvider)
	if err != nil {
		return "", err
	}

	prUrl, err := remoteVcsSvcProvider.CreatePullRequest(pullRequestParams)
	if err != nil {
		return "", err
	}
	return prUrl, nil
}

// GetRepoIdentifier returns the repository identifier.
// This is a wrapper around the GetRepoIdentifier method in GitUtil.
// This is done to avoid direct dependency on GitUtil in the client code.
func (gitOps *gitOperations) GetRepoIdentifier(repoUrl string) string {
	return gitOps.gitUtil.GetRepoIdentifier(repoUrl)
}

// GetRemoteServiceProvider returns the remote service provider.
func (gitOps *gitOperations) GetRemoteServiceProvider(remoteURL string) *consts.GitServiceProvider {
	return gitOps.gitUtil.GetRemoteServiceProvider(remoteURL)
}

// logAndReturnErr logs the error message and returns the error.
func (gitOps *gitOperations) logAndReturnErr(errMsg string, err error) (string, error) {
	err = fmt.Errorf("err: %s, details: %s", err, errMsg)
	tflog.Error(gitOps.ctx, err.Error())
	return "", err
}

func (gitOps *gitOperations) getVcsProviderName() (*consts.GitServiceProvider, error) {
	remoteName, err := gitOps.GetRemoteName()
	if err != nil {
		return nil, err
	}
	repoURL, err := gitOps.GetRemoteURL(remoteName)
	if err != nil {
		return nil, err
	}
	remoteServiceProvider := gitOps.GetRemoteServiceProvider(repoURL)

	return remoteServiceProvider, err
}

// IsGitRepo() checks if the current directory is a git repository.
func (gitOps *gitOperations) IsGitRepo() (bool, error) {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), consts.ErrNotGitRepo) {
			return false, errors.New(string(out))
		}
		return false, fmt.Errorf("error checking if the current working directory is a git repository: %s", string(out))
	}
	return strings.TrimSpace(string(out)) == "true", nil
}

// IsGitRoot() checks if the current directory is the root of the git repository.
func (gitOps *gitOperations) IsGitRoot() (bool, error) {
	// Check if the current directory is the root of the git repository.
	cmd := exec.Command("git", "rev-parse", "--show-cdup")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("error checking if the current directory is the root of the git repository: %s", string(out))
	}
	if strings.TrimSpace(string(out)) != "../" {
		return false, nil
	}

	// Check if the current directory is inside a superproject's working tree.
	cmd = exec.Command("git", "rev-parse", "--show-superproject-working-tree")
	out, err = cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("error checking if the current directory is inside a superproject's working tree: %s", string(out))
	}
	if strings.TrimSpace(string(out)) != "" {
		return false, nil
	}
	return true, nil
}

// IsGitTreeClean() checks if the current working directory is clean.
func (gitOps *gitOperations) IsGitTreeClean() (bool, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("error checking if the current working directory is clean: %s", string(out))
	}

	status := strings.TrimSpace(string(out))
	if status == "" {
		return true, nil
	}

	lines := strings.Split(status, "\n")
	for _, line := range lines {
		if strings.Contains(line, ".gitignore") {
			return true, nil
		}
	}

	return false, nil
}

// GetCurrentBranch returns the current branch.
func (gitOps *gitOperations) GetCurrentBranch() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("error getting current branch: %s", string(out))
	}
	return strings.TrimSpace(string(out)), nil
}

// BranchExists checks if the specified branch exists in the remote.
func (gitOps *gitOperations) BranchExists(remote string, branch string) (bool, error) {
	out, err := exec.Command("git", "ls-remote", "--heads", remote, branch).CombinedOutput()
	if err != nil {
		errorMessage := fmt.Sprintf("error checking if branch exists, branch: %s, remote: %s, error: %s", branch, remote, string(out))
		tflog.Error(gitOps.ctx, errorMessage)
		return false, err
	}
	return strings.Contains(strings.TrimSpace(string(out)), "refs/heads/"+branch), nil
}

func (gitOps *gitOperations) IsSSHUrl(repoUrl string) bool {
	return strings.HasPrefix(repoUrl, "git@")
}

func (gitOps *gitOperations) IsSupportedVCSProvider(repoUrl string) bool {
	// check if the repoUrl contains github or gitlab from global constants
	if strings.Contains(repoUrl, string(consts.GitHub)) || strings.Contains(repoUrl, string(consts.GitLab)) || strings.Contains(repoUrl, string(consts.Bitbucket)) {
		return true
	}
	return false
}
func (gitOps *gitOperations) GetDefaultBaseBranch() (string, error) {
	remoteName, err := gitOps.GetRemoteName()
	if err != nil {
		return "", err
	}
	// run git remote show command to get the default base branch.
	cmd := exec.Command("git", "remote", "show", remoteName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("error running git remote show: %v", err)
	}
	// parse the output to find the default base branch.
	// The default base branch is usually listed as "HEAD branch: <branch_name>"
	// in the output of git remote show.
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "  HEAD branch:") {
			parts := strings.Split(line, ":")
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1]), nil
			}
		}
	}

	return "", fmt.Errorf("could not find default base branch in git remote show output")
}
