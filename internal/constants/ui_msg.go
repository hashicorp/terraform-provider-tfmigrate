package constants

const (
	// InvalidRepositoryIdentifier is the error displayed when the repository identifier is invalid.
	InvalidRepositoryIdentifier = `Invalid repository identifier: %s. Expected format: <owner>/<repo-name>`
	// ErrNoUpstreamBranch is the error displayed when no upstream branch is found for the current branch.
	ErrNoUpstreamBranch = `No upstream branch found for the current branch.`
	// ErrorFetchingRemote is the warning message displayed when the tool is unable to fetch the remote name.
	ErrorFetchingRemote = `Error fetching remote name, err: %v, all git operations will be skipped.`
	// ErrorFetchingRemoteURL is the warning message displayed when the tool is unable to fetch the remote URL.
	ErrorFetchingRemoteURL = `Error fetching remote URL, err: %v, all git operations will be skipped.`
	// ErrorValidatingGitToken is the error displayed when the tool is unable to validate the git token.
	ErrorValidatingGitToken = `Error validating the Git token: %v`
	// ErrorCreatingNewTokenvalidator is the error displayed when the tool is unable to create a new token validator.
	ErrorCreatingNewTokenvalidator = `Error creating new token validator: %v`
	// ErrorCreatingBranch is the warning message displayed when the tool is unable to create a branch.
	ErrorCreatingBranch = `Error creating or checking out branch %s, err: %v.`
	// WarnNotOnGithubOrGitlab is the warning displayed when the user's repository is not on GitHub.
	WarnNotOnGithubOrGitlab = `Your repository URL is %s. Only GitHub and Gitlab is supported.`
	// SuggestSettingClassicGitHubTokenValue is the suggestion displayed when the TF_GIT_PAT_TOKEN environment variable is not set with a classic GitHub token.
	SuggestSettingClassicGitHubTokenValue = `Set the value of the TF_GIT_PAT_TOKEN environment variable with a classic GitHub token to see all git operation related options.`
	// SuggestUsingGithub is the suggestion displayed when the repository is not hosted on GitHub.
	SuggestUsingGithubOrGitlab = `Repository must be hosted on GitHub Or Gitlab to see all git operation related options.`
	// SuggestSettingUnexpiredToken is the suggestion displayed when the TF_GIT_PAT_TOKEN environment variable is set with an expired token.
	SuggestSettingUnexpiredToken = `Set the TF_GIT_PAT_TOKEN environment variable with a non-expired classic GitHub token to enable all git operation related options.`
	// SuggestProvidingAccessToToken is the suggestion displayed when the token does not have access to the required organization.
	SuggestProvidingAccessToToken = `Authorize the token to access the required organization.`
	// SuggestSettingGitlabTokenValue is the suggestion displayed when the TF_GIT_PAT_TOKEN environment variable is not set.
	SuggestSettingGitlabTokenValue = `Set the value of the TF_GIT_PAT_TOKEN environment variable with a non-expired Gitlab token to see all git operation related options.`
	// SuggestProvidingRepoReadPermissionToToken is the suggestion displayed when the token does not have read permission to the required repository.
	SuggestProvidingRepoReadPermissionToToken = `Authorize the token to read the required repository.`
	// SuggestProvidingRepoWritePermissionToToken is the suggestion displayed when the token does not have write permission to the required repository.
	SuggestProvidingRepoWritePermissionToToken = `Authorize the token to write to the required repository.`
	// SuggestValidatingRepoNameOrTokenDoesNotHaveAccessToRead is the suggestion displayed when the repository name is incorrect or the token lacks read access.
	SuggestValidatingRepoNameOrTokenDoesNotHaveAccessToRead = `Ensure the repository name is correct or authorize the token to access the repository.`
	// SuggestCheckingApiDocs is the suggestion displayed when an unexpected status code or nil permissions are encountered.
	SuggestCheckingApiDocs = `Check the API documentation for more information.`
	// SuggestServerErrorSolution is the suggestion displayed when a server error occurs during an API call.
	SuggestServerErrorSolution = `Verify the following:
1. Please check the API https://api.github.com/repos/<owner>/<repo-name> is reachable and is giving proper response.
2. Check the API documentation for more information.
3. Try setting TF_MIGRATE_ENABLE_LOG=true to enable logs and check the logs for more information.
   This is not recommended for production environments, as it may expose sensitive information.
4. If the issue persists, please contact HashiCorp support.`
	// SuggestUnknownErrorSolution is the suggestion displayed when an unknown error occurs.
	SuggestUnknownErrorSolution = `Verify the following:
1. Please check that you are connected to the internet.
2. Try setting TF_MIGRATE_ENABLE_LOG=true to enable logs and check the logs for more information.
3. Optionally, you can set TF_MIGRATE_LOG_LEVEL=debug to see debug logs.
   This is not recommended for production environments, as it may expose sensitive information.
4. If the issue persists, please contact HashiCorp support.`
)
