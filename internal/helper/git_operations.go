// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0.

package gitops

import (
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
	GetRemoteName() (string, error)
	GetRemoteURL(remoteName string) (string, error)
	ResetToLastCommittedVersion(repoPath string) error
	ListBranches(repoPath string) ([]string, error)
	DeleteLocalBranch(repoPath, branchName string) error
	CreateCommit(repoPath, message string) (string, error)
	PushCommit(repoPath string, remoteName string, branchName string, githubToken string, force bool) error
	CreatePullRequest(params gitUtil.PullRequestParams) (string, error)
	PushCommitUsingGit(remoteName string, branchName string) error
	GetRepoIdentifier(repoUrl string) string
	GetRemoteServiceProvider(remoteURL string) *consts.GitServiceProvider
}

type GitUserConfig struct {
	Name  string
	Email string
}

// ProviderType represents the type of a Git service provider.
type ProviderType string

// TokenRegex stores the regex patterns for identifying tokens.
type TokenRegex struct {
	Pattern string
	Type    ProviderType
}

type ProviderConfig struct {
	ProviderUrl string
	Type        ProviderType
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

// PushCommit push commit to the remote repository.
func (gitOps *gitOperations) PushCommit(repoPath, remoteName, branchName, githubToken string, force bool) error {
	authToken := githubToken
	repo, err := gitOps.gitUtil.OpenRepository(repoPath)
	if err != nil {
		return err
	}

	// Check out the branch.
	worktree, err := gitOps.gitUtil.Worktree(repo)
	if err != nil {
		return err
	}
	err = gitOps.gitUtil.Checkout(worktree, &git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branchName),
	})
	if err != nil {
		return err
	}

	// Push the changes to the remote repository.
	author, err := gitOps.gitUtil.GlobalGitConfig()
	if err != nil {
		return err
	}
	err = gitOps.gitUtil.Push(repo, &git.PushOptions{
		RemoteName: remoteName,
		Auth: &transportHttp.BasicAuth{
			Username: author.Name,
			Password: authToken,
		},
		Force: force,
	})
	if err != nil {
		if errors.Is(err, git.NoErrAlreadyUpToDate) {
			tflog.Info(gitOps.ctx, "Everything is up-to-date")
		} else {
			return err
		}
	}
	return nil
}

// CreatePullRequest creates a pull request or merge request based on the serviceProvider (GitHub or GitLab).
func (gitOps *gitOperations) CreatePullRequest(pullRequestParams gitUtil.PullRequestParams) (string, error) {

	// Validate the repository identifier.
	if len(strings.Split(pullRequestParams.RepoIdentifier, "/")) != 2 {
		return "", fmt.Errorf(consts.InvalidRepositoryIdentifier, pullRequestParams.RepoIdentifier)
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

func (gitOps *gitOperations) PushCommitUsingGit(remoteName string, branchName string) error {
	// execute git push command.
	out, err := exec.Command("git", "push", "-u", remoteName, branchName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to push the commit: %s", out)
	}
	tflog.Info(context.Background(), "Pushed the commit to remote", map[string]interface{}{"remote": remoteName, "branch": branchName})
	return nil
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
