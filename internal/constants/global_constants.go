package constants

type GitServiceProvider string

var (

	// GitHub, GitLab, and Bitbucket are the supported Git service providers.
	GitHub                    GitServiceProvider = "github.com"
	GitLab                    GitServiceProvider = "gitlab.com"
	Bitbucket                 GitServiceProvider = "bitbucket.org"
	UnknownGitServiceProvider GitServiceProvider = "unknown"
)
