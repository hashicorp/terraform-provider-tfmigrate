package token_validator

import (
	"context"

	"terraform-provider-tfmigrate/internal/util/git"

	consts "terraform-provider-tfmigrate/internal/constants"

	"github.com/hashicorp/go-hclog"

	cliErrs "terraform-provider-tfmigrate/internal/cli_errors"
)

// tokenValidatorFactory implements TokenValidatorFactory.
type tokenValidatorFactory struct {
	ctx    context.Context
	logger hclog.Logger
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
func NewTokenValidatorFactory(ctx context.Context, logger hclog.Logger) TokenValidatorFactory {
	return &tokenValidatorFactory{
		ctx:    ctx,
		logger: logger,
	}
}

// NewTokenValidator CreateValidator returns the appropriate TokenValidator based on the provider.
func (f *tokenValidatorFactory) NewTokenValidator(gitServiceProvider *consts.GitServiceProvider) (TokenValidator, error) {
	if gitServiceProvider != nil && *gitServiceProvider == consts.GitHub {
		return &githubTokenValidator{
			ctx:        f.ctx,
			logger:     f.logger,
			git:        git.NewGitUtil(f.logger),
			githubUtil: git.NewGithubUtil(f.ctx, f.logger),
		}, nil
	}

	if gitServiceProvider != nil && *gitServiceProvider == consts.GitLab {
		return &gitlabTokenValidator{
			ctx:        f.ctx,
			logger:     f.logger,
			git:        git.NewGitUtil(f.logger),
			gitlabUtil: git.NewGitlabUtil(f.ctx, f.logger),
		}, nil
	}
	return nil, cliErrs.ErrGitServiceProviderNotSupported
}
