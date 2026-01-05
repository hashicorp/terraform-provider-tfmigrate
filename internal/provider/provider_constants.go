// Copyright IBM Corp. 2024, 2025
// SPDX-License-Identifier: MPL-2.0

package provider

const (
	ProviderPatTokenMissing         = "Missing Git PAT Token."
	ProviderPatTokenMissingDetailed = "The provider cannot create the VCS API client as there is a missing or empty value for the VCS API client. Set the password value in the configuration or use the %s environment variable. If either is already set, ensure the value is not empty."

	DirPathDoesNotExist               = "Specified Directory Path does not exist."
	DirPathDoesNotExistDetailed       = "The given directory path %s does not exist. Please provide a valid directory path."
	UpdateActionNotSupported          = "Update Action is not supported."
	UpdateActionNotSupportedDetailed  = "This resource does not support update operation; No Acton will be performed."
	DestroyActionNotSupported         = "Destroy Action is not supported for this resource."
	DestroyActionNotSupportedDetailed = "This resource does not support destroy operation; No Acton will be performed."

	TerraformInitSuccess = "Terraform Init Completed."
	TerraformInitFailed  = "Terraform Init Failed."
	TerraformPlanSuccess = "Add %d, Change %d, Remove %d"
	TerraformPlanFailed  = "Terraform Plan Failed."
)
