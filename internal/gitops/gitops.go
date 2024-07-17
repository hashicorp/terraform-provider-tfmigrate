package gitops

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"os"
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
	"golang.org/x/oauth2"

	netHttp "net/http"
)

// create a separate file for the error messages
// custom error handling
// create a struct for repopath, remoteName, PAT, ?

const (
	VALID_REPO_URL_FORMAT = "https://github.com/username/repository.git"
	VALID_URL_REGEX       = `^(https?|git)://.*\.git$`
	VALID_BARNCH_NAME     = `^[a-zA-Z0-9/_-]+$`
)

type GitUserConfig struct {
	Name  string
	Email string
}

// 1. CLONE A REMOTE REPOSITORY - not for implementation.
func CloneRepository(repoURL, directory string) error {
	url_regex := regexp.MustCompile(VALID_URL_REGEX)
	if !url_regex.MatchString(repoURL) {
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

// 2. RESET THE WORKSPACE TO LAST COMMIT VERSION.
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

// 3. LIST BRANCHES.
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

// 4. CREATE AND SWITCH TO A LOCAL BRANCH.
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

// 5. DELETE A LOCAL BRANCH.
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

// 6. CREATE A COMMIT IN LOCAL.
func CreateCommit(repoPath, message string) (string, error) {
	if len(message) > 255 {
		return "", fmt.Errorf("commit message too long: must be 255 characters or less")
	}

	repo, err := git.PlainOpen(repoPath)
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
	author := GlobalGitConfig()

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

// 7. PUSH COMMIT TO REMOTE.
func PushCommit(repoPath string, remoteName string, branchName string, github_token string, force bool) error {
	authToken := github_token
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
	author := GlobalGitConfig()
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

// 8. Creates a pull request on GitHub.
func CreatePullRequest(repoIdentifier, baseBranch, featureBranch, title, body, github_token string) (string, error) {

	if len(strings.Split(repoIdentifier, "/")) != 2 {
		return "", fmt.Errorf("invalid repository identifier. It should be in the format: owner/repository")
	}

	repoOwner := strings.Split(repoIdentifier, "/")[0]
	repoName := strings.Split(repoIdentifier, "/")[1]

	authToken := github_token
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: authToken},
	)

	//tc := oauth2.NewClient(ctx, ts)

	ts2 := &oauth2.Transport{
		Source: oauth2.ReuseTokenSource(nil, ts),
		Base: &netHttp.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	tc := &netHttp.Client{
		Transport: ts2,
	}

	client := github.NewClient(tc)

	draft := true
	newPR := &github.NewPullRequest{
		Title: github.String(title),
		Head:  github.String(featureBranch),
		Base:  github.String(baseBranch),
		Body:  github.String(body),
		Draft: &draft,
	}

	pr, _, err := client.PullRequests.Create(ctx, repoOwner, repoName, newPR)
	if err != nil {
		return "", err
	}
	return pr.GetHTMLURL(), nil
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
func GlobalGitConfig() GitUserConfig {
	// Get the global git config file path
	repo, err := git.PlainOpen(".")
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
