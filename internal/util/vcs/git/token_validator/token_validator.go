package token_validator

import (
	"context"

	"terraform-provider-tfmigrate/internal/util/vcs/git"

	consts "terraform-provider-tfmigrate/internal/constants"

	cliErrs "terraform-provider-tfmigrate/internal/cli_errors"
)

// tokenValidatorFactory implements TokenValidatorFactory.
type tokenValidatorFactory struct {
	ctx context.Context
}

// TokenValidator is the base interface for validating tokens.
type TokenValidator interface {
	ValidateToken(repoUrl string, repoIdentifier string) (string, error)
}

// TokenValidatorFactory is the factory interface for creating token validators.
type TokenValidatorFactory interface {
	NewTokenValidator(gitServiceProvider *consts.GitServiceProvider) (TokenValidator, error)
}

// NewTokenValidatorFactory creates a new instance of TokenValidatorFactory.
func NewTokenValidatorFactory(ctx context.Context) TokenValidatorFactory {
	return &tokenValidatorFactory{
		ctx: ctx,
	}
}

// NewTokenValidator CreateValidator returns the appropriate TokenValidator based on the provider.
func (f *tokenValidatorFactory) NewTokenValidator(gitServiceProvider *consts.GitServiceProvider) (TokenValidator, error) {
	if gitServiceProvider != nil && *gitServiceProvider == consts.GitHub {
		return &githubTokenValidator{
			ctx:        f.ctx,
			git:        git.NewGitUtil(f.ctx),
			githubUtil: git.NewGithubUtil(f.ctx),
		}, nil
	}

	if gitServiceProvider != nil && *gitServiceProvider == consts.GitLab {
		return &gitlabTokenValidator{
			ctx:        f.ctx,
			git:        git.NewGitUtil(f.ctx),
			gitlabUtil: git.NewGitlabUtil(f.ctx),
		}, nil
	}
	return nil, cliErrs.ErrGitServiceProviderNotSupported
}
