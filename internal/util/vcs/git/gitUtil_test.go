package git

import (
	"context"
	"os"
	"testing"

	consts "terraform-provider-tfmigrate/internal/constants"

	cliErrs "terraform-provider-tfmigrate/internal/cli_errors"

	"github.com/stretchr/testify/require"
)

func TestGetGitToken(t *testing.T) {
	for name, tc := range map[string]struct {
		err       error
		gitSvcPvd *consts.GitServiceProvider
		token     string
	}{
		"gitSvcPvdIsNil": {
			err: cliErrs.ErrGitServiceProviderNotSupported,
		},
		"unknownGitSvcPvd": {
			gitSvcPvd: &consts.UnknownGitServiceProvider,
			err:       cliErrs.ErrGitServiceProviderNotSupported,
		},
		"githubTokenNotSet": {
			gitSvcPvd: &consts.GitHub,
			err:       cliErrs.ErrTfGitPatTokenNotSet,
		},
		"githubTokenEmpty": {
			gitSvcPvd: &consts.GitHub,
			err:       cliErrs.ErrTfGitPatTokenEmpty,
			token:     "",
		},
		"githubTokenFineGrained": {
			gitSvcPvd: &consts.GitHub,
			err:       cliErrs.ErrTfGitPatTokenFineGrained,
			token:     "github_pat_12B85MNZZ1X7XmmpLbDvMj_TojrCCf7yKw9bOM8Doe2t6yq0hcHGZ672zbevH0kKwD5YAFEGLBLVWo2y3",
		},
		"githubTokenUnrecognised": {
			gitSvcPvd: &consts.GitHub,
			err:       cliErrs.ErrTfGitPatTokenInvalid,
			token:     "unrecognised_token_1234ABCDef5", //nolint:misspell
		},
		"githubTokenClassic": {
			gitSvcPvd: &consts.GitHub,
			token:     "ghp_aBp47xyKz45uXXf3JfDrg8w22EfFgY1ytpLM",
		},
		"gitlabTokenNotSet": {
			gitSvcPvd: &consts.GitLab,
			err:       cliErrs.ErrTfGitPatTokenNotSet,
		},
		"gitlabTokenEmpty": {
			gitSvcPvd: &consts.GitLab,
			err:       cliErrs.ErrTfGitPatTokenEmpty,
			token:     "",
		},
		"gitlabTokenValid": {
			gitSvcPvd: &consts.GitLab,
			token:     "glpat-zxy9KZZxNmzxyPqxLK5vJWxyLmK",
		},
	} {
		t.Run(name, func(t *testing.T) {
			// Arrange
			r := require.New(t)
			ctx := context.Background()
			gitOps := NewGitUtil(ctx)

			// Environment variable setup
			if name != "gitSvcPvdIsNil" && name != "unknownGitSvcPvd" {
				if name == "githubTokenNotSet" || name == "gitlabTokenNotSet" {
					if err := os.Unsetenv("TF_GIT_PAT_TOKEN"); err != nil {
						t.Fatalf("Error unsetting environment variable: %+v", err)
					}
				} else {
					if err := os.Setenv("TF_GIT_PAT_TOKEN", tc.token); err != nil {
						t.Fatalf("Error setting environment variable: %+v", err)
					}
				}
			}

			// Act
			token, err := gitOps.GetGitToken(tc.gitSvcPvd)

			// Assert
			if err != nil {
				r.Equal(tc.err, err)
				return
			}

			r.Equal(tc.token, token)
		})

		t.Cleanup(func() {
			if name != "gitSvcPvdIsNil" && name != "unknownGitSvcPvd" {
				_ = os.Unsetenv("TF_GIT_PAT_TOKEN") // Cleanup environment variable
			}
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
			gitOps := NewGitUtil(ctx)
			// Act
			repoIdentifier := gitOps.GetRepoIdentifier(tc.repoUrl)

			// Assert
			r.Equal(repoIdentifier, tc.repoIdentifier)
		})
	}

}

func TestGetOrgAndRepoName(t *testing.T) {

	for name, tc := range map[string]struct {
		orgName  string
		repoName string
		repoId   string
	}{
		"validRepoId": {
			orgName:  "hashicorp",
			repoName: "terraform-provider-aws",
			repoId:   "hashicorp/terraform-provider-aws",
		},
	} {
		t.Run(name, func(t *testing.T) {
			// Arrange
			r := require.New(t)
			ctx := context.Background()
			gitOps := NewGitUtil(ctx)
			// Act
			orgName, repoName := gitOps.GetOrgAndRepoName(tc.repoId)

			// Assert
			r.Equal(orgName, tc.orgName)
			r.Equal(repoName, tc.repoName)
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
			gitOps := NewGitUtil(ctx)
			// Act
			gitSvcPvd := gitOps.GetRemoteServiceProvider(tc.repoUrl)

			// Assert
			r.Equal(gitSvcPvd, tc.gitSvcPvd)
		})
	}
}
