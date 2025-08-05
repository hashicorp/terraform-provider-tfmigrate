package remote_svc_provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	cliErrs "terraform-provider-tfmigrate/internal/cli_errors"

	byteUtil "terraform-provider-tfmigrate/internal/util/byte"

	"terraform-provider-tfmigrate/internal/constants"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

func handleNonSuccessResponseFromVcsApi(resp *http.Response) (int, error) {
	if resp == nil {
		tflog.Error(context.Background(), fmt.Sprintf("error fetching repository details resp %v", resp))
		return 0, cliErrs.ErrUnknownError
	}

	responseBody := getResponseBodyFromHttpResponse(resp)
	responseHeaders := getHeaderFromHttpResponse(resp)
	statusCode := resp.StatusCode
	tflog.Error(context.Background(), fmt.Sprintf("received api response: %s, responseHeaders: %s, statusCode:  %d", responseBody, responseHeaders, statusCode))

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
func gitTokenErrorHandler(err error, statusCode ...int) (string, error) {
	if err == nil {
		return "", nil
	}

	tflog.Error(context.Background(), fmt.Sprintf("error validating git token err: %v", err))

	switch {
	case errors.Is(err, cliErrs.ErrGitServiceProviderNotSupported):
		return constants.SuggestUsingSupportedVcsProvider, err
	case
		errors.Is(err, cliErrs.ErrTfGitPatTokenNotSet),
		errors.Is(err, cliErrs.ErrTfGitPatTokenEmpty),
		errors.Is(err, cliErrs.ErrTfGitPatTokenFineGrained),
		errors.Is(err, cliErrs.ErrTfGitPatTokenInvalid):
		return fmt.Sprintf(constants.SuggestSettingValidTokenValue, constants.GitTokenEnvName), err
	case errors.Is(err, cliErrs.ErrTokenExpired):
		return fmt.Sprintf(constants.SuggestSettingUnexpiredToken, constants.GitTokenEnvName), err
	case errors.Is(err, cliErrs.ErrTokenDoesNotHaveAccessToOrg):
		return constants.SuggestProvidingAccessToToken, err
	case errors.Is(err, cliErrs.ErrTokenDoesNotHaveReadPermission):
		return constants.SuggestProvidingRepoReadPermissionToToken, err
	case errors.Is(err, cliErrs.ErrTokenDoesNotHaveWritePermission):
		return constants.SuggestProvidingRepoWritePermissionToToken, err
	case errors.Is(err, cliErrs.ErrBitbucketTokenTypeNotSupported):
		return fmt.Sprintf(constants.SuggestSettingValidTokenValue, constants.GitTokenEnvName), err
	case errors.Is(err, cliErrs.ErrTokenDoesNotHavePrWritePermission):
		return constants.SuggestCheckingApiDocs, err
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
func getResponseBodyFromHttpResponse(response *http.Response) string {
	buf := new(bytes.Buffer)
	if _, err := io.Copy(buf, response.Body); err != nil {
		tflog.Error(context.Background(), fmt.Sprintf("error while reading response body error %v", err))
	}
	return prettifyJsonResponseBody(buf.String())
}

// getHeaderFromHttpResponse gets the header from the http response.
func getHeaderFromHttpResponse(response *http.Response) string {
	headersJson, err := json.MarshalIndent(response.Header, "", "  ")
	if err != nil {
		tflog.Error(context.Background(), fmt.Sprintf("error while pretty printing headers, err: %v ", err))
		return fmt.Sprintf("%v", response.Header)
	}
	return string(headersJson)
}

// prettifyJsonResponseBody prettifies the json response body.
func prettifyJsonResponseBody(jsonString string) string {
	prettyJson, err := byteUtil.PrettyPrintJSON([]byte(jsonString))
	if err != nil {
		tflog.Error(context.Background(), fmt.Sprintf("error while pretty printing json, err: %v ", err))
		return jsonString
	}
	return prettyJson
}
