package remote_svc_provider

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
		tokenValidatorFactory RemoteVcsSvcProviderFactory
	}{
		"success": {
			tokenValidatorFactory: &remoteSvcProviderFactory{
				ctx: ctx,
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			factory := NewRemoteSvcProviderFactory(ctx)
			r.NotNil(factory)
			r.Equal(tc.tokenValidatorFactory, factory)
		})

	}
}

func TestNewTokenValidator(t *testing.T) {
	ctx := context.Background()
	factory := NewRemoteSvcProviderFactory(ctx)

	for name, tc := range map[string]struct {
		err                error
		gitServiceProvider *consts.GitServiceProvider
		tokenValidator     RemoteVcsSvcProvider
	}{
		"GithubSvcProvider": {
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
			tokenValidator, err := factory.NewRemoteVcsSvcProvider(tc.gitServiceProvider)

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
