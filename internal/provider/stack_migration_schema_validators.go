package provider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// absolutePathValidator is a custom validator that checks if the provided path is an absolute path.
type absolutePathValidator struct{}

// orgEnvNameValidator is a custom validator that checks if the organization name is provided via the `TFE_ORGANIZATION` environment variable.
type orgEnvNameValidator struct{}

// projectEnvNameValidator is a custom validator that checks if the project name is provided via the `TFE_PROJECT` environment variable.
type projectEnvNameValidator struct{}

// Description returns the validator's description.
func (a absolutePathValidator) Description(_ context.Context) string {
	return "absolute path to Stack configuration files"
}

// MarkdownDescription returns the validator's description in Markdown format.
func (a absolutePathValidator) MarkdownDescription(_ context.Context) string {
	return "absolute path to Stack configuration files"
}

// ValidateString validates that the path is absolute.
func (a absolutePathValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			fmt.Sprintf("Invalid value for attribute %s", attrConfigurationDir),
			fmt.Sprintf("The path %s cannot be null. Please provide a valid absolute path.", req.Path.String()),
		)
		return
	}

	if req.ConfigValue.IsUnknown() {
		tflog.Debug(ctx, fmt.Sprintf("Attribute %s is unknown, skipping validation", attrConfigurationDir))
		return
	}

	sourceBundlePath := req.ConfigValue.ValueString()
	tflog.Debug(ctx, fmt.Sprintf("Validating absolute path: %s", sourceBundlePath))

	// check if a path exists and is a directory
	fileInfo, err := os.Stat(sourceBundlePath)
	if os.IsNotExist(err) {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Path Does Not Exist",
			fmt.Sprintf("The path %q does not exist. Please provide a valid absolute path.", sourceBundlePath),
		)
		return
	}

	// check if the path is a directory
	if !fileInfo.IsDir() {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Path Is Not a Directory",
			fmt.Sprintf("The path %q is not a directory. Please provide a valid absolute directory path.", sourceBundlePath),
		)
		return
	}

	if err != nil {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Error Accessing Path",
			fmt.Sprintf("An error occurred while accessing the path %q: %s", sourceBundlePath, err.Error()),
		)
		return
	}

	if !filepath.IsAbs(sourceBundlePath) {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid Path",
			fmt.Sprintf("The path %q is not absolute. Please provide an absolute path.", sourceBundlePath),
		)
	}
}

// Description returns a human-readable description of the organization environment variable validator.
func (o orgEnvNameValidator) Description(_ context.Context) string {
	return "value defaults to the `TF_ORGANIZATION` environment variable."
}

// MarkdownDescription returns a Markdown description of the organization environment variable validator.
func (o orgEnvNameValidator) MarkdownDescription(_ context.Context) string {
	return "value defaults to `TF_ORGANIZATION` environment variable."
}

// ValidateString validates the organization name from the environment variable `TFE_ORGANIZATION`.
func (o orgEnvNameValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() {
		if _, exists := os.LookupEnv(TfeOrganizationEnvName); !exists {
			resp.Diagnostics.AddAttributeError(
				req.Path,
				"Invalid Organization Name",
				"The organization name cannot be null. Please provide a valid organization name or set the TFE_ORGANIZATION environment variable.",
			)
			return
		}
	}

	if req.ConfigValue.IsUnknown() {
		tflog.Debug(ctx, "Organization name is unknown, skipping validation")
		return
	}

	orgVal := req.ConfigValue.ValueString()
	exists := false

	// Check if the organization name is provided in the resource attribute
	if orgVal != "" {
		tflog.Debug(ctx, fmt.Sprintf("Organization name %s provided in resource attribute, skipping environment variable check", orgVal))
		return
	}

	if orgVal, exists = os.LookupEnv(TfeOrganizationEnvName); !exists || orgVal == "" {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Missing Organization Name",
			"A TFE organization name is required via the provider configuration or the TFE_ORGANIZATION environment variable.",
		)
		return
	}

	// validate organization name length between 3 and 40 characters
	if len(orgVal) < 3 || len(orgVal) > 40 {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Organization name constraint Violation",
			organizationNameConstraintViolationMsg,
		)
		return
	}

	// validate organization name against regex `^[a-zA-Z0-9 _-]+$`
	if !nameRegex.MatchString(orgVal) {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Organization name constraint Violation",
			organizationNameConstraintViolationMsg,
		)
		return
	}

	tflog.Debug(ctx, fmt.Sprintf("Organization name from environment variable: %s", orgVal))
}

// Description returns a human-readable description of the project environment variable validator.
func (p projectEnvNameValidator) Description(_ context.Context) string {
	return "value defaults to the `TFE_PROJECT` environment variable."
}

// MarkdownDescription returns a Markdown description of the project environment variable validator.
func (p projectEnvNameValidator) MarkdownDescription(_ context.Context) string {
	return "value defaults to `TFE_PROJECT` environment variable."
}

// ValidateString validates the project name from the environment variable `TFE_PROJECT`.
func (p projectEnvNameValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() {
		if _, exists := os.LookupEnv(TfeProjectEnvName); !exists {
			resp.Diagnostics.AddAttributeError(
				req.Path,
				"Invalid Project Name",
				"The project name cannot be null. Please provide a valid project name or set the TFE_PROJECT environment variable.",
			)
			return
		}
	}

	if req.ConfigValue.IsUnknown() {
		return
	}

	projectVal := req.ConfigValue.ValueString()
	exists := false

	// Check if the project name is provided in the resource attribute
	if projectVal != "" {
		tflog.Debug(ctx, fmt.Sprintf("Project name %s provided in resource attribute, skipping environment variable check", projectVal))
		return
	}

	// If the project name is not provided in the resource attribute, check the environment variable
	if projectVal, exists = os.LookupEnv(TfeProjectEnvName); !exists || projectVal == "" {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Missing Project Name",
			"A TFE project name is required via the provider configuration or the TFE_PROJECT environment variable.",
		)
	}

	// validate project name length between 3 and 40 characters
	if len(projectVal) < 3 || len(projectVal) > 40 {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Project name constraint Violation",
			projectNameConstraintViolationMsg,
		)
		return
	}

	// validate project name against regex `^[a-zA-Z0-9 _-]+$`
	if !nameRegex.MatchString(projectVal) {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Project name constraint Violation",
			projectNameConstraintViolationMsg,
		)
		return
	}

	tflog.Debug(ctx, fmt.Sprintf("Project name from environment variable: %s", projectVal))
}
