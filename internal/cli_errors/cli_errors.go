package cli_errors

var (
	ErrGitSvcPvdNotSupported = CliOperationError("git service provider not supported")

	ErrTfGitPatTokenEmpty              = GitTokenError(`TF_GIT_PAT_TOKEN environment variable is empty`)
	ErrTfGitPatTokenNotSet             = GitTokenError(`TF_GIT_PAT_TOKEN environment variable not set`)
	ErrGitServiceProviderNotSupported  = GitTokenError(`git service provider not supported`)
	ErrRepositoryNotFound              = GitTokenError(`the repository was not found`)
	ErrResponsePermissionsNil          = GitTokenError(`token permissions array is nil or empty`)
	ErrTokenDoesNotHaveAccessToOrg     = GitTokenError(`the provided git token does not have access to the organization`)
	ErrTokenDoesNotHaveReadPermission  = GitTokenError(`the provided git token does not have read permission to the repository`)
	ErrTokenDoesNotHaveWritePermission = GitTokenError(`the provided git token does not have write permission to the repository`)
	ErrTokenExpired                    = GitTokenError(`the provided git token has expired`)
	ErrTfGitPatTokenFineGrained        = GitTokenError(`TF_GIT_PAT_TOKEN is a Github fine-grained token`)
	ErrTfGitPatTokenUnrecognized       = GitTokenError(`TF_GIT_PAT_TOKEN token type is not recognized`)
	ErrTfGitPatTokenValid              = GitTokenError(`TF_GIT_PAT_TOKEN is valid`)
	ErrTfGitPatTokenInvalid            = GitTokenError(`TF_GIT_PAT_TOKEN is invalid`)

	ErrServerError          = ApiError(`server error during API call`)
	ErrUnexpectedStatusCode = ApiError(`unexpected API status code`)
	ErrUnknownError         = ApiError(`unknown error occurred during API call`)
)

// CliOperationError represents the type of error that occurred during the CLI operation.
type CliOperationError string

func (e CliOperationError) Error() string {
	return string(e)
}

// GitTokenError represents errors during git Personal Access Token (PAT) extraction and validation.
type GitTokenError string

func (e GitTokenError) Error() string {
	return string(e)
}

// ApiError represents the type of error that occurred during the API call.
type ApiError string

func (e ApiError) Error() string {
	return string(e)
}
