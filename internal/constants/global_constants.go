package constants

type GitServiceProvider string

var (

	// GitHub and GitLab are the supported Git service providers.
	GitHub                    GitServiceProvider = "github.com"
	GitLab                    GitServiceProvider = "gitlab.com"
	UnknownGitServiceProvider GitServiceProvider = "unknown"
	TfRemoteScheme                               = `https`
)
