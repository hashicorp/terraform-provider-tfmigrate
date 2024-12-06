// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

const (
	PROVIDER_PAT_TOKEN_MISSING          = "Missing Git PAT Token."
	PROVIDER_PAT_TOKEN_MISSING_DETAILED = "The provider cannot create the VCS API client as there is a missing or empty value for the VCS API client. Set the password value in the configuration or use the TFM_GITHUB_TOKEN environment variable. If either is already set, ensure the value is not empty."

	DIR_PATH_DOES_NOT_EXIST              = "Specified Directory Path does not exist."
	DIR_PATH_DOES_NOT_EXIST_DETAILED     = "The given directory path %s does not exist. Please provide a valid directory path."
	UPDATE_ACTION_NOT_SUPPORTED          = "Update Action is not supported."
	UPDATE_ACTION_NOT_SUPPORTED_DETAILED = "This resource does not support update operation; No Acton will be performed."
	DESTROY_ACTION_NOT_SUPPORTED         = "Destroy Action is not supported for this resource."

	TERRAFORM_INIT_SUCCESS = "Terraform Init Completed."
	TERRAFORM_INIT_FAILED  = "Terraform Init Failed."
	TERRAFORM_PLAN_SUCCESS = "Add %d, Change %d, Remove %d"
	TERRAFORM_PLAN_FAILED  = "Terrform Plan Failed."
)
