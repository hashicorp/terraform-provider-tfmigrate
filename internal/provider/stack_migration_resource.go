// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"

	"github.com/hashicorp/go-tfe"

	tfeUtil "terraform-provider-tfmigrate/internal/util/tfe"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Define constants for the stack resource.
const (
	stackMigrationResourceName        = "_stack_migration"
	stackMigrationResourceDescription = "Defines a resource for migrating existing HCP Terraform workspaces to deployments within a non-VCS stack. Each workspace maps one-to-one with a stack deployment. The resource uploads configuration files to the stack and monitors the upload and deployment status."
	orgNameRegex                      = "^[a-zA-Z0-9 _-]+$" // Regex for valid organization names
	projectNameRegex
)

var (
	_ resource.Resource                   = &stackResource{}
	_ resource.ResourceWithConfigure      = &stackResource{}
	_ resource.ResourceWithValidateConfig = &stackResource{}
	_ resource.ResourceWithModifyPlan     = &stackResource{}

	// these statuses are: "converged", "errored", "canceled".
	stacksConfigTerminatingStatuses = []tfe.StackConfigurationStatus{
		tfe.StackConfigurationStatusConverged,
		tfe.StackConfigurationStatusErrored,
		tfe.StackConfigurationStatusCanceled,
	}
)

// stackResource is the implementation of the resource.
type stackResource struct {
	tfeClient   *tfe.Client
	orgName     string
	projectName string
	tfeUtil     tfeUtil.TfeUtil
}

// absolutePathValidator validates that a path is absolute.
type absolutePathValidator struct{}

// StackMigrationResourceModel describes the resource data model.
type StackMigrationResourceModel struct {
	ConfigFileDir          types.String `tfsdk:"config_file_dir"`          // ConfigFileDir is the directory path containing configuration files.
	ConfigStatus           types.String `tfsdk:"config_status"`            // ConfigStatus is the status of the stack configuration.
	CurrentConfigurationId types.String `tfsdk:"current_configuration_id"` // CurrentConfigurationId is the ID of the current stack configuration.
	Name                   types.String `tfsdk:"name"`                     // Name is the name of the stack Must be unique within the organization and project, must be a non-Vcs driven stack.
	Org                    types.String `tfsdk:"org"`                      // Org is the HCP Terraform organization name in which the stack exists. The value can be overridden by the `TFE_ORGANIZATION` environment variable.
	Project                types.String `tfsdk:"project"`                  // Project is the HCP Terraform project name in which the stack exists. The value can be overridden by the `TFE_PROJECT` environment variable.
	SourceBundleHash       types.String `tfsdk:"config_hash"`              // SourceBundleHash is the hash of the configuration files in the directory. This is used to detect changes in the configuration files.
}

// NewStackResource is a constructor for the stack resource.
func NewStackResource() resource.Resource {
	return &stackResource{}
}

// Description returns the validator's description.
func (v *absolutePathValidator) Description(_ context.Context) string {
	return "absolute path to Stack configuration files"
}

// MarkdownDescription returns the validator's description in Markdown format.
func (v *absolutePathValidator) MarkdownDescription(_ context.Context) string {
	return "absolute path to Stack configuration files"
}

// ValidateString validates that the path is absolute.
func (v *absolutePathValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid Path",
			"The path cannot be null or unknown. Please provide a valid absolute path.",
		)
		return
	}

	path := req.ConfigValue.ValueString()

	// check if path exists and is a directory
	fileInfo, err := os.Stat(path)
	if os.IsNotExist(err) {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Path Does Not Exist",
			fmt.Sprintf("The path %q does not exist. Please provide a valid absolute path.", path),
		)
		return
	}

	// check if the path is a directory
	if !fileInfo.IsDir() {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Path Is Not a Directory",
			fmt.Sprintf("The path %q is not a directory. Please provide a valid absolute directory path.", path),
		)
		return
	}

	if err != nil {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Error Accessing Path",
			fmt.Sprintf("An error occurred while accessing the path %q: %s", path, err.Error()),
		)
		return
	}

	if !filepath.IsAbs(path) {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid Path",
			fmt.Sprintf("The path %q is not absolute. Please provide an absolute path.", path),
		)
	}
}

// Schema defines the schema for the resource.
func (r *stackResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Version:             1,
		Description:         stackMigrationResourceDescription,
		MarkdownDescription: stackMigrationResourceDescription,
		Attributes: map[string]schema.Attribute{
			"org": schema.StringAttribute{
				MarkdownDescription: "The organization name. This is the organization where the stack exists. The value can be  overridden `TFE_ORGANIZATION` environment variable.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"project": schema.StringAttribute{
				MarkdownDescription: "The project name. This is the project where the stack exists. The value can be overridden by `TFE_PROJECT` environment variable.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The stack name.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 90),
					stringvalidator.RegexMatches(regexp.MustCompile(`^[a-zA-Z0-9 _-]+$`), "Stack name must be unique and between 3-40 characters and may contain valid characters including ASCII letters, numbers, spaces, as well as dashes (-), and underscores (_)."),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"config_file_dir": schema.StringAttribute{
				MarkdownDescription: "The directory path containing configuration files. Must be an absolute path.",
				Required:            true,
				Validators: []validator.String{
					&absolutePathValidator{},
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"config_hash": schema.StringAttribute{
				MarkdownDescription: "The hash of the configuration files in the directory. This is used to detect changes in the configuration files.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"current_configuration_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The ID of the current stack configuration. This is used to track the current configuration of the stack.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"config_status": schema.StringAttribute{
				MarkdownDescription: "The status of the stack configuration. This is used to track the status of the stack configuration upload.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

// ValidateConfig validates the configuration of the resource.
func (r *stackResource) ValidateConfig(ctx context.Context, request resource.ValidateConfigRequest, response *resource.ValidateConfigResponse) {
	var data StackMigrationResourceModel
	response.Diagnostics.Append(request.Config.Get(ctx, &data)...)
	if response.Diagnostics.HasError() {
		return
	}

	// Check if the organization is set and valid
	if data.Org.IsUnknown() {
		response.Diagnostics.AddAttributeError(
			path.Root("org"),
			"Invalid Organization",
			"The organization cannot be unknown. Please provide a valid organization name. Gets overridden by the `TFE_ORGANIZATION` environment variable if set.",
		)
		return
	}

	// Override organization from environment variable if set
	if envOrg, ok := os.LookupEnv(TfeOrganizationEnvName); ok && envOrg != "" {
		data.Org = types.StringValue(envOrg)
	}

	// Check if the organization is null
	if data.Org.IsNull() {
		response.Diagnostics.AddAttributeError(
			path.Root("org"),
			"Invalid Organization",
			"The organization cannot be null. Please provide a valid organization name. Gets overridden by the `TFE_ORGANIZATION` environment variable if set.",
		)
		return
	}

	// Validate the organization name against the regex pattern
	if !regexp.MustCompile(orgNameRegex).MatchString(data.Org.ValueString()) {
		response.Diagnostics.AddAttributeError(
			path.Root("org"),
			"Invalid Organization Name",
			"The organization name must contain only valid characters including ASCII letters, numbers, spaces, as well as dashes (-), and underscores (_).",
		)
		return
	}

	// validate regex patter and length constraints for org
	if len(data.Org.ValueString()) < 1 || len(data.Org.ValueString()) > 40 {
		response.Diagnostics.AddAttributeError(
			path.Root("org"),
			"Invalid Organization Length",
			"The organization name must be between 1 and 40 characters long.",
		)
		return
	}

	// Check if the project is set and valid
	if data.Project.IsUnknown() {
		response.Diagnostics.AddAttributeError(
			path.Root("project"),
			"Invalid Project",
			"The project cannot be unknown. Please provide a valid project name. Gets overridden by the `TFE_PROJECT` environment variable if set.",
		)
		return
	}

	// Override project from environment variable if set
	if envProject, ok := os.LookupEnv(TfeProjectEnvName); ok && envProject != "" {
		data.Project = types.StringValue(envProject)
	}

	// Check if the project is null
	if data.Project.IsNull() {
		response.Diagnostics.AddAttributeError(
			path.Root("project"),
			"Invalid Project",
			"The project cannot be null. Please provide a valid project name. Gets overridden by the `TFE_PROJECT` environment variable if set.",
		)
		return
	}

	// Validate the project name against the regex pattern
	if !regexp.MustCompile(projectNameRegex).MatchString(data.Project.ValueString()) {
		response.Diagnostics.AddAttributeError(
			path.Root("project"),
			"Invalid Project Name",
			"The project name must contain only valid characters including ASCII letters, numbers, spaces, as well as dashes (-), and underscores (_).",
		)
		return
	}

	// validate regex patter and length constraints for a project
	if len(data.Project.ValueString()) < 3 || len(data.Project.ValueString()) > 40 {
		response.Diagnostics.AddAttributeError(
			path.Root("project"),
			"Invalid Project Length",
			"The project name must be between 3 and 40 characters long.",
		)
		return
	}

	r.projectName = data.Project.ValueString()
	r.orgName = data.Org.ValueString()

}

// Configure sets up the TFE client using the provider configuration data.
func (r *stackResource) Configure(_ context.Context, request resource.ConfigureRequest, response *resource.ConfigureResponse) {

	if request.ProviderData == nil {
		response.Diagnostics.AddError(
			"Provider not configured",
			"ProviderData must not be nil. This is a bug in the tfe provider, so please report it on GitHub.",
		)
		return
	}

	providerConfigData, ok := request.ProviderData.(ProviderResourceData)
	if !ok {
		response.Diagnostics.AddError(
			"Unexpected resource Configure type",
			fmt.Sprintf("Expected tfe.ConfiguredClient, got %T.This is a bug in the tfe provider, so please report it on GitHub.", request.ProviderData),
		)
		return
	}

	if providerConfigData.TfeToken == "" {

		// If the TFE token is still not set in the provider, try to read the Login token from the provider configuration
		tfeLoginToken, err := providerConfigData.TfeUtil.ReadTfeToken(providerConfigData.Hostname)
		if err != nil {
			response.Diagnostics.AddError(
				"Failed to read TFE token",
				fmt.Sprintf("Could not read TFE token: %s", err.Error()),
			)
			return
		}

		if tfeLoginToken == "" {
			response.Diagnostics.AddError(
				"Missing TFE Token",
				"The TFE token must be provided in the provider configuration block or as an environment variable.",
			)
			return
		}

		providerConfigData.TfeToken = tfeLoginToken
	}

	if providerConfigData.Hostname != HcpTerraformHost {
		response.Diagnostics.AddError(
			"Host name must be set to app.terraform.io",
			fmt.Sprintf("The hostname must be %q, but got %q. Please check your provider configuration.", HcpTerraformHost, providerConfigData.Hostname),
		)
		return
	}

	httpClient := tfe.DefaultConfig().HTTPClient
	transport := httpClient.Transport.(*http.Transport)

	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	transport.TLSClientConfig.InsecureSkipVerify = providerConfigData.SslSkipVerify

	tfeClient, err := providerConfigData.TfeUtil.NewClient(&tfe.Config{
		Address:    providerConfigData.Hostname,
		Token:      providerConfigData.TfeToken,
		HTTPClient: httpClient,
	})

	if err != nil {
		response.Diagnostics.AddError(
			"Failed to create TFE client",
			fmt.Sprintf("Could not create TFE client: %s", err.Error()),
		)
		return
	}

	r.tfeClient = tfeClient
	r.tfeUtil = providerConfigData.TfeUtil
}

// ModifyPlan is called when the provider has an opportunity to modify
// the plan: once during the plan phase when Terraform is determining
// the diff that should be shown to the user for approval, and once
// during the apply phase with any unknown values from configuration
// filled in with their final values.
func (r *stackResource) ModifyPlan(ctx context.Context, request resource.ModifyPlanRequest, response *resource.ModifyPlanResponse) {

	if request.Plan.Raw.IsNull() {
		// Indicates that the plan is empty, which can happen if the resource is being created
		// then do not modify the plan
		return
	}

	var stackMigrationResource StackMigrationResourceModel
	diags := request.Config.Get(ctx, &stackMigrationResource)
	response.Diagnostics.Append(diags...)
	if response.Diagnostics.HasError() {
		return
	}

	// Calculate a new hash based on config (your own logic)
	sourceBundleHash, err := r.tfeUtil.CalculateStackSourceBundleHash(stackMigrationResource.ConfigFileDir.ValueString())
	if err != nil {
		response.Diagnostics.AddError(
			"Unable to Calculate Configuration Hash",
			fmt.Sprintf("Could not calculate the hash of the configuration files in the directory %q, err: %s", stackMigrationResource.ConfigFileDir.ValueString(), err.Error()))
		return
	}

	stackMigrationResource.SourceBundleHash = types.StringValue(sourceBundleHash)

	// Read the current stack configuration ID
	stack, err := r.tfeUtil.ReadStackByName(r.orgName, r.projectName, stackMigrationResource.Name.ValueString(), r.tfeClient)
	if err != nil {
		response.Diagnostics.AddError(
			"Error Reading Stack",
			fmt.Sprintf("Could not read stack %q, err: %s", stackMigrationResource.Name.ValueString(), err.Error()),
		)
		return
	}

	if stack.VCSRepo != nil {
		response.Diagnostics.AddError(
			"VCS Driven Stack",
			"The stack is VCS driven and does not support configuration upload. Please provide a non-VCS driven stack.",
		)
		return
	}

	// If the stack has the latest configuration, set the current configuration ID and the status of the configuration
	if stack.LatestStackConfiguration != nil {
		stackMigrationResource.CurrentConfigurationId = types.StringValue(stack.LatestStackConfiguration.ID)
		stackMigrationResource.ConfigStatus = types.StringValue(stack.LatestStackConfiguration.Status)
	} else {
		stackMigrationResource.CurrentConfigurationId = types.StringNull()
		stackMigrationResource.ConfigStatus = types.StringNull()
	}

	// update the plan with the new values
	diags = response.Plan.Set(ctx, &stackMigrationResource)
	response.Diagnostics.Append(diags...)
}

// Create creates the resource and sets the initial Terraform state.
func (r *stackResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// Retrieve values from the plan
	var stackMigrationResource StackMigrationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &stackMigrationResource)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// check if the organization exists
	if _, err := r.tfeUtil.RedTfeOrgByName(r.orgName, r.tfeClient); err != nil {
		resp.Diagnostics.AddError(
			"Error Reading organization",
			fmt.Sprintf("The organization %q does not exist or could not be accessed: %s", r.orgName, err.Error()),
		)
		return
	}

	// check if the project exists
	project, err := r.tfeUtil.ReadProjectByName(r.orgName, r.projectName, r.tfeClient)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading project",
			fmt.Sprintf("The project %q does not exist or could not be accessed: %s", r.projectName, err.Error()),
		)
		return
	}

	// check if the stack exists
	stack, err := r.tfeUtil.ReadStackByName(r.orgName, project.ID, stackMigrationResource.Name.ValueString(), r.tfeClient)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading stack",
			fmt.Sprintf("The stack %q does not exist or could not be accessed: %s", stackMigrationResource.Name.ValueString(), err.Error()),
		)
		return
	}

	// if the stack is VCS driven, do not allow configuration upload
	if stack.VCSRepo != nil {
		resp.Diagnostics.AddError(
			"VCS Driven Stack",
			"The stack is VCS driven and does not support configuration upload. Please use the VCS provider to manage the stack.",
		)
		return
	}

	// calculate the hash of the configuration files in the directory
	sourceBundleHash, err := r.tfeUtil.CalculateStackSourceBundleHash(stackMigrationResource.ConfigFileDir.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Calculating Configuration Hash",
			fmt.Sprintf("Could not calculate the hash of the configuration files in the directory %q: %s", stackMigrationResource.ConfigFileDir.ValueString(), err.Error()),
		)
		return
	}

	// check if the stack configuration can be uploaded
	allowUpload, err := r.allowCreateActionStacksConfigUpload(stack)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Checking Stack Configuration Upload",
			fmt.Sprintf("Could not check if stack configuration can be uploaded: %s", err.Error()),
		)
		return
	}

	var stackConfigurationId string
	if allowUpload {
		cfgId, diags := r.uploadConfig(stack.ID, stackMigrationResource.ConfigFileDir.ValueString())
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		stackConfigurationId = cfgId

	} else {
		stackConfigurationId = stack.LatestStackConfiguration.ID
		tflog.Info(ctx, fmt.Sprintf("The current configuration ID %s is in state %s, begging to poll ...", stackMigrationResource.CurrentConfigurationId.ValueString(), stack.LatestStackConfiguration.Status))
	}

	// NOTE: if no error is returned from allowStacksConfigUpload and allowUpload is false,
	//  that means the stack configuration is in a transitioning state, so we need
	//  to wait for the stack configuration to reach a terminal state before
	//  continuing to reset of the resource creation logic.

	status, diags := r.awaitConfigCompletion(stackConfigurationId)
	if diags.HasError() {
		resp.Diagnostics.Append(diags...)
	}

	// Set state
	createResource := StackMigrationResourceModel{
		SourceBundleHash:       types.StringValue(sourceBundleHash),
		CurrentConfigurationId: types.StringValue(stackConfigurationId),
		ConfigStatus:           types.StringValue(status.String()),
		Name:                   types.StringValue(stack.Name),
		Org:                    types.StringValue(r.orgName),
		Project:                types.StringValue(r.projectName),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &createResource)...)
}

// Read refreshes the Terraform state with the latest data.
func (r *stackResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Retrieve values from a state
	var stackMigrationResourceData StackMigrationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &stackMigrationResourceData)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// calculate the hash of the configuration files in the directory
	// during raed it is assumed that the hash of the configuration files
	// provided in the config_file_dir is the same as the one that is
	// responsible for the current stack configuration state. Hence, we
	// calculate the hash of the configuration files in the directory
	// and set it to the config_hash attribute in the state.
	sourceBundleHash, err := r.tfeUtil.CalculateStackSourceBundleHash(stackMigrationResourceData.ConfigFileDir.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Calculating Configuration Hash",
			fmt.Sprintf("Could not calculate the hash of the configuration files in the directory %q: %s", stackMigrationResourceData.ConfigFileDir.ValueString(), err.Error()),
		)
		return
	}

	// read the stack by name
	stack, err := r.tfeUtil.ReadStackByName(r.orgName, r.projectName, stackMigrationResourceData.Name.ValueString(), r.tfeClient)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Stack",
			fmt.Sprintf("Could not read stack %q: %s", stackMigrationResourceData.Name.ValueString(), err.Error()),
		)
		return
	}

	stackReadData := StackMigrationResourceModel{
		SourceBundleHash: types.StringValue(sourceBundleHash),
		Name:             types.StringValue(stack.Name),
		Org:              types.StringValue(r.orgName),
		Project:          types.StringValue(r.projectName),
	}

	if stack.LatestStackConfiguration != nil {
		stackReadData.CurrentConfigurationId = types.StringValue(stack.LatestStackConfiguration.ID)
		stackReadData.ConfigStatus = types.StringValue(stack.LatestStackConfiguration.Status)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &stackReadData)...)
}

// Update updates the resource and sets the updated Terraform state.
func (r *stackResource) Update(ctx context.Context, updateRequest resource.UpdateRequest, updateResponse *resource.UpdateResponse) {
	var currentState StackMigrationResourceModel
	var plannedState StackMigrationResourceModel

	updateResponse.Diagnostics.Append(updateRequest.State.Get(ctx, &currentState)...)
	if updateResponse.Diagnostics.HasError() {
		return
	}

	updateResponse.Diagnostics.Append(updateRequest.Plan.Get(ctx, &plannedState)...)
	if updateResponse.Diagnostics.HasError() {
		return
	}

	skipConfigIdUpdate := false
	skipConfigStatusUpdate := false

	// read the current stack by name
	stack, err := r.tfeUtil.ReadStackByName(r.orgName, r.projectName, plannedState.Name.ValueString(), r.tfeClient)
	if err != nil || stack == nil || stack.LatestStackConfiguration == nil || stack.LatestStackConfiguration.ID == "" {
		updateResponse.Diagnostics.AddError(
			"Error Reading Stack",
			fmt.Sprintf("Could not read stack %q, err: %s", plannedState.Name.ValueString(), err.Error()),
		)
		return
	}

	/*
	   Take action:
	   1. If the SourceBundleHash has changed, we need to upload the new configuration.
	   2. If the org, project, or name has changed, we need to update the stack configuration.
	*/
	if !currentState.SourceBundleHash.Equal(plannedState.SourceBundleHash) ||
		!currentState.Org.Equal(plannedState.Org) ||
		!currentState.Project.Equal(plannedState.Project) ||
		!currentState.Name.Equal(plannedState.Name) {
		configId, configStatus, diags := r.updateIfSourceBundleHashUpdated(stack, plannedState.ConfigFileDir.ValueString())
		if diags.HasError() {
			updateResponse.Diagnostics.Append(diags...)
		}
		skipConfigIdUpdate = true
		skipConfigStatusUpdate = true
		// update the planned state with the new configuration ID and status
		plannedState.CurrentConfigurationId = types.StringValue(configId)
		plannedState.ConfigStatus = types.StringValue(configStatus.String())
	}

	/*
	   Take action:
	   1. If the current configuration ID is different from the planned configuration ID, we need to update the configuration ID.
	*/
	if !currentState.CurrentConfigurationId.Equal(plannedState.CurrentConfigurationId) && !skipConfigIdUpdate {
		status, diags := r.updateIfConfigIdUpdated(plannedState.CurrentConfigurationId.ValueString())
		if diags.HasError() {
			updateResponse.Diagnostics.Append(diags...)
			return
		}
		// update the planned state with the new configuration status
		plannedState.ConfigStatus = types.StringValue(status.String())
	}

	/*
	   Take action:
	   1. If the config status is different from the planned config status, we need to update the config status.
	*/
	if !currentState.ConfigStatus.Equal(plannedState.ConfigStatus) && !skipConfigStatusUpdate {
		status, diags := r.updateIfConfigStatusIsUpdated(tfe.StackConfigurationStatus(plannedState.ConfigStatus.ValueString()), plannedState.CurrentConfigurationId.ValueString())
		if diags != nil && diags.HasError() {
			updateResponse.Diagnostics.Append(diags...)
			return
		}
		// update the planned state with the new configuration status
		plannedState.ConfigStatus = types.StringValue(status.String())
	}

	// Update state with the new values
	updateResponse.Diagnostics.Append(updateResponse.State.Set(ctx, &plannedState)...)

}

// Delete deletes the resource and removes the Terraform state.
func (r *stackResource) Delete(ctx context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	tflog.Warn(ctx, DestroyActionNotSupported)
	resp.Diagnostics.AddWarning(DestroyActionNotSupported, DestroyActionNotSupportedDetailed)
}

// Metadata returns the resource type name.
func (r *stackResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + stackMigrationResourceName
}

func (r *stackResource) allowCreateActionStacksConfigUpload(stack *tfe.Stack) (bool, error) {

	if stack == nil {
		return false, fmt.Errorf("stack cannot be nil")
	}

	// check if the stack has the latest stack configuration
	latestStackConfiguration := stack.LatestStackConfiguration
	if latestStackConfiguration == nil {
		return true, nil
	}

	// if the latest stack configuration status is in the allowed statuses and there are no deployments, allow configuration upload
	if slices.Contains(stacksConfigTerminatingStatuses, tfe.StackConfigurationStatus(latestStackConfiguration.Status)) {
		if len(latestStackConfiguration.DeploymentNames) == 0 {
			return true, nil
		}

		if len(latestStackConfiguration.DeploymentNames) > 0 {
			return false, fmt.Errorf("stack configuration upload is not allowed when there are deployments in the stack. Please remove the deployments before uploading the configuration")
		}
	}

	// for status other than "canceled", "errored", "converged" we need to return false
	// because we cannot upload configuration when the stack is in a transitioning state
	// for the transitioning states, "pending", "queued", "preparing", "enqueueing", "converging"
	// we need to wait for the stack configuration to reach a terminal state
	return false, nil

}

func (r *stackResource) uploadConfig(stackId string, configFileAbsPath string) (string, diag.Diagnostics) {
	// Upload the configuration files to the stack
	var diagnostics diag.Diagnostics
	configId, err := r.tfeUtil.UploadStackConfigFile(stackId, configFileAbsPath, r.tfeClient)
	if err != nil {
		diagnostics.AddError(
			"Error Uploading Stack Configuration",
			fmt.Sprintf("Could not upload stack configuration: %s", err.Error()),
		)
		return "", diagnostics
	}
	return configId, diagnostics
}

func (r *stackResource) awaitConfigCompletion(configId string) (tfe.StackConfigurationStatus, diag.Diagnostics) {
	var diagnostics diag.Diagnostics
	status, err := r.tfeUtil.AwaitStackConfigurationCompletion(configId, r.tfeClient)
	if err != nil {
		diagnostics.AddError(
			"Error Awaiting Stack Configuration Completion",
			fmt.Sprintf("Could not complete polling for stack configuration completion for configuration ID %s: %s", configId, err.Error()),
		)
		return "", diagnostics
	}
	return status, diagnostics
}

func (r *stackResource) updateIfSourceBundleHashUpdated(currentStackData *tfe.Stack, sourceBundleAbsPath string) (string, tfe.StackConfigurationStatus, diag.Diagnostics) {
	var diagnostics diag.Diagnostics
	var shouldUploadConfig bool
	configId := currentStackData.LatestStackConfiguration.ID

	// handle the case when the stack configuration is in a terminal state
	if slices.Contains(stacksConfigTerminatingStatuses, tfe.StackConfigurationStatus(currentStackData.LatestStackConfiguration.Status)) {
		if len(currentStackData.LatestStackConfiguration.DeploymentNames) > 0 {
			diagnostics.AddError(
				"Stack Configuration Upload Not Allowed",
				"Stack configuration upload is not allowed when there are deployments in the stack. Please remove the deployments before uploading the configuration.",
			)
			return "", "", diagnostics
		}
		shouldUploadConfig = true

	}

	if shouldUploadConfig {
		// Upload the configuration files to the stack
		configId, diagnostics = r.uploadConfig(currentStackData.ID, sourceBundleAbsPath)
		if diagnostics.HasError() {
			diagnostics.Append(diagnostics...)
			return "", "", diagnostics
		}
	}

	// Await the completion of the stack configuration upload
	status, diags := r.awaitConfigCompletion(configId)
	if diags.HasError() {
		diagnostics.Append(diags...)
		return "", "", diagnostics
	}

	return configId, status, diagnostics
}

func (r *stackResource) updateIfConfigIdUpdated(configId string) (tfe.StackConfigurationStatus, diag.Diagnostics) {
	status, diags := r.awaitConfigCompletion(configId)
	if diags.HasError() {
		return "", diags
	}
	return status, diags
}

func (r *stackResource) updateIfConfigStatusIsUpdated(status tfe.StackConfigurationStatus, configId string) (tfe.StackConfigurationStatus, diag.Diagnostics) {

	if slices.Contains(stacksConfigTerminatingStatuses, status) {
		return status, nil
	}

	status, diags := r.awaitConfigCompletion(configId)
	return status, diags
}
