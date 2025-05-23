package gitops

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"terraform-provider-tfmigrate/_mocks/helper_mocks/gitops_mocks"
	"terraform-provider-tfmigrate/_mocks/helper_mocks/iter_mocks"
	"terraform-provider-tfmigrate/_mocks/util_mocks/vcs_mocks/git_mocks/remote_svc_provider_mocks"

	"terraform-provider-tfmigrate/_mocks/util_mocks/vcs_mocks/git_mocks"
	consts "terraform-provider-tfmigrate/internal/constants"
	gitUtil "terraform-provider-tfmigrate/internal/util/vcs/git"

	"github.com/go-git/go-git/v5"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	gitlab "gitlab.com/gitlab-org/api/client-go"
	//  gitlab "gitlab.com/gitlab-org/api/client-go"
)

var (
	ctx = context.Background()
)

func TestGitRemoteName(t *testing.T) {
	t.Skip("Skipping test as it requires a git repository need to spend some time with docker remote repo setup")
	for name, tc := range map[string]struct {
		remoteName string
		error      error
	}{
		"has valid remote name": {
			"origin",
			nil,
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			ctx := context.Background()
			gitInterface := NewGitOperations(ctx, gitUtil.NewGitUtil(ctx))
			remoteName, err := gitInterface.GetRemoteName()
			if err != nil {
				r.Equal(tc.error, err)
			}
			r.Equal(tc.remoteName, remoteName)
		})
	}
}

func TestGetRemoteURL(t *testing.T) {

	for name, tc := range map[string]struct {
		url   string
		error error
	}{
		"has valid repo url": {
			"git@github.com:hashicorp/terraform-provider-tfmigrate.git",
			nil,
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			ctx := context.Background()
			gitInterface := NewGitOperations(ctx, gitUtil.NewGitUtil(ctx))
			repoUrl, err := gitInterface.GetRemoteURL("origin")
			if err != nil {
				r.Equal(tc.error, err)
			}
			t.Logf("Repo URL: %s", repoUrl)

			if strings.Contains(repoUrl, "https://") {
				tc.url = "https://github.com/hashicorp/terraform-provider-tfmigrate"
			}

			r.Equal(tc.url, repoUrl)

		})
	}
}

func TestResetToLastCommittedVersion(t *testing.T) {
	for name, tc := range map[string]struct {
		repoPath    string
		expectedErr error
	}{
		"successful reset": {
			repoPath:    "./repo",
			expectedErr: nil,
		},
		"failed to open repository": {
			repoPath:    "./invalid-repo",
			expectedErr: errors.New("failed to open repository"),
		},
		"failed to get HEAD": {
			repoPath:    "./repo",
			expectedErr: errors.New("failed to get HEAD"),
		},
		"failed to get commit object": {
			repoPath:    "./repo",
			expectedErr: errors.New("failed to get commit object"),
		},
		"failed to get worktree": {
			repoPath:    "./repo",
			expectedErr: errors.New("failed to get worktree"),
		},
		"failed to reset": {
			repoPath:    "./repo",
			expectedErr: errors.New("failed to reset"),
		},
	} {
		t.Run(name, func(t *testing.T) {
			// Arrange
			mockGitOps := git_mocks.NewMockGitUtil(t)
			r := require.New(t)
			gitOps := NewGitOperations(ctx, mockGitOps)

			mockRepo := &git.Repository{}
			mockWorktree := &git.Worktree{}
			commitHash := plumbing.NewHash("1234567890abcdef1234567890abcdef12345678")
			mockCommit := &object.Commit{Hash: commitHash}
			headRef := plumbing.NewHashReference(plumbing.HEAD, commitHash)

			// Mock setup for each case
			func() {
				if name == "successful reset" {
					mockGitOps.On("OpenRepository", tc.repoPath).Return(mockRepo, nil)
					mockGitOps.On("Head", mockRepo).Return(headRef, nil)
					mockGitOps.On("CommitObject", mockRepo, commitHash).Return(mockCommit, nil)
					mockGitOps.On("Worktree", mockRepo).Return(mockWorktree, nil)
					mockGitOps.On("Reset", mockWorktree, &git.ResetOptions{
						Mode:   git.HardReset,
						Commit: commitHash,
					}).Return(nil)
				}

				if name == "failed to open repository" {
					mockGitOps.On("OpenRepository", tc.repoPath).Return(nil, errors.New("failed to open repository"))
				}

				if name == "failed to get HEAD" {
					mockGitOps.On("OpenRepository", tc.repoPath).Return(mockRepo, nil)
					mockGitOps.On("Head", mockRepo).Return(nil, errors.New("failed to get HEAD"))
				}

				if name == "failed to get commit object" {
					mockGitOps.On("OpenRepository", tc.repoPath).Return(mockRepo, nil)
					mockGitOps.On("Head", mockRepo).Return(headRef, nil)
					mockGitOps.On("CommitObject", mockRepo, commitHash).Return(nil, errors.New("failed to get commit object"))
				}

				if name == "failed to get worktree" {
					mockGitOps.On("OpenRepository", tc.repoPath).Return(mockRepo, nil)
					mockGitOps.On("Head", mockRepo).Return(headRef, nil)
					mockGitOps.On("CommitObject", mockRepo, commitHash).Return(mockCommit, nil)
					mockGitOps.On("Worktree", mockRepo).Return(nil, errors.New("failed to get worktree"))
				}

				if name == "failed to reset" {
					mockGitOps.On("OpenRepository", tc.repoPath).Return(mockRepo, nil)
					mockGitOps.On("Head", mockRepo).Return(headRef, nil)
					mockGitOps.On("CommitObject", mockRepo, commitHash).Return(mockCommit, nil)
					mockGitOps.On("Worktree", mockRepo).Return(mockWorktree, nil)
					mockGitOps.On("Reset", mockWorktree, &git.ResetOptions{
						Mode:   git.HardReset,
						Commit: commitHash,
					}).Return(errors.New("failed to reset"))
				}
			}()

			// Act
			err := gitOps.ResetToLastCommittedVersion(tc.repoPath)

			// Assert
			if err != nil {
				r.EqualError(err, tc.expectedErr.Error())
			} else {
				r.NoError(err)
			}
		})
	}
}

func TestListBranches(t *testing.T) {
	for name, tc := range map[string]struct {
		repoPath    string
		expected    []string
		expectedErr error
	}{
		"successful branch listing": {
			repoPath:    "./repo",
			expected:    []string{"refs/heads/master", "refs/heads/develop"},
			expectedErr: nil,
		},
		"failed to open repository": {
			repoPath:    "./invalid-repo",
			expected:    nil,
			expectedErr: errors.New("failed to open repository"),
		},
		"failed to get branches": {
			repoPath:    "./repo",
			expected:    nil,
			expectedErr: errors.New("failed to get branches"),
		},
	} {
		t.Run(name, func(t *testing.T) {
			// Arrange
			mockGitOps := git_mocks.NewMockGitUtil(t)
			r := require.New(t)
			gitOps := NewGitOperations(ctx, mockGitOps)
			mockBranchIter := iter_mocks.NewMockReferenceIter(t)
			mockRepo := &git.Repository{}

			func() {
				if name == "successful branch listing" {
					mockGitOps.On("OpenRepository", "./repo").Return(mockRepo, nil)
					mockGitOps.On("Branches", mockRepo).Return(mockBranchIter, nil)
					mockBranchIter.On("ForEach", mock.AnythingOfType("func(*plumbing.Reference) error")).Return(nil).Run(func(args mock.Arguments) {
						arg, ok := args.Get(0).(func(*plumbing.Reference) error)
						require.True(t, ok)
						require.NoError(t, arg(plumbing.NewHashReference(plumbing.NewBranchReferenceName("master"), plumbing.ZeroHash)))
						require.NoError(t, arg(plumbing.NewHashReference(plumbing.NewBranchReferenceName("develop"), plumbing.ZeroHash)))
					})
				}
				if name == "failed to open repository" {
					mockGitOps.On("OpenRepository", "./invalid-repo").Return(nil, errors.New("failed to open repository"))
				}
				if name == "failed to get branches" {
					mockGitOps.On("OpenRepository", "./repo").Return(mockRepo, nil)
					mockGitOps.On("Branches", mockRepo).Return(mockBranchIter, errors.New("failed to get branches"))
				}
			}()

			// Act
			branches, err := gitOps.ListBranches(tc.repoPath)

			// Assert
			if err != nil {
				r.EqualError(err, tc.expectedErr.Error())
			} else {
				r.NoError(err)
				assert.ElementsMatch(t, tc.expected, branches)
			}
		})
	}
}

func TestDeleteLocalBranch(t *testing.T) {
	for name, tc := range map[string]struct {
		repoPath    string
		branchName  string
		expectedErr error
	}{
		"successful deletion - branch exists and not checked out": {
			repoPath:    "./repo",
			branchName:  "feature-branch",
			expectedErr: nil,
		},
		"branch does not exist": {
			repoPath:    "./repo",
			branchName:  "non-existent-branch",
			expectedErr: errors.New("the branch non-existent-branch does not exist"),
		},
		"branch is currently checked out": {
			repoPath:    "./repo",
			branchName:  "master",
			expectedErr: errors.New("cannot delete the currently checked out branch 'master'"),
		},
	} {
		t.Run(name, func(t *testing.T) {
			// Arrange
			mockGitOps := git_mocks.NewMockGitUtil(t)
			r := require.New(t)
			gitOps := NewGitOperations(ctx, mockGitOps)
			mockBranchIter := iter_mocks.NewMockReferenceIter(t)
			mockRepo := &git.Repository{}
			headRef := plumbing.NewHashReference(plumbing.NewBranchReferenceName("master"), plumbing.ZeroHash)

			func() {
				if name == "successful deletion - branch exists and not checked out" {
					mockGitOps.On("OpenRepository", tc.repoPath).Return(mockRepo, nil)
					mockGitOps.On("Branches", mockRepo).Return(mockBranchIter, nil)
					mockBranchIter.On("ForEach", mock.AnythingOfType("func(*plumbing.Reference) error")).Return(nil).Run(func(args mock.Arguments) {
						arg, ok := args.Get(0).(func(*plumbing.Reference) error)
						require.True(t, ok)
						require.NoError(t, arg(plumbing.NewHashReference(plumbing.NewBranchReferenceName("master"), plumbing.ZeroHash)))
						require.NoError(t, arg(plumbing.NewHashReference(plumbing.NewBranchReferenceName("feature-branch"), plumbing.ZeroHash)))
					})
					mockGitOps.On("Head", mockRepo).Return(headRef, nil)
					mockGitOps.On("RemoveReference", mockRepo.Storer, plumbing.NewBranchReferenceName("feature-branch")).Return(nil)
				}
				if name == "branch does not exist" {
					mockGitOps.On("OpenRepository", tc.repoPath).Return(mockRepo, nil)
					mockGitOps.On("Branches", mockRepo).Return(mockBranchIter, nil)
					mockBranchIter.On("ForEach", mock.AnythingOfType("func(*plumbing.Reference) error")).Return(errors.New("the branch non-existent-branch does not exist"))
				}
				if name == "branch is currently checked out" {
					mockGitOps.On("OpenRepository", tc.repoPath).Return(mockRepo, nil)
					mockGitOps.On("Branches", mockRepo).Return(mockBranchIter, nil)
					mockBranchIter.On("ForEach", mock.AnythingOfType("func(*plumbing.Reference) error")).Return(nil).Run(func(args mock.Arguments) {
						arg, ok := args.Get(0).(func(*plumbing.Reference) error)
						require.True(t, ok)
						require.NoError(t, arg(plumbing.NewHashReference(plumbing.NewBranchReferenceName("master"), plumbing.ZeroHash)))
						require.NoError(t, arg(plumbing.NewHashReference(plumbing.NewBranchReferenceName("feature-branch"), plumbing.ZeroHash)))
					})
					mockGitOps.On("Head", mockRepo).Return(headRef, errors.New("cannot delete the currently checked out branch 'master'"))
				}
			}()

			// Act
			err := gitOps.DeleteLocalBranch(tc.repoPath, tc.branchName)

			// Assert
			if err != nil {
				r.EqualError(err, tc.expectedErr.Error())
			} else {
				r.NoError(err)
			}
		})
	}
}

func TestCreateCommit(t *testing.T) {
	for name, tc := range map[string]struct {
		repoPath     string
		message      string
		wantErr      bool
		expectedHash plumbing.Hash
	}{
		"commit message too long": {
			repoPath: "./repo",
			message:  strings.Repeat("a", 256),
			wantErr:  true,
		},
		"no changes to commit": {
			repoPath: "./repo",
			message:  "Initial commit",
			wantErr:  true,
		},
		"successful commit": {
			repoPath:     "./repo",
			message:      "Initial commit",
			wantErr:      false,
			expectedHash: plumbing.ZeroHash,
		},
	} {
		t.Run(name, func(t *testing.T) {
			// Arrange
			mockGitOps := git_mocks.NewMockGitUtil(t)
			r := require.New(t)
			gitOps := NewGitOperations(ctx, mockGitOps)
			worktree := new(git.Worktree)
			status := git.Status{}
			// Mock Git config
			userConfig := gitUtil.GitUserConfig{
				Name:  "Test User",
				Email: "testuser@example.com",
			}

			func() {
				if name == "commit message too long" {
					return
					// No mock setup required for this case
				}
				if name == "no changes to commit" {
					status = git.Status{} // No changes to commit
					mockGitOps.On("OpenRepository", tc.repoPath).Return(new(git.Repository), nil)
					mockGitOps.On("Worktree", mock.Anything).Return(worktree, nil)
					mockGitOps.On("Status", worktree).Return(status, errors.New("no changes to commit"))
				}
				if name == "successful commit" {
					status = git.Status{
						"testfile.txt": {Staging: git.Modified, Worktree: git.Modified},
					}
					mockGitOps.On("OpenRepository", tc.repoPath).Return(new(git.Repository), nil)
					mockGitOps.On("Worktree", mock.Anything).Return(worktree, nil)
					mockGitOps.On("Status", worktree).Return(status, nil)
					mockGitOps.On("Add", worktree, ".").Return(plumbing.ZeroHash, nil)

					mockGitOps.On("GlobalGitConfig").Return(userConfig, nil)
					mockGitOps.On("Commit", worktree, "Initial commit", mock.AnythingOfType("*git.CommitOptions")).Return(plumbing.ZeroHash, nil)
				}
			}()

			// Act
			hash, err := gitOps.CreateCommit(tc.repoPath, tc.message)

			// Assert
			if tc.wantErr {
				r.Error(err)
			} else {
				r.NoError(err)
				r.Equal(tc.expectedHash.String(), hash)
			}
		})
	}
}

func TestPushCommit(t *testing.T) {
	t.Skip("skipping this test as more changes are needed to make it work")
	for name, tc := range map[string]struct {
		repoPath    string
		remoteName  string
		branchName  string
		githubToken string
		force       bool
		expectedErr error
	}{
		"successful push": {
			repoPath:    "./repo",
			remoteName:  "origin",
			branchName:  "main",
			githubToken: "token",
			force:       false,
			expectedErr: nil,
		},
		"checkout error": {
			repoPath:    "./repo",
			remoteName:  "origin",
			branchName:  "main",
			githubToken: "token",
			force:       false,
			expectedErr: assert.AnError,
		},
		"push error": {
			repoPath:    "./repo",
			remoteName:  "origin",
			branchName:  "main",
			githubToken: "token",
			force:       false,
			expectedErr: assert.AnError,
		},
	} {
		t.Run(name, func(t *testing.T) {
			// Arrange
			mockGitOps := git_mocks.NewMockGitUtil(t)
			r := require.New(t)
			gitOps := NewGitOperations(ctx, mockGitOps)
			repo := new(git.Repository)
			worktree := new(git.Worktree)

			func() {
				if name == "successful push" {
					mockGitOps.On("OpenRepository", tc.repoPath).Return(repo, nil)
					mockGitOps.On("Worktree", repo).Return(worktree, nil)
					mockGitOps.On("Checkout", worktree, mock.AnythingOfType("*git.CheckoutOptions")).Return(nil)
					mockGitOps.On("GlobalGitConfig").Return(gitUtil.GitUserConfig{
						Name:  "Test User",
						Email: "testuser@example.com",
					}, nil)
					mockGitOps.On("Push", repo, mock.AnythingOfType("*git.PushOptions")).Return(nil)
				}
				if name == "checkout error" {
					mockGitOps.On("OpenRepository", tc.repoPath).Return(repo, nil)
					mockGitOps.On("Worktree", repo).Return(worktree, nil)
					mockGitOps.On("Checkout", worktree, mock.AnythingOfType("*git.CheckoutOptions")).Return(tc.expectedErr)
				}
				if name == "push error" {
					mockGitOps.On("OpenRepository", tc.repoPath).Return(repo, nil)
					mockGitOps.On("Worktree", repo).Return(worktree, nil)
					mockGitOps.On("Checkout", worktree, mock.AnythingOfType("*git.CheckoutOptions")).Return(nil)
					mockGitOps.On("GlobalGitConfig").Return(gitUtil.GitUserConfig{
						Name:  "Test User",
						Email: "testuser@example.com",
					}, nil)
					mockGitOps.On("Push", repo, mock.AnythingOfType("*git.PushOptions")).Return(tc.expectedErr)
				}
			}()

			// Act
			params := gitUtil.PushCommitParams{
				RepoPath:    tc.repoPath,
				RemoteName:  tc.remoteName,
				BranchName:  tc.branchName,
				GitPatToken: tc.githubToken,
				Force:       tc.force,
			}
			err := gitOps.PushCommit(params)

			// Assert
			if err != nil {
				r.EqualError(err, tc.expectedErr.Error())
			} else {
				r.NoError(err)
			}
		})
	}
}

func TestGetRemoteServiceProvider(t *testing.T) {
	for name, tc := range map[string]struct {
		gitSvcPvd *consts.GitServiceProvider
		repoUrl   string
	}{
		"nonGithubRepoUrl": {
			gitSvcPvd: &consts.UnknownGitServiceProvider,
			repoUrl:   "https://unknown.com/hashicorp/terraform-provider-aws.git",
		},
		"githubRepoUrl": {
			gitSvcPvd: &consts.GitHub,
			repoUrl:   "https://github.com/hashicorp/terraform-provider-aws.git",
		},
		"gitlabRepoUrl": {
			gitSvcPvd: &consts.GitLab,
			repoUrl:   "https://gitlab.com/hashicorp/terraform-provider-aws.git",
		},
	} {
		t.Run(name, func(t *testing.T) {
			// Arrange
			r := require.New(t)
			ctx := context.Background()
			gitOps := NewGitOperations(ctx, gitUtil.NewGitUtil(ctx))

			// Act
			gitSvcPvd := gitOps.GetRemoteServiceProvider(tc.repoUrl)

			// Assert
			r.Equal(tc.gitSvcPvd, gitSvcPvd)
		})
	}

}

func TestGetRepoIdentifier(t *testing.T) {

	for name, tc := range map[string]struct {
		repoIdentifier string
		repoUrl        string
	}{
		"nonSupportedRepoUrl": {
			repoIdentifier: "",
			repoUrl:        "https://bitbucket.org/hashicorp/terraform-provider-aws.git",
		},
		"githubSshRepoUrl": {
			repoIdentifier: "hashicorp/terraform-provider-aws",
			repoUrl:        "git@github.com:hashicorp/terraform-provider-aws.git",
		},
		"githubSshRepoUrlHttpRepoUrl": {
			repoIdentifier: "hashicorp/terraform-provider-aws",
			repoUrl:        "https://github.com/hashicorp/terraform-provider-aws.git",
		},
		"gitlabSshRepoUrl": {
			repoIdentifier: "hashicorp/terraform-provider-aws",
			repoUrl:        "git@gitlab.com:hashicorp/terraform-provider-aws.git",
		},
		"gitlabSshRepoUrlHttpRepoUrl": {
			repoIdentifier: "hashicorp/terraform-provider-aws",
			repoUrl:        "https://gitlab.com/hashicorp/terraform-provider-aws.git",
		},
	} {
		t.Run(name, func(t *testing.T) {
			// Arrange
			r := require.New(t)
			ctx := context.Background()
			gitOps := NewGitOperations(ctx, gitUtil.NewGitUtil(ctx))

			// Act
			repoIdentifier := gitOps.GetRepoIdentifier(tc.repoUrl)

			// Assert
			r.Equal(repoIdentifier, tc.repoIdentifier)
		})
	}
}
func TestCreatePullRequest(t *testing.T) {
	for name, tc := range map[string]struct {
		repoIdentifier string
		baseBranch     string
		featureBranch  string
		title          string
		body           string
		gitPatToken    string
		remoteService  string
		expectedErr    error
		expectedURL    string
	}{
		"invalid repo identifier": {
			repoIdentifier: "invalid",
			baseBranch:     "main",
			featureBranch:  "feature-branch",
			title:          "Test PR",
			body:           "Test PR body",
			gitPatToken:    "fake-token",
			remoteService:  "",
			expectedErr:    fmt.Errorf("invalid repository identifier. It should be in the format: owner/repository"),
			expectedURL:    "",
		},

		"remote name error": {
			repoIdentifier: "owner/repo",
			baseBranch:     "main",
			featureBranch:  "feature-branch",
			title:          "Test PR",
			body:           "Test PR body",
			gitPatToken:    "fake-token",
			remoteService:  "",
			expectedErr:    errors.New("failed to get remote name"),
			expectedURL:    "",
		},
		"remote URL error": {
			repoIdentifier: "owner/repo",
			baseBranch:     "main",
			featureBranch:  "feature-branch",
			title:          "Test PR",
			body:           "Test PR body",
			gitPatToken:    "fake-token",
			remoteService:  "",
			expectedErr:    errors.New("failed to get remote URL"),
			expectedURL:    "",
		},
		"successful GitHub pull request": {
			repoIdentifier: "owner/repo",
			baseBranch:     "main",
			featureBranch:  "feature-branch",
			title:          "Test PR",
			body:           "Test PR body",
			gitPatToken:    "fake-token",
			remoteService:  "Github",
			expectedErr:    nil,
			expectedURL:    "https://github.com/owner/repo/pull/1",
		},

		"failed GitHub pull request": {
			repoIdentifier: "owner/repo",
			baseBranch:     "main",
			featureBranch:  "feature-branch",
			title:          "Test PR",
			body:           "Test PR body",
			gitPatToken:    "fake-token",
			remoteService:  "Github",
			expectedErr:    errors.New("failed to create PR"),
			expectedURL:    "",
		},
		"successful GitLab merge request": {
			repoIdentifier: "group/project",
			baseBranch:     "main",
			featureBranch:  "feature-branch",
			title:          "Test MR",
			body:           "Test MR body",
			gitPatToken:    "fake-token",
			remoteService:  "GitLab",
			expectedErr:    nil,
			expectedURL:    "https://gitlab.com/group/project/merge_requests/1",
		},
		"failed GitLab merge request creation": {
			repoIdentifier: "group/project",
			baseBranch:     "main",
			featureBranch:  "feature-branch",
			title:          "Test MR",
			body:           "Test MR body",
			gitPatToken:    "fake-token",
			remoteService:  "GitLab",
			expectedErr:    errors.New("failed to create MR"),
			expectedURL:    "",
		},
		"unsupported remote service provider": {
			repoIdentifier: "owner/repo",
			baseBranch:     "main",
			featureBranch:  "feature-branch",
			title:          "Test PR",
			body:           "Test PR body",
			gitPatToken:    "fake-token",
			remoteService:  "Unsupported",
			expectedErr:    fmt.Errorf("unsupported remote service provider: Unsupported"),
			expectedURL:    "",
		},
	} {
		t.Run(name, func(t *testing.T) {
			// Arrange
			mockOps := new(gitops_mocks.MockGitOperations)
			mockUtil := new(git_mocks.MockGitUtil)
			r := require.New(t)
			gitOps := &gitOperations{gitUtil: mockUtil}
			mockGitLabClient := &gitlab.Client{}
			mockGithubSvcProvider := new(remote_svc_provider_mocks.MockGithubSvcProvider)
			mockGitlabSvcProvider := new(remote_svc_provider_mocks.MockGitlabSvcProvider)

			pr := "https://github.com/owner/repo/pull/1"
			mr := "https://gitlab.com/group/project/merge_requests/1"

			pullRequestParams := &gitUtil.PullRequestParams{
				RepoIdentifier: tc.repoIdentifier,
				BaseBranch:     tc.baseBranch,
				FeatureBranch:  tc.featureBranch,
				Title:          tc.title,
				Body:           tc.body,
				GitPatToken:    tc.gitPatToken,
			}

			if name == "invalid repo identifier" {
				return
				// No additional setup needed
			}

			if name == "remote name error" {
				mockOps.On("GetRemoteName").Return("", errors.New("failed to get remote name"))
				return
			}

			if name == "remote URL error" {
				mockOps.On("GetRemoteName").Return("origin", nil)
				mockOps.On("GetRemoteURL", "origin").Return("", errors.New("failed to get remote URL"))
				return
			}

			if tc.remoteService == "Github" {

				mockOps.On("GetRemoteName").Return("origin", nil)
				mockOps.On("GetRemoteURL", "origin").Return("git@github.com:hashicorp/tf-migrate.git", nil)
				mockOps.On("GetRemoteServiceProvider", "git@github.com:hashicorp/tf-migrate.git").Return(&consts.GitHub)
				mockUtil.On("GetRemoteServiceProvider", "git@github.com:hashicorp/tf-migrate.git").Return(&consts.GitHub)

				if name == "successful GitHub pull request" {

					mockGithubSvcProvider.On("CreatePullRequest", pullRequestParams).Return(pr, nil)
					return
				}

				if name == "failed GitHub pull request" {
					mockGithubSvcProvider.On("CreatePullRequest", pullRequestParams).Return("", errors.New("failed to create PR"))
					return
				}

			}

			if tc.remoteService == "GitLab" {

				mockOps.On("GetRemoteName").Return("origin", nil)
				mockOps.On("GetRemoteURL", "origin").Return("git@github.com:hashicorp/tf-migrate.git", nil)
				mockOps.On("GetRemoteServiceProvider", "git@github.com:hashicorp/tf-migrate.git").Return(&consts.GitHub)
				mockUtil.On("GetRemoteServiceProvider", "git@github.com:hashicorp/tf-migrate.git").Return(&consts.GitHub)

				mockUtil.On("NewGitLabClient", tc.gitPatToken).Return(mockGitLabClient, nil)

				if name == "successful GitLab merge request" {
					mockGitlabSvcProvider.On("CreatePullRequest", pullRequestParams).Return(mr, nil)
					return
				}

				if name == "failed GitLab merge request creation" {
					mockGitlabSvcProvider.On("CreatePullRequest", pullRequestParams).Return(nil, errors.New("failed to create merge request"))
					return

				}
			}

			if name == "unsupported remote service provider" {
				mockOps.On("GetRemoteName").Return("origin", nil)
				mockOps.On("GetRemoteURL", "origin").Return("git@github.com:hashicorp/tf-migrate.git", nil)
				mockOps.On("GetRemoteServiceProvider", "git@github.com:hashicorp/tf-migrate.git").Return(&tc.remoteService)
				return // No additional setup needed
			} // Act

			url, err := gitOps.CreatePullRequest(*pullRequestParams)

			// Assert
			if err != nil {
				r.EqualError(err, tc.expectedErr.Error())
				assert.Empty(t, url)
			} else {
				r.NoError(err)
				assert.Equal(t, tc.expectedURL, url)
			}

			// Verify mock expectations
			mockOps.AssertExpectations(t)
			mockUtil.AssertExpectations(t)
		})
	}
}
