package constants

type GitServiceProvider string

var (

	// GitHub, GitLab, and Bitbucket are the supported Git service providers.
	GitHub                    GitServiceProvider = "github.com"
	GitLab                    GitServiceProvider = "gitlab.com"
	Bitbucket                 GitServiceProvider = "bitbucket.org"
	UnknownGitServiceProvider GitServiceProvider = "unknown"

	TerraformRPCAPICookie   = "fba0991c9bcd453982f0d88e2da95940"
	TerraformMagicCookieKey = "TERRAFORM_RPCAPI_COOKIE"
	TfRemoteScheme          = `https`
)

const (
	// GitTokenEnvName is the environment variable name for the Git Personal Access Token.
	GitTokenEnvName = "TF_GIT_PAT_TOKEN"
)
