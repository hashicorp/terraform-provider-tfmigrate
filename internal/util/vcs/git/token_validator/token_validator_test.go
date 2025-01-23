package token_validator

import (
	"context"
	cliErrs "terraform-provider-tfmigrate/internal/cli_errors"
	consts "terraform-provider-tfmigrate/internal/constants"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewTokenValidatorFactory(t *testing.T) {
	ctx := context.Background()

	for name, tc := range map[string]struct {
		tokenValidatorFactory TokenValidatorFactory
	}{
		"success": {
			tokenValidatorFactory: &tokenValidatorFactory{
				ctx: ctx,
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			factory := NewTokenValidatorFactory(ctx)
			r.NotNil(factory)
			r.Equal(tc.tokenValidatorFactory, factory)
		})

	}
}

func TestNewTokenValidator(t *testing.T) {
	ctx := context.Background()
	factory := NewTokenValidatorFactory(ctx)

	for name, tc := range map[string]struct {
		err                error
		gitServiceProvider *consts.GitServiceProvider
		tokenValidator     TokenValidator
	}{
		"GithubTokenValidator": {
			gitServiceProvider: &consts.GitHub,
		},
		"GitServiceProviderNotRecognised": {
			gitServiceProvider: &consts.UnknownGitServiceProvider,
			err:                cliErrs.ErrGitServiceProviderNotSupported,
		},
		"GitServiceProviderNil": {
			err: cliErrs.ErrGitServiceProviderNotSupported,
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			tokenValidator, err := factory.NewTokenValidator(tc.gitServiceProvider)

			if err != nil {
				r.Nil(tokenValidator)
				r.Equal(tc.err, err)
				return
			}

			r.NoError(err)
			r.NotNil(tokenValidator)

		})
	}
}
