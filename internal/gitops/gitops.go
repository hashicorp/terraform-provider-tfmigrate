// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gitops

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/google/go-github/v45/github"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/xanzy/go-gitlab"
	"golang.org/x/oauth2"
)

const (
	VALID_REPO_URL_FORMAT = "https://github.com/username/repository.git"
	VALID_URL_REGEX       = `^(https?|git)://.*\.git$`
	VALID_BARNCH_NAME     = `^[a-zA-Z0-9/_-]+$`
)

const (
	ProviderGitHub  ProviderType = "GitHub"
	ProviderGitLab  ProviderType = "GitLab"
	ProviderUnknown ProviderType = "Unknown"
)

type GitUserConfig struct {
	Name  string
	Email string
}

type PullRequestParams struct {
	RepoIdentifier string
	BaseBranch     string
	FeatureBranch  string
	Title          string
	Body           string
	GitPatToken    string
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

// providerConfigs contains the mapping of hostnames to provider types.
var providerConfigs = []ProviderConfig{
	{ProviderUrl: "github.com", Type: ProviderGitHub},
	{ProviderUrl: "gitlab.com", Type: ProviderGitLab},
}

// tokenRegexList contains patterns for each token type.
var tokenRegexList = []TokenRegex{
	{Pattern: `^ghp_[a-zA-Z0-9]{36}$`, Type: ProviderGitHub},    // GitHub PAT format
	{Pattern: `^glpat-[a-zA-Z0-9-]{20}$`, Type: ProviderGitLab}, // GitLab PAT format
}

// CloneRepository clones a repository from the given URL to the specified directory.
func CloneRepository(repoURL, directory string) error {
	urlRegex := regexp.MustCompile(VALID_URL_REGEX)
	if !urlRegex.MatchString(repoURL) {
		return fmt.Errorf("invalid repository URL. It should be in the format: %s", VALID_REPO_URL_FORMAT)
	}

	_, err := git.PlainClone(directory, false, &git.CloneOptions{
		URL:      repoURL,
		Progress: os.Stdout,
	})
	if err != nil {
		return err
	}
	return nil
}

// ResetToLastCommittedVersion resets the working directory to the last committed version.
func ResetToLastCommittedVersion(repoPath string) error {
	repo, err := openRepository(repoPath)
	if err != nil {
		return err
	}

	ref, err := repo.Head()
	if err != nil {
		return err
	}

	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return err
	}

	worktree, err := repo.Worktree()
	if err != nil {
		return err
	}

	err = worktree.Reset(&git.ResetOptions{
		Mode:   git.HardReset,
		Commit: commit.Hash,
	})
	if err != nil {
		return err
	}
	return nil
}

// ListBranches lists all the branches in a repository.
func ListBranches(repoPath string) ([]string, error) {
	var branches []string

	repo, err := openRepository(repoPath)
	if err != nil {
		return nil, err
	}

	branchesIter, err := repo.Branches()
	if err != nil {
		return nil, err
	}

	err = branchesIter.ForEach(func(ref *plumbing.Reference) error {
		branches = append(branches, ref.Name().String())
		return nil
	})
	if err != nil {
		return nil, err
	}

	return branches, nil
}

// CreateAndSwitchBranch creates a new branch and switches to it.
func CreateAndSwitchBranch(repoPath, branchName string) error {
	repo, err := openRepository(repoPath)
	if err != nil {
		return err
	}

	worktree, err := repo.Worktree()
	if err != nil {
		return err
	}

	branches, err := ListBranches(repoPath)
	if err != nil {
		return err
	}

	// Check if the branch already exists.
	if slices.Contains(branches, branchName) {
		return fmt.Errorf("branch '%s' already exists", branchName)
	}

	// Check if the branch name is valid.
	branch_name := regexp.MustCompile(VALID_BARNCH_NAME)
	if !branch_name.MatchString(branchName) {
		return fmt.Errorf("invalid branch name '%s'. Branch names can only contain letters, digits, '_', '-', and '/'", branchName)
	}

	// Create and switch to the new branch.
	err = worktree.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branchName),
		Create: true,
	})
	if err != nil {
		return err
	}
	return nil
}

// DeleteLocalBranch deletes a local branch.
func DeleteLocalBranch(repoPath, branchName string) error {
	repo, err := openRepository(repoPath)
	if err != nil {
		return err
	}

	// Check if the branch exists.
	branches, err := ListBranches(repoPath)
	if err != nil {
		return err
	}

	// Check if the branch already exists.
	if slices.Contains(branches, branchName) {
		return fmt.Errorf("branch '%s' already exists", branchName)
	}

	// Check if the branch to delete is currently checked out.
	headRef, err := repo.Head()
	if err != nil {
		return err
	}
	if headRef.Name().Short() == branchName {
		return fmt.Errorf("cannot delete the currently checked out branch '%s'", branchName)
	}

	// Delete the branch
	err = repo.Storer.RemoveReference(plumbing.NewBranchReferenceName(branchName))
	if err != nil {
		return err
	}
	return nil
}

// CreateCommit creates a new commit with the given message.
func CreateCommit(repoPath, message string) (string, error) {

	if len(message) > 255 {
		return "", fmt.Errorf("commit message too long: must be 255 characters or less")
	}

	repo, err := openRepository(repoPath)
	if err != nil {
		return "", err
	}

	worktree, err := repo.Worktree()
	if err != nil {
		return "", err
	}

	// Check if there are changes to commit.
	status, err := worktree.Status()
	if err != nil {
		return "", err
	}

	if status.IsClean() {
		return "", fmt.Errorf("no changes to commit")
	}

	// Add all changes to the staging area.
	_, err = worktree.Add(".")
	if err != nil {
		return "", err
	}

	// Retrieve the author name and email from the Git config.
	author := GlobalGitConfig(repoPath)

	// Commit the changes.
	commit, err := worktree.Commit(message, &git.CommitOptions{
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

// PushCommit pushes the changes to the remote repository.
func PushCommit(repoPath string, remoteName string, branchName string, githubToken string, force bool) error {
	authToken := githubToken
	repo, err := openRepository(repoPath)
	if err != nil {
		return err
	}

	// Check out the branch.
	w, err := repo.Worktree()
	if err != nil {
		return err
	}
	err = w.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branchName),
	})
	if err != nil {
		return err
	}

	// Push the changes to the remote repository.
	author := GlobalGitConfig(repoPath)
	err = repo.Push(&git.PushOptions{
		InsecureSkipTLS: true,
		RemoteName:      remoteName,
		Auth: &http.BasicAuth{
			Username: author.Name,
			Password: authToken,
		},
		Force: force,
	})
	if err != nil {
		if err == git.NoErrAlreadyUpToDate {
			log.Println("Everything is up-to-date")
		} else {
			return err
		}
	}
	return nil
}

// CreateRequest creates a pull request or merge request based on the serviceProvider (GitHub or GitLab).
func CreatePullRequest(params PullRequestParams) (string, error) {
	serviceProvider, err := GetServiceProvider()
	if err != nil {
		return "", fmt.Errorf("failed to get remote provider: %w", err)
	}

	patTokentype, err := IdentifyTokenType(params.GitPatToken)
	if err != nil {
		return "", fmt.Errorf("failed to identify token type: %w", err)
	}

	if len(strings.Split(params.RepoIdentifier, "/")) != 2 {
		return "", fmt.Errorf("invalid repository identifier. It should be in the format: owner/repository")
	}
	repoOwner := strings.Split(params.RepoIdentifier, "/")[0]
	repoName := strings.Split(params.RepoIdentifier, "/")[1]

	if serviceProvider == ProviderGitHub {
		if patTokentype != ProviderGitHub {
			return "", fmt.Errorf("your VCS provider is github but you have set a wrong PAT token: %s", patTokentype)
		}

		ctx := context.Background()
		ts := oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: params.GitPatToken},
		)
		tc := oauth2.NewClient(ctx, ts)
		client := github.NewClient(tc)

		newPR := &github.NewPullRequest{
			Title: github.String(params.Title),
			Head:  github.String(params.FeatureBranch),
			Base:  github.String(params.BaseBranch),
			Body:  github.String(params.Body),
		}
		pr, _, err := client.PullRequests.Create(ctx, repoOwner, repoName, newPR)
		if err != nil {
			return "", fmt.Errorf("failed to create GitHub pull request: %w", err)
		}
		return pr.GetHTMLURL(), nil
	} else if serviceProvider == ProviderGitLab {
		if patTokentype != ProviderGitLab {
			return "", fmt.Errorf("your VCS provider is gitlab but you have set a wrong PAT token: %s", patTokentype)
		}
		git, err := gitlab.NewClient(params.GitPatToken)
		if err != nil {
			return "", fmt.Errorf("failed to create GitLab client: %w", err)
		}
		mrOptions := &gitlab.CreateMergeRequestOptions{
			SourceBranch: &params.FeatureBranch,
			TargetBranch: &params.BaseBranch,
			Title:        &params.Title,
			Description:  &params.Body,
		}
		mr, _, err := git.MergeRequests.CreateMergeRequest(params.RepoIdentifier, mrOptions)
		if err != nil {
			return "", fmt.Errorf("failed to create GitLab merge request: %w", err)
		}
		return mr.WebURL, nil
	} else {
		return "", fmt.Errorf("unsupported service provider: %s", serviceProvider)
	}
}

// List remote references in a repository.
func ListRemote(repoPath string) ([]string, error) {
	repo, err := openRepository(repoPath)
	if err != nil {
		return nil, err
	}

	remotes, err := repo.Remotes()
	if err != nil {
		return nil, err
	}

	var remoteNames []string
	for _, remote := range remotes {
		remoteNames = append(remoteNames, remote.Config().Name)
	}

	return remoteNames, nil
}

// GetGitConfig retrieves a global Git configuration value.
func GlobalGitConfig(repoPath string) GitUserConfig {
	// Get the global git config file path
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return GitUserConfig{}
	}

	cfg, err := repo.ConfigScoped(config.GlobalScope)
	if err != nil {
		return GitUserConfig{}
	}
	return cfg.User

}

// Private helper function to open a repository.
func openRepository(repoPath string) (*git.Repository, error) {
	repo, err := git.PlainOpenWithOptions(repoPath, &git.PlainOpenOptions{
		DetectDotGit: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open repository: %w", err)
	}
	return repo, nil
}

func PushCommitUsingGit(remoteName string, branchName string) error {
	// execute git push command
	out, err := exec.Command("git", "push", "-u", remoteName, branchName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to push the commit: %s", out)
	}
	tflog.Info(context.Background(), "Pushed the commit to remote", map[string]interface{}{"remote": remoteName, "branch": branchName})
	return nil
}

func GetRemoteName() (string, error) {
	output, err := exec.
		Command("git", "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}").
		CombinedOutput()

	if strings.Contains(string(output), "no upstream configured for branch") {
		return "origin", nil
	}
	if err != nil {
		errorMessage := fmt.Sprintf("error getting remote name: %s", string(output))
		return "", fmt.Errorf(errorMessage, err)
	}

	upstream := strings.TrimSpace(string(output))
	remoteName := strings.Split(upstream, "/")[0]
	if remoteName == "" {
		return "", fmt.Errorf("error fetching remote name")
	}

	return remoteName, nil
}

// GetServiceProvider returns the service provider name based on the remote URL.
func GetServiceProvider() (ProviderType, error) {
	remoteName, err := GetRemoteName()
	if err != nil {
		return ProviderUnknown, fmt.Errorf("error getting remote name: %w", err)
	}

	// Execute git command to fetch the remote URL.
	cmd := exec.Command("git", "remote", "get-url", remoteName)
	out, err := cmd.Output()
	if err != nil {
		return ProviderUnknown, fmt.Errorf("error getting remote URL: %w", err)
	}

	// Trim whitespace and extract the remote URL.
	remoteUrl := strings.TrimSpace(string(out))

	// Match the remote URL with the known providers.
	for _, config := range providerConfigs {
		if strings.Contains(remoteUrl, config.ProviderUrl) {
			return config.Type, nil
		}
	}

	return ProviderUnknown, errors.New("unknown service provider")
}

// IdentifyTokenType determines the type of the PAT token.
func IdentifyTokenType(token string) (ProviderType, error) {
	for _, tokenRegex := range tokenRegexList {
		matched, err := regexp.MatchString(tokenRegex.Pattern, token)
		if err != nil {
			return ProviderUnknown, fmt.Errorf("error processing regex for token type %s: %w", tokenRegex.Type, err)
		}
		if matched {
			return tokenRegex.Type, nil
		}
	}
	return ProviderUnknown, errors.New("unable to identify token type: invalid token format")
}
