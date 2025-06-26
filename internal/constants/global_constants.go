package constants

type GitServiceProvider string

var (

	// GitHub and GitLab are the supported Git service providers.
	GitHub                    GitServiceProvider = "github.com"
	GitLab                    GitServiceProvider = "gitlab.com"
	UnknownGitServiceProvider GitServiceProvider = "unknown"

	TerraformRPCAPICookie string = "fba0991c9bcd453982f0d88e2da95940"
)
