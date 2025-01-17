package token_validator

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/hashicorp/go-hclog"

	cliErrs "terraform-provider-tfmigrate/internal/cli_errors"

	byteUtil "terraform-provider-tfmigrate/internal/util/byte"

	"terraform-provider-tfmigrate/internal/constants"
)

// handleNonSuccessResponse handles the non-success response.
func handleNonSuccessResponse(resp *http.Response, logger hclog.Logger) (int, error) {

	if resp == nil {
		logger.Error(fmt.Sprintf("error fetching repository details resp %v", resp))
		return 0, cliErrs.ErrUnknownError
	}

	return handleGitRepoResponse(resp, logger)
}

func handleGitRepoResponse(resp *http.Response, logger hclog.Logger) (int, error) {
	responseBody := getResponseBodyFromHttpResponse(resp, logger)
	responseHeaders := getHeaderFromHttpResponse(resp, logger)
	statusCode := resp.StatusCode
	logger.Error(fmt.Sprintf("received api response: %s, responseHeaders: %s, statusCode:  %d", responseBody, responseHeaders, statusCode))

	if statusCode >= http.StatusInternalServerError {
		return statusCode, cliErrs.ErrServerError
	}

	// validate the response status code 5xx.
	switch statusCode {
	case http.StatusUnauthorized:
		return statusCode, cliErrs.ErrTokenExpired
	case http.StatusForbidden:
		return statusCode, cliErrs.ErrTokenDoesNotHaveAccessToOrg
	case http.StatusNotFound:
		return statusCode, cliErrs.ErrRepositoryNotFound
	default:
		return statusCode, cliErrs.ErrUnexpectedStatusCode
	}
}

// gitTokenErrorHandler handles the git token error and returns appropriate suggestions applicable to the error.
func gitTokenErrorHandler(err error, logger hclog.Logger, statusCode ...int) (string, error) {
	if err == nil {
		return "", nil
	}

	logger.Error(fmt.Sprintf("error validating git token err: %v", err))

	switch {
	case errors.Is(err, cliErrs.ErrGitServiceProviderNotSupported):
		return constants.SuggestUsingGithubOrGitlab, err
	case
		errors.Is(err, cliErrs.ErrGithubTokenNotSet),
		errors.Is(err, cliErrs.ErrGithubTokenEmpty),
		errors.Is(err, cliErrs.ErrGithubTokenFineGrained),
		errors.Is(err, cliErrs.ErrGithubTokenUnrecognized):
		return constants.SuggestSettingClassicGitHubTokenValue, err
	case errors.Is(err, cliErrs.ErrTokenExpired):
		return constants.SuggestSettingUnexpiredToken, err
	case errors.Is(err, cliErrs.ErrTokenDoesNotHaveAccessToOrg):
		return constants.SuggestProvidingAccessToToken, err
	case errors.Is(err, cliErrs.ErrTokenDoesNotHaveReadPermission):
		return constants.SuggestProvidingRepoReadPermissionToToken, err
	case errors.Is(err, cliErrs.ErrTokenDoesNotHaveWritePermission):
		return constants.SuggestProvidingRepoWritePermissionToToken, err
	case errors.Is(err, cliErrs.ErrRepositoryNotFound):
		return constants.SuggestValidatingRepoNameOrTokenDoesNotHaveAccessToRead, err
	case errors.Is(err, cliErrs.ErrResponsePermissionsNil),
		errors.Is(err, cliErrs.ErrUnexpectedStatusCode):
		return constants.SuggestCheckingApiDocs, err
	case errors.Is(err, cliErrs.ErrServerError):
		return constants.SuggestServerErrorSolution, fmt.Errorf("server error occurred during API call with status code: %d", statusCode[0])
	default:
		return constants.SuggestUnknownErrorSolution, cliErrs.ErrUnknownError
	}
}

// getResponseBodyFromHttpResponse gets the response body from the http response.
func getResponseBodyFromHttpResponse(response *http.Response, logger hclog.Logger) string {
	buf := new(bytes.Buffer)
	if _, err := io.Copy(buf, response.Body); err != nil {
		logger.Error(fmt.Sprintf("error while reading response body error %v", err))
	}
	return prettifyJsonResponseBody(buf.String(), logger)
}

// getHeaderFromHttpResponse gets the header from the http response.
func getHeaderFromHttpResponse(response *http.Response, logger hclog.Logger) string {
	headersJson, err := json.MarshalIndent(response.Header, "", "  ")
	if err != nil {
		logger.Error(fmt.Sprintf("error while pretty printing headers, err: %v ", err))
		return fmt.Sprintf("%v", response.Header)
	}
	return string(headersJson)
}

// prettifyJsonResponseBody prettifies the json response body.
func prettifyJsonResponseBody(jsonString string, logger hclog.Logger) string {
	prettyJson, err := byteUtil.PrettyPrintJSON([]byte(jsonString))
	if err != nil {
		logger.Error(fmt.Sprintf("error while pretty printing json, err: %v ", err))
		return jsonString
	}
	return prettyJson
}
