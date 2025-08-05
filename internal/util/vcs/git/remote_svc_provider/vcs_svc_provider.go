package remote_svc_provider

import (
	"context"

	"terraform-provider-tfmigrate/internal/util/vcs/git"

	consts "terraform-provider-tfmigrate/internal/constants"

	cliErrs "terraform-provider-tfmigrate/internal/cli_errors"
)

// remoteSvcProviderFactory implements RemoteVcsSvcProviderFactory.
type remoteSvcProviderFactory struct {
	ctx context.Context
}

// RemoteVcsSvcProvider is the interface for performing operations on remote VCS services.
type RemoteVcsSvcProvider interface {
	ValidateToken(repoUrl string, repoIdentifier string, tokenFromProvider string) (string, error)
	CreatePullRequest(params git.PullRequestParams) (string, error)
}

// RemoteVcsSvcProviderFactory is the factory interface for creating RemoteVcsSvcProvider.
type RemoteVcsSvcProviderFactory interface {
	NewRemoteVcsSvcProvider(gitServiceProvider *consts.GitServiceProvider) (RemoteVcsSvcProvider, error)
}

// NewRemoteSvcProviderFactory creates a new instance of RemoteVcsSvcProviderFactory.
func NewRemoteSvcProviderFactory(ctx context.Context) RemoteVcsSvcProviderFactory {
	return &remoteSvcProviderFactory{
		ctx: ctx,
	}
}

// NewRemoteVcsSvcProvider returns the appropriate RemoteVcsSvcProvider based on the provider.
func (f *remoteSvcProviderFactory) NewRemoteVcsSvcProvider(gitServiceProvider *consts.GitServiceProvider) (RemoteVcsSvcProvider, error) {
	if gitServiceProvider != nil && *gitServiceProvider == consts.GitHub {
		return &githubSvcProvider{
			ctx:        f.ctx,
			git:        git.NewGitUtil(f.ctx),
			githubUtil: git.NewGithubUtil(f.ctx),
		}, nil
	}

	if gitServiceProvider != nil && *gitServiceProvider == consts.GitLab {
		return &gitlabSvcProvider{
			ctx:        f.ctx,
			git:        git.NewGitUtil(f.ctx),
			gitlabUtil: git.NewGitlabUtil(f.ctx),
		}, nil
	}

	if gitServiceProvider != nil && *gitServiceProvider == consts.Bitbucket {
		return NewBitbucketSvcProvider(f.ctx, git.NewGitUtil(f.ctx), git.NewBitbucketUtil(f.ctx)), nil
	}
	return nil, cliErrs.ErrGitServiceProviderNotSupported
}
