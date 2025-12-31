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
	"terraform-provider-tfmigrate/internal/constants"
	stackConstants "terraform-provider-tfmigrate/internal/constants/stack"
	httpUtil "terraform-provider-tfmigrate/internal/util/net"
	tfeUtil "terraform-provider-tfmigrate/internal/util/tfe"

	"golang.org/x/exp/maps"

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	tfstateUtil "github.com/hashicorp/terraform-migrate-utility/tfstateutil"
	"github.com/hashicorp/terraform-plugin-framework/diag"

	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"

	"github.com/hashicorp/go-tfe"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Define constants for the stack resource.
const (
	stackMigrationResourceName = `_stack_migration` // stackMigrationResourceName is the name of the resource for migrating existing HCP Terraform workspaces to deployments within a non-VCS stack.

	// stackMigrationResourceDescription is the description of the resource for migrating existing HCP Terraform workspaces to deployments within a non-VCS stack.
	stackMigrationResourceDescription = `Defines a resource for migrating existing HCP Terraform workspaces to deployments within a non-VCS stack. Each workspace maps one-to-one with a stack deployment. The resource uploads configuration files to the stack and monitors the upload and deployment status.`

	// Attribute names.
	attrConfigurationDir           = `config_file_dir`              // attrConfigurationDir is the attribute for the directory containing the stack configuration files.
	attrCurrentConfigurationId     = `current_configuration_id`     // attrCurrentConfigurationId is the attribute for the ID of the current stack configuration.
	attrCurrentConfigurationStatus = `current_configuration_status` // attrCurrentConfigurationStatus is the attribute for the status of the stack configuration.
	attrMigrationHash              = `migration_hash`               // attrMigrationHash is the attribute that allows the resource to track the migration state of the stack.
	attrName                       = `name`                         // attrName is the attribute for the name of the stack.
	attrOrganization               = `organization`                 // attrOrganization is the attribute for the HCP Terraform organization name in which the stack exists.
	attrProject                    = `project`                      // attrProject is the attribute for the HCP Terraform project name in which the stack exists.
	attrSourceBundleHash           = `source_bundle_hash`           // attrSourceBundleHash is the attribute for the hash of the configuration files in the directory.
	attrTerraformConfigDir         = `terraform_config_dir`         // attrTerraformConfigDir is the attribute for the directory containing the Terraform configuration files from which stack configurations are generated.
	attrTerraformConfigHash        = `terraform_config_hash`        // attrTerraformConfigHash is the attribute for the hash of the Terraform configuration files in the directory.
	attrWorkspaceDeploymentMapping = `workspace_deployment_mapping` // attrWorkspaceDeploymentMapping is the attribute for the map of workspace names to stack deployment names.

	/* Attribute markdown descriptions. */

	// configurationDirDescription is the Markdown description for the `config_file_dir` attribute.
	configurationDirDescription = `The directory path containing configuration files. Must be an absolute path.`

	// currentConfigurationIdDescription is the Markdown description for the `current_configuration_id` attribute.
	currentConfigurationIdDescription = `The ID of the current stack configuration. This is used to track the current configuration of the stack.`

	// currentConfigurationStatusDescription is the Markdown description for the `current_configuration_status` attribute.
	currentConfigurationStatusDescription = `The status of the stack configuration. This is used to track the status of the stack configuration upload.`

	// migrationHashDescription is the Markdown description for the `migration_hash` attribute.
	migrationHashDescription = `The hash used for tracking the migration state of the stack.`

	// nameDescription is the Markdown description for the `name` attribute.
	nameDescription = `The stack name. Must be unique within the organization and project, must be a non-VCS driven stack.`

	// organizationDescription is the Markdown description for the `organization` attribute.
	organizationDescription = "The organization name to which the stack belongs. This must reference an existing organization. Either this attribute or the `TFE_ORGANIZATION` environment variable is required; if both are set, the attribute value takes precedence."

	// projectDescription is the Markdown description for the `project` attribute.
	projectDescription = "The project name to which the stack belongs. This must reference an existing project. Either this attribute or the `TFE_PROJECT` environment variable is required; if both are set, the attribute value takes precedence."

	// sourceBundleHashDescription is the Markdown description for the `source_bundle_hash` attribute.
	sourceBundleHashDescription = `The hash of the configuration files in the directory. This is used to detect changes in the stack configuration files.`

	// terraformConfigDirDescription is the Markdown description for the `terraform_config_dir` attribute.
	terraformConfigDirDescription = `The directory path containing the Terraform configuration files from which stack configurations are generated. Must be an absolute path.`

	// terraformConfigHashDescription is the Markdown description for the `terraform_config_hash` attribute.
	terraformConfigHashDescription = `The hash of the Terraform configuration files in the directory. This is used to detect changes in the Terraform configuration files.`

	// workspaceDeploymentMappingDescription is the Markdown description for the `workspace_deployment_mapping` attribute.
	workspaceDeploymentMappingDescription = `A map of workspace names to stack deployment names. This is used to map the workspaces to the stack deployments. The keys are the workspace names, and the values are the stack deployment names.`

	/* validator constants. */

	// deploymentNameConstraintViolationMsg is the error message for deployment name constraint violations.
	deploymentNameConstraintViolationMsg = `The deployment name must be between 1 and 90 characters long and may contain valid characters including ASCII letters, numbers, spaces, as well as dashes (-), and underscores (_).`

	// organizationNameConstraintViolationMsg is the error message for organization name constraint violations.
	organizationNameConstraintViolationMsg = `The organization name must be between 3 and 40 characters long and may contain valid characters including ASCII letters, numbers, spaces, as well as dashes (-), and underscores (_).`

	// projectNameConstraintViolationMsg is the error message for project name constraint violations.
	projectNameConstraintViolationMsg = `The project name must be between 3 and 40 characters long and may contain valid characters including ASCII letters, numbers, spaces, as well as dashes (-), and underscores (_).`

	// stackNameConstraintViolationMsg is the error message for stack name constraint violations.
	stackNameConstraintViolationMsg = `The stack name must be between 1 and 90 characters long and may contain valid characters including ASCII letters, numbers, spaces, as well as dashes (-), and underscores (_).`

	// workspaceNameConstraintViolationMsg is the error message for workspace name constraint violations.
	workspaceNameConstraintViolationMsg = `The workspace name must be between 1 and 260 characters long and may contain valid characters including ASCII letters, numbers, spaces, as well as dashes (-), and underscores (_).`

	/* Configuration hash calculation error constants. */

	// configHasErrSummary is the summary for the error when the configuration hash cannot be calculated.
	configHasErrSummary = `Unable to Calculate Configuration Hash`

	// configHashErrDetails is the detailed error message when the configuration hash cannot be calculated.
	configHashErrDetails = `Could not calculate the hash of the configuration files in the directory %q, err: %s`

	// errDiagDetailsSourceBundleUploadChk is the diagnostic error details when checking if the source bundle upload is allowed.
	errDiagDetailsSourceBundleUploadChk = "Failed to check if source bundle upload is allowed for stack %s: %s"

	// errFailedToGetStateValues is the error message when the state values cannot be retrieved.
	errFailedToGetStateValues = "Failed to get state values"

	// errFailedToGetPlanValues is the error message when the plan values cannot be retrieved.
	errFailedToGetPlanValues = "Failed to get plan values"

	stackDeploymentHclFileExt = `.tfdeploy.hcl`
)

var (
	_ resource.Resource              = &stackMigrationResource{}
	_ resource.ResourceWithConfigure = &stackMigrationResource{}
	// _ resource.ResourceWithModifyPlan = &stackMigrationResource{}.

	projectAndOrgNameRegex = regexp.MustCompile(`^[a-zA-Z0-9 _-]{3,40}$`)  // projectAndOrgNameRegex Regex for valid organization, and project names
	stackNameRegex         = regexp.MustCompile(`^[a-zA-Z0-9 _-]{1,90}$`)  // projectAndOrgNameRegex Regex for valid stack names
	workspaceNameRegex     = regexp.MustCompile(`^[a-zA-Z0-9 _-]{1,260}$`) // workspaceNameRegex Regex for valid workspace names

)

// StackMigrationResourceModel is the data model for the stack migration resource used by the Terraform provider
// to create, read, and update tfmigrate_stack_migration resource's state.
type StackMigrationResourceModel struct {
	ConfigurationDir           types.String `tfsdk:"config_file_dir"`              // ConfigurationDir is the absolute directory path containing stack configuration files.
	CurrentConfigurationId     types.String `tfsdk:"current_configuration_id"`     // CurrentConfigurationId is the ID of the current stack configuration.
	CurrentConfigurationStatus types.String `tfsdk:"current_configuration_status"` // CurrentConfigurationStatus  is the status of the stack configuration.
	MigrationHash              types.String `tfsdk:"migration_hash"`               // MigrationHash is the hash is used for tracking the state of workspace to stack migration.
	Name                       types.String `tfsdk:"name"`                         // Name is the name of the stack Must be unique within the organization and project, must be a non-Vcs driven stack.
	Organization               types.String `tfsdk:"organization"`                 // Organization is the HCP Terraform organization name in which the stack exists. The value can be provided by the `TFE_ORGANIZATION` environment variable.
	Project                    types.String `tfsdk:"project"`                      // Project is the HCP Terraform project name in which the stack exists. The value can be provided by the `TFE_PROJECT` environment variable.
	SourceBundleHash           types.String `tfsdk:"source_bundle_hash"`           // SourceBundleHash is the hash of the configuration files in the directory. This is used to detect changes in the configuration files.
	TerraformConfigDir         types.String `tfsdk:"terraform_config_dir"`         // TerraformConfigDir is the absolute directory path containing the Terraform configuration files from which stack configurations are generated.
	TerraformConfigHash        types.String `tfsdk:"terraform_config_hash"`        // TerraformConfigHash is the hash of the Terraform configuration files in the directory. This is used to detect changes in the Terraform configuration files.
	WorkspaceDeploymentMapping types.Map    `tfsdk:"workspace_deployment_mapping"` // WorkspaceDeploymentMapping is a map of workspace names to stack deployment names. This is used to map the workspaces to the stack deployments.
}

// stackMigrationResource implements the resource.Resource interface for managing stack migrations in HCP Terraform.
type stackMigrationResource struct {
	deploymentStateImportMap  map[string]bool                     // deploymentStateImportMap is a map that tracks if a deployment has an import attribute set to true that indicates the deployment's state could be imported from the TFE API.
	existingOrganization      *tfe.Organization                   // an existingOrganization is the organization in which the stack exists.
	existingProject           *tfe.Project                        // an existingProject is the project in which the stack exists.
	existingStack             *tfe.Stack                          // an existingStack is the stack to which the workspace will be migrated.
	hclParser                 *hclparse.Parser                    // hclParser is the HCL parser used to parse HCL files.
	httpClient                httpUtil.Client                     // httpClient is the HTTP client used to make requests to the TFE API configured with TLS settings and retry logic.
	isStateModular            bool                                // isStateModular indicates whether the state is modular or not. If true, the state is modular, and the stack migration resource will use the modular update strategy.
	migrationHashService      StackMigrationHashService           // migrationHashService is the service used to generate and manage migration hash for stack migrations.
	retryAbandonedDeployments bool                                // retryAbandonedDeployments indicates whether to retry deployments by generating a new deployment run set to true during update action only
	stackSourceBundleAbsPath  string                              // stackSourceBundleAbsPath is the absolute path to the stack source bundle directory containing the stack configuration files.
	terraformConfigDirAbsPath string                              // terraformConfigDirAbsPath is the absolute path to the Terraform configuration directory containing the Terraform configuration files.
	tfeClient                 *tfe.Client                         // tfeClient is the TFE client used to interact with the HCP Terraform API.
	tfeConfig                 *tfe.Config                         // tfeConfig is the TFE client configuration used to create the TFE client.
	tfeUtil                   tfeUtil.TfeUtil                     // tfeUtil is the utility for interacting with the TFE API, used to perform operations like uploading stack configurations and calculating source bundle hashes.
	tfstateUtil               tfstateUtil.TfWorkspaceStateUtility // tfstateUtil is the utility for interacting with the Terraform state, used to perform operations like reading and writing state files.
	workspaceToStackMap       map[string]string                   // workspaceToStackMap is a map of workspace names to stack deployment names, used to map the workspaces to the stack deployments.
}

// NewStackMigrationResource creates a new instance of the stack migration resource.
func NewStackMigrationResource() resource.Resource {
	return &stackMigrationResource{}
}

// Schema defines the schema for the stack migration resource.
func (r *stackMigrationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Version:             1,
		Description:         stackMigrationResourceDescription,
		MarkdownDescription: stackMigrationResourceDescription,
		Attributes: map[string]schema.Attribute{
			attrConfigurationDir: schema.StringAttribute{
				MarkdownDescription: configurationDirDescription,
				Required:            true,
				Validators: []validator.String{
					&absolutePathValidator{},
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			attrCurrentConfigurationId: schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: currentConfigurationIdDescription,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			attrCurrentConfigurationStatus: schema.StringAttribute{
				MarkdownDescription: currentConfigurationStatusDescription,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			attrMigrationHash: schema.StringAttribute{
				MarkdownDescription: migrationHashDescription,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			attrName: schema.StringAttribute{
				MarkdownDescription: nameDescription,
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 90),
					stringvalidator.RegexMatches(stackNameRegex, stackNameConstraintViolationMsg),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			attrOrganization: schema.StringAttribute{
				MarkdownDescription: organizationDescription,
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.LengthBetween(3, 40),
					stringvalidator.RegexMatches(projectAndOrgNameRegex, organizationNameConstraintViolationMsg),
					&orgEnvNameValidator{},
				},
				Default: stringdefault.StaticString(os.Getenv(TfeOrganizationEnvName)),
			},
			attrProject: schema.StringAttribute{
				MarkdownDescription: projectDescription,
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.LengthBetween(3, 40),
					stringvalidator.RegexMatches(projectAndOrgNameRegex, projectNameConstraintViolationMsg),
					&projectEnvNameValidator{},
				},
				Default: stringdefault.StaticString(os.Getenv(TfeProjectEnvName)),
			},
			attrSourceBundleHash: schema.StringAttribute{
				MarkdownDescription: sourceBundleHashDescription,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			attrTerraformConfigDir: schema.StringAttribute{
				MarkdownDescription: terraformConfigDirDescription,
				Required:            true,
				Validators: []validator.String{
					&absolutePathValidator{},
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			attrTerraformConfigHash: schema.StringAttribute{
				MarkdownDescription: terraformConfigHashDescription,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			attrWorkspaceDeploymentMapping: schema.MapAttribute{
				MarkdownDescription: workspaceDeploymentMappingDescription,
				Required:            true,
				ElementType:         types.StringType,
				Validators: []validator.Map{
					mapvalidator.NoNullValues(),
					mapvalidator.KeysAre(
						stringvalidator.RegexMatches(workspaceNameRegex, workspaceNameConstraintViolationMsg),
					),
					mapvalidator.ValueStringsAre(
						stringvalidator.RegexMatches(stackNameRegex, deploymentNameConstraintViolationMsg),
					),
				},
			},
		},
	}
}

// Create is called when the resource is created. It uploads the stack configuration files to the stack and waits for the stack configuration to converge, cancel, or error out.
func (r *stackMigrationResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
	var plan StackMigrationResourceModel
	tflog.Debug(ctx, "Starting Create operation for stack migration resource")

	// retrieve values from the plan
	response.Diagnostics.Append(request.Plan.Get(ctx, &plan)...)
	if response.Diagnostics.HasError() {
		return
	}
	tflog.Debug(ctx, fmt.Sprintf("Received plan for stack migration resource: %+v", plan))

	// update the context for the TFE client, HTTP client, and migration hash service.
	r.tfeUtil.UpdateContext(ctx)
	r.httpClient.UpdateContext(ctx)
	r.migrationHashService.UpdateContext(ctx)

	// set the absolute path for the stack source bundle and Terraform configuration directories.
	r.stackSourceBundleAbsPath = plan.ConfigurationDir.ValueString()
	r.terraformConfigDirAbsPath = plan.TerraformConfigDir.ValueString()

	migrationMapAttrVal := plan.WorkspaceDeploymentMapping.Elements()
	migrationMap := make(map[string]string, len(migrationMapAttrVal))
	// convert the map attribute value to a map[string]string
	for key, value := range migrationMapAttrVal {
		migrationMap[key] = value.(types.String).ValueString()
	}

	tflog.Debug(ctx, fmt.Sprintf("Retrieving workspace state to stack state conversion metadata for organization %q and workspace %q and migration map %v",
		plan.Organization.ValueString(), maps.Keys(migrationMap)[0], migrationMap))
	response.Diagnostics.Append(r.getWorkspaceToStackStateConversionMetaData(ctx, maps.Keys(migrationMap)[0], plan.Organization.ValueString())...)
	if response.Diagnostics.HasError() {
		tflog.Error(ctx, fmt.Sprintf("Failed to retrieve workspace state to stack state conversion metadata for organization %q and workspace %q and migration map %v",
			plan.Organization.ValueString(), maps.Keys(migrationMap)[0], migrationMap))
		return
	}
	tflog.Debug(ctx, fmt.Sprintf("Successfully retrieved workspace state to stack state conversion metadata for organization %q and workspace %q and migration map %v",
		plan.Organization.ValueString(), maps.Keys(migrationMap)[0], migrationMap))
	tflog.Debug(ctx, fmt.Sprintf("Is workspace state fully modular: %v", r.isStateModular))
	tflog.Debug(ctx, fmt.Sprintf("Workspace to Stacks State resource mapping: %v", r.workspaceToStackMap))

	// retrieve the required values from the plan
	// organizationName, projectName, stackName, stackConfigDirectory, and migrationMap.
	organizationName := plan.Organization.ValueString()
	projectName := plan.Project.ValueString()
	stackName := plan.Name.ValueString()
	stackConfigDirectory := plan.ConfigurationDir.ValueString()

	// start the stack migration process
	tflog.Info(ctx, "Starting to apply stack migration configuration to a new stack migration resource")
	saveState, state, diags := r.applyStackConfiguration(ctx, organizationName, projectName, stackName, stackConfigDirectory, migrationMap, true, true)
	response.Diagnostics.Append(diags...)
	if response.Diagnostics.HasError() || response.Diagnostics.WarningsCount() > 0 && !saveState {
		return
	}

	response.Diagnostics.Append(response.State.Set(ctx, &state)...)
	tflog.Info(ctx, "Successfully saved stack migration configuration to a new stack migration resource")
}

// Read is called when the resource is read, it retrieves the current state of the stack migration resource and updates the state with the latest values.
func (r *stackMigrationResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
	var state StackMigrationResourceModel
	var err error
	r.tfeUtil.UpdateContext(ctx)
	r.migrationHashService.UpdateContext(ctx)

	// read the current state
	response.Diagnostics.Append(request.State.Get(ctx, &state)...)
	if response.Diagnostics.HasError() {
		tflog.Error(ctx, errFailedToGetStateValues)
		return
	}

	tflog.Info(ctx, "Successfully read the current state for stack migration resource")

	// read the organization.
	if r.existingOrganization, err = r.tfeUtil.ReadOrgByName(state.Organization.ValueString(), r.tfeClient); err != nil {
		response.Diagnostics.AddError(
			"Error Reading organization",
			fmt.Sprintf("The organization %q does not exist or could not be accessed: %s", state.Organization.ValueString(), err.Error()),
		)
		return
	}

	// read the project.
	r.existingProject, err = r.tfeUtil.ReadProjectByName(r.existingOrganization.Name, state.Project.ValueString(), r.tfeClient)
	if err != nil {
		response.Diagnostics.AddError(
			"Error Reading project",
			fmt.Sprintf("The project %q does not exist or could not be accessed in organization %q: %s", state.Project.ValueString(), r.existingOrganization.Name, err.Error()),
		)
		return
	}

	// check read stack already.
	r.existingStack, err = r.tfeUtil.ReadStackByName(r.existingOrganization.Name, r.existingProject.ID, state.Name.ValueString(), r.tfeClient)
	if err != nil {
		response.Diagnostics.AddError(
			"Error Reading stack",
			fmt.Sprintf("The stack %q does not exist or could not be accessed in organization %q and project %q: %s", state.Name.ValueString(), r.existingOrganization.Name, r.existingProject.Name, err.Error()),
		)
		return
	}

	// if the stack is a VCS-driven stack, throw an error.
	if r.existingStack.VCSRepo != nil {
		response.Diagnostics.AddError(
			"Migration to VCS backed stacks is not supported",
			fmt.Sprintf("The stack %q in organization %q and project %q is a VCS backed stacks. The `tfmigrate_stack_migration` resource supports migration to non-VCS backed stacks only ", r.existingStack.Name, r.existingOrganization.Name, r.existingProject.Name),
		)
		return
	}

	// NOTE:
	//   calculate the hash of the configuration files in the directory
	//   during raed it is assumed that the hash of the configuration files
	//   provided in the config_file_dir is the same as the one that is
	//   responsible for the current stack configuration state. Hence, we
	//   calculate the hash of the configuration files in the directory
	//   and set it to the source_bundle_hash attribute in the state.
	//
	sourceBundleHash, err := r.tfeUtil.CalculateConfigFileHash(state.ConfigurationDir.ValueString())
	if err != nil {
		response.Diagnostics.AddError(
			"Error Calculating Configuration Hash",
			fmt.Sprintf("Could not calculate the hash of the configuration files in the directory %q: %s", state.ConfigurationDir.ValueString(), err.Error()),
		)
		return
	}

	// calculate the hash of the Terraform configuration files in the directory
	terraformConfigHash, err := r.tfeUtil.CalculateConfigFileHash(state.TerraformConfigDir.ValueString())
	if err != nil {
		response.Diagnostics.AddError(
			"Error Calculating Terraform Configuration Hash",
			fmt.Sprintf("Could not calculate the hash of the Terraform configuration files in the directory %q: %s", state.TerraformConfigDir.ValueString(), err.Error()),
		)
		return
	}

	// retrieve the current migration data from the existing state
	stackMigrationData, diags := r.getCurrentMigrationDataFromExistingState(ctx, state.WorkspaceDeploymentMapping)
	if diags.HasError() {
		response.Diagnostics.Append(diags...)
		return
	}

	// generate the migration hash using the migration data
	migrationHash, err := r.migrationHashService.GetMigrationHash(stackMigrationData)
	if err != nil {
		response.Diagnostics.AddError(
			"Error Generating Migration Hash",
			fmt.Sprintf("Could not generate migration hash for stack %q in organization %q: %s", r.existingStack.Name, r.existingOrganization.Name, err.Error()),
		)
		return
	}

	// update the values in the updatedState
	updatedState := StackMigrationResourceModel{}
	updatedState.ConfigurationDir = state.ConfigurationDir
	updatedState.CurrentConfigurationId = types.StringValue(r.existingStack.LatestStackConfiguration.ID)
	updatedState.CurrentConfigurationStatus = types.StringValue(string(r.existingStack.LatestStackConfiguration.Status))
	updatedState.MigrationHash = types.StringValue(migrationHash)
	updatedState.Name = types.StringValue(r.existingStack.Name)
	updatedState.Organization = types.StringValue(r.existingOrganization.Name)
	updatedState.Project = types.StringValue(r.existingProject.Name)
	updatedState.SourceBundleHash = types.StringValue(sourceBundleHash)
	updatedState.TerraformConfigDir = state.TerraformConfigDir
	updatedState.TerraformConfigHash = types.StringValue(terraformConfigHash)
	updatedState.WorkspaceDeploymentMapping = state.WorkspaceDeploymentMapping

	data, _ := r.migrationHashService.GetMigrationData(migrationHash)
	diags.AddWarning(
		"Migration Data Retrieved",
		prettyPrintJSON(data),
	)
	response.Diagnostics.Append(diags...)

	// save the updated state
	response.Diagnostics.Append(response.State.Set(ctx, &updatedState)...)

	tflog.Info(ctx, "Successfully saved the state for stack migration resource")
}

// Update is called when the resource is updated, it applies the stack configuration files to an existing stack migration resource and updates the state with the new values.
func (r *stackMigrationResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
	var plan, state, newState StackMigrationResourceModel
	r.tfeUtil.UpdateContext(ctx)
	r.httpClient.UpdateContext(ctx)
	r.migrationHashService.UpdateContext(ctx)

	// Retrieve values from the plan
	response.Diagnostics.Append(request.Plan.Get(ctx, &plan)...)
	if response.Diagnostics.HasError() {
		tflog.Error(ctx, "Failed to get plan values")
		return
	}

	// Retrieve values from the state
	response.Diagnostics.Append(request.State.Get(ctx, &state)...)
	if response.Diagnostics.HasError() {
		tflog.Error(ctx, errFailedToGetStateValues)
		return
	}

	// Retrieve the migration map from the plan
	migrationMapFromPlan := make(map[string]string)
	for key, value := range plan.WorkspaceDeploymentMapping.Elements() {
		migrationMapFromPlan[key] = value.(types.String).ValueString()
	}

	r.stackSourceBundleAbsPath = plan.ConfigurationDir.ValueString()
	r.terraformConfigDirAbsPath = plan.TerraformConfigDir.ValueString()

	// Fetch workspace to stack state conversion metadata
	tflog.Debug(ctx, fmt.Sprintf("[Update-Action] Retrieving workspace state to stack state conversion metadata for organization %q and workspace %q and migration map %v",
		plan.Organization.ValueString(), maps.Keys(migrationMapFromPlan)[0], migrationMapFromPlan))
	if response.Diagnostics.Append(r.getWorkspaceToStackStateConversionMetaData(ctx, maps.Keys(migrationMapFromPlan)[0], plan.Organization.ValueString())...); response.Diagnostics.HasError() {
		tflog.Error(ctx, fmt.Sprintf("[Update-Action] Failed to retrieve workspace state to stack state conversion metadata for organization %q and workspace %q and migration map %v",
			plan.Organization.ValueString(), maps.Keys(migrationMapFromPlan)[0], migrationMapFromPlan))
		return
	}
	tflog.Debug(ctx, fmt.Sprintf("[Update-Action] Successfully retrieved workspace state to stack state conversion metadata for organization %q and workspace %q and migration map %v",
		plan.Organization.ValueString(), maps.Keys(migrationMapFromPlan)[0], migrationMapFromPlan))
	tflog.Debug(ctx, fmt.Sprintf("[Update-Action] Is workspace state fully modular: %v", r.isStateModular))
	tflog.Debug(ctx, fmt.Sprintf("[Update-Action] Workspace to Stacks State resource mapping: %v", r.workspaceToStackMap))

	// Starting to update the state of the existing stack migration resource by uploading the configuration files
	tflog.Info(ctx, "[Update-Action] Starting to apply stack migration configuration to an existing stack migration resource")

	isNewState := false
	isNewHash := false
	if plan.CurrentConfigurationId.IsUnknown() {
		//NOTE: this section handles
		// new configuration upload
		// workspace state conversion
		// and state upload during an update action

		var diags diag.Diagnostics
		_, newState, diags = r.applyStackConfiguration(
			ctx,
			plan.Organization.ValueString(),
			plan.Project.ValueString(),
			plan.Name.ValueString(),
			plan.ConfigurationDir.ValueString(),
			migrationMapFromPlan,
			true, true)
		if response.Diagnostics.Append(diags...); response.Diagnostics.HasError() {
			return
		}
		isNewState = true
	} else if !plan.TerraformConfigHash.IsUnknown() && !plan.SourceBundleHash.IsUnknown() && plan.MigrationHash.IsUnknown() {

		// NOTE: this section handles update of deployment group data for an existing configuration
		//  also retries failed deployments
		r.retryAbandonedDeployments = true
		tflog.Info(ctx, "Updating existing stack migration resource by retrying to upload state to the failed/retryable deployments")
		tflog.Debug(ctx, fmt.Sprintf("Validating precondition before re-attempting state upload for retryable/failed deployments for stack %q in organization %q: %+v", plan.Name.ValueString(), plan.Organization.ValueString(), state.WorkspaceDeploymentMapping))
		if diags := r.createActionPreconditions(
			ctx,
			plan.Organization.ValueString(),
			plan.Project.ValueString(),
			plan.Name.ValueString(),
			plan.ConfigurationDir.ValueString(),
			migrationMapFromPlan,
		); diags.HasError() {
			response.Diagnostics.Append(diags...)
			return
		}
		tflog.Info(ctx, "Successfully validated precondition before re-attempting state upload for retryable/failed deployments for stack migration resource")
		tflog.Info(ctx, "Starting to re-attempt state upload for retryable/failed deployments for stack migration resource")
		newMigrationData := r.uploadStackDeploymentsState(ctx, migrationMapFromPlan)
		tflog.Info(ctx, "Successfully re-attempted state upload for retryable/failed deployments for stack migration resource")
		tflog.Debug(ctx, fmt.Sprintf("Updating migration data for stack %q in organization %q", plan.Name.ValueString(), plan.Organization.ValueString()))
		newMigrationHash, err := r.migrationHashService.GetMigrationHash(newMigrationData)
		if err != nil {
			tflog.Error(ctx, fmt.Sprintf("Failed to generate migration hash for stack %q in organization %q: %s", plan.Name.ValueString(), plan.Organization.ValueString(), err.Error()))
			response.Diagnostics.AddError(
				"Error calculating migration hash",
				fmt.Sprintf("Failed to calculate migration hash: %s", err.Error()),
			)
		}
		plan.MigrationHash = types.StringValue(newMigrationHash)
		isNewHash = true
		tflog.Info(ctx, "Successfully updated plan data with a new migration hash for existing stack migration resource")
	}

	if isNewState {
		plan.CurrentConfigurationId = newState.CurrentConfigurationId
		plan.CurrentConfigurationStatus = newState.CurrentConfigurationStatus
		plan.Name = newState.Name
		plan.Organization = newState.Organization
		plan.Project = newState.Project
		plan.SourceBundleHash = newState.SourceBundleHash
		plan.TerraformConfigHash = newState.TerraformConfigHash
		plan.ConfigurationDir = newState.ConfigurationDir
		plan.TerraformConfigDir = newState.TerraformConfigDir
		plan.WorkspaceDeploymentMapping = newState.WorkspaceDeploymentMapping
		plan.MigrationHash = newState.MigrationHash
		tflog.Info(ctx, "Successfully updated plan data with new stack configuration details for existing stack migration resource")
	}

	if !isNewHash && !isNewState {
		tflog.Info(ctx, "No parameters changed that require stack configuration update or migration hash recalculation, skipping update operation for existing stack migration resource")
		response.Diagnostics.Append(response.State.Set(ctx, &plan)...)
		return
	}

	response.Diagnostics.Append(response.State.Set(ctx, plan)...)
}

// Delete is called when the resource is deleted, since the stack migration resource does not support deletion, it logs a warning and adds a warning to the response diagnostics.
func (r *stackMigrationResource) Delete(ctx context.Context, _ resource.DeleteRequest, response *resource.DeleteResponse) {
	tflog.Warn(ctx, DestroyActionNotSupported)
	response.Diagnostics.AddWarning(DestroyActionNotSupported, DestroyActionNotSupportedDetailed)
}

// Configure is called when the resource is configured, it sets up the TFE client and validates the provider configuration.
func (r *stackMigrationResource) Configure(ctx context.Context, configureRequest resource.ConfigureRequest, configureResponse *resource.ConfigureResponse) {
	tflog.Debug(ctx, fmt.Sprintf("Configuring Stack Migration PR Resource with ProviderData: %+v", configureRequest.ProviderData))
	if configureRequest.ProviderData == nil {
		return
	}

	providerConfigData, ok := configureRequest.ProviderData.(ProviderResourceData)
	if !ok {
		configureResponse.Diagnostics.AddError(
			"Unexpected resource Configure type",
			fmt.Sprintf("Expected tfe.ConfiguredClient, got %T.This is a bug in the tfe provider, so please report it on GitHub.", configureRequest.ProviderData),
		)
		return
	}

	r.tfeUtil = providerConfigData.TfeUtil

	if providerConfigData.Hostname != HcpTerraformHost {
		configureResponse.Diagnostics.AddError(
			"Host name must be set to app.terraform.io",
			fmt.Sprintf("The hostname must be %q, but got %q. Please check your provider configuration.", HcpTerraformHost, providerConfigData.Hostname),
		)
		return
	}

	if providerConfigData.TfeToken == "" {
		configureResponse.Diagnostics.AddError(
			"Missing TFE token",
			"Please provide a valid TFE token in the provider configuration.",
		)
		return
	}

	defaultTfeConfig := tfe.DefaultConfig()
	httpClient := defaultTfeConfig.HTTPClient
	transport := httpClient.Transport.(*http.Transport)

	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	transport.TLSClientConfig.InsecureSkipVerify = providerConfigData.SslSkipVerify

	defaultTfeConfig.Address = fmt.Sprintf("%s://%s", constants.TfRemoteScheme, providerConfigData.Hostname)
	defaultTfeConfig.Token = providerConfigData.TfeToken
	defaultTfeConfig.HTTPClient = httpClient

	// configure the resource with tfe configuration
	r.tfeConfig = defaultTfeConfig

	// create a new HTTPUtil client with the configured TLS settings
	httpUtilClient := httpUtil.NewClient(ctx)
	if err := httpUtilClient.SetTlsConfig(transport.TLSClientConfig); err != nil {
		configureResponse.Diagnostics.AddError(
			"Failed to set TLS configuration",
			fmt.Sprintf("Could not set TLS configuration: %s", err.Error()),
		)
		return
	}
	r.httpClient = httpUtilClient

	hcpTerraformClient, err := r.tfeUtil.NewClient(defaultTfeConfig)
	if err != nil {
		configureResponse.Diagnostics.AddError(
			"Failed to create TFE client",
			fmt.Sprintf("Could not create TFE client: %s", err.Error()),
		)
		return
	}

	// set the TFE client in the resource
	r.tfeClient = hcpTerraformClient

	// create a new StackMigrationHashService with the HTTP client
	r.migrationHashService = NewStackMigrationHashService(ctx, r.tfeUtil, r.tfeConfig, r.tfeClient, r.httpClient)

	// set the hcl parser for parsing HCL files
	r.hclParser = hclparse.NewParser()

	// initialize the deployment state import map
	r.deploymentStateImportMap = make(map[string]bool)

	r.tfstateUtil = tfstateUtil.NewTfWorkspaceStateUtility(ctx)

	tflog.Debug(ctx, fmt.Sprintf("resource configuration completd with clients: tfeclient: %+v", r.tfeClient))
}

// ModifyPlan is called to modify the plan before it is applied. It checks the current state and modifies the plan based on the existing state and the update strategy.
func (r *stackMigrationResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	tflog.Debug(ctx, "Starting ModifyPlan operation for stack migration resource")
	var plan StackMigrationResourceModel
	var state StackMigrationResourceModel
	var err error

	// If this is a destroy operation, no modifications needed
	if req.Plan.Raw.IsNull() {
		return
	}

	// If this is a create operation, the state will be empty
	if req.State.Raw.IsNull() {
		tflog.Debug(ctx, "No existing state found, skipping plan modifications for create operation")
		return
	}

	r.tfeUtil.UpdateContext(ctx)
	r.httpClient.UpdateContext(ctx)
	r.migrationHashService.UpdateContext(ctx)
	r.stackSourceBundleAbsPath = plan.ConfigurationDir.ValueString()
	r.terraformConfigDirAbsPath = plan.TerraformConfigDir.ValueString()

	// Retrieve values from the plan
	if resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...); resp.Diagnostics.HasError() {
		tflog.Error(ctx, errFailedToGetPlanValues)
		return
	}

	// Retrieve values from the state
	if resp.Diagnostics.Append(req.State.Get(ctx, &state)...); resp.Diagnostics.HasError() {
		tflog.Error(ctx, errFailedToGetStateValues)
		return
	}

	// validate all the fields in the state are non-empty strings
	if resp.Diagnostics.Append(validateStateData(state)...); resp.Diagnostics.HasError() {
		return
	}

	migrationMapFromPlan := map[string]string{}
	for k, v := range plan.WorkspaceDeploymentMapping.Elements() {
		migrationMapFromPlan[k] = v.(types.String).ValueString()
	}

	// validate action preconditions
	if resp.Diagnostics.Append(
		r.createActionPreconditions(ctx, plan.Organization.ValueString(), plan.Project.ValueString(), plan.Name.ValueString(), plan.ConfigurationDir.ValueString(), migrationMapFromPlan)...); resp.Diagnostics.HasError() {
		return
	}

	// get the current configurationHash
	currentSourceBundleHash, err := r.tfeUtil.CalculateConfigFileHash(plan.ConfigurationDir.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			configHasErrSummary,
			fmt.Sprintf(configHashErrDetails, plan.ConfigurationDir.ValueString(), err.Error()),
		)
		tflog.Error(ctx, fmt.Sprintf("Failed to calculate the hash of the configuration files in the directory %s: %s", plan.ConfigurationDir.ValueString(), err.Error()))
		return
	}

	// get the current terraform configuration hash
	currentTerraformConfigHash, err := r.tfeUtil.CalculateConfigFileHash(plan.TerraformConfigDir.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			configHasErrSummary,
			fmt.Sprintf(configHashErrDetails, plan.TerraformConfigDir.ValueString(), err.Error()),
		)
		tflog.Error(ctx, fmt.Sprintf("Failed to calculate the hash of the Terraform configuration files in the directory %s: %s", plan.TerraformConfigDir.ValueString(), err.Error()))
		return
	}

	stackName := r.existingStack.Name
	organizationName := r.existingOrganization.Name
	projectName := r.existingProject.Name
	plan.SourceBundleHash = types.StringValue(currentSourceBundleHash)
	plan.TerraformConfigHash = types.StringValue(currentTerraformConfigHash)

	if r.existingStack.LatestStackConfiguration == nil {
		// NOTE: if latestStackConfiguration is nil,
		//  meaning the stack configuration has been deleted outside the resource
		//  In this case we modify the attributes as follows
		//   - current_configuration_id = unknown
		//   - current_configuration_status = unknown
		//   - migration_hash = unknown
		//   - source_bundle_hash = currentSourceBundleHash
		//   - terraform_config_hash = currentTerraformConfigHash

		plan.CurrentConfigurationId = types.StringUnknown()
		plan.CurrentConfigurationStatus = types.StringUnknown()
		plan.MigrationHash = types.StringUnknown()
		resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
		return
	}

	// Current data
	currentStackConfigID := r.existingStack.LatestStackConfiguration.ID
	currentStackConfigStatus := r.existingStack.LatestStackConfiguration.Status

	// State data
	stateConfigurationId := state.CurrentConfigurationId.ValueString()
	stateConfigurationStatus := state.CurrentConfigurationStatus.ValueString()

	if currentStackConfigID == "" || string(currentStackConfigStatus) == "" {
		// validate latestStackConfiguration in the existing stack
		resp.Diagnostics.AddError(
			"Invalid Stack Configuration State",
			fmt.Sprintf(stackConstants.CurrentStackConfigIsNotValid, stackName, organizationName, projectName),
		)
		return
	}

	plan.CurrentConfigurationId = types.StringValue(currentStackConfigID)

	// handle configurationId changes
	if stateConfigurationId != currentStackConfigID {
		plan.CurrentConfigurationStatus = types.StringUnknown()
		plan.MigrationHash = types.StringUnknown()
		resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
		return
	}

	// handle configuration status changes
	if stateConfigurationStatus != currentStackConfigStatus.String() {
		plan.CurrentConfigurationStatus = types.StringUnknown()
		plan.MigrationHash = types.StringUnknown()
		resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
		return
	}

	plan.CurrentConfigurationStatus = types.StringValue(currentStackConfigStatus.String())

	// NOTE:
	//  Handle `tfe.StackConfigurationStatusErrored`, `tfe.StackConfigurationStatusCanceled`.
	//  At this we are sure that neither the configuration ID nor status has changed.
	//  and the configuration has either errored, cancelled from the last apply
	if slices.Contains(stackConstants.ErroredOrCancelledStackConfigurationStatuses,
		tfe.StackConfigurationStatus(state.CurrentConfigurationStatus.ValueString())) {
		// If the prior configuration is in an errored or canceled state, any changes are allowed.
		tflog.Debug(ctx, "Prior stack configuration is in an errored or cancelled state, allowing all changes")
		plan.CurrentConfigurationId = types.StringUnknown()
		plan.CurrentConfigurationStatus = types.StringUnknown()
		plan.MigrationHash = types.StringUnknown()
		resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
		return
	}

	// NOTE: Handle `tfe.StackConfigurationStatusRunning`
	//  At this we are sure that neither the configuration ID nor status has changed.
	//  and the configuration is still running from the last apply
	if slices.Contains(stackConstants.RunningStackConfigurationStatuses,
		currentStackConfigStatus) {
		// NOTE: If the configurationId and status are unchanged and configuration is running
		//  check the following attributes for idempotency:
		//  - source_bundle_hash
		//  - terraform_config_hash
		//  - workspace_deployment_mapping
		if isIdempotentConfig, diags := r.isIdempotentConfig(plan, state,
			currentSourceBundleHash, currentTerraformConfigHash); !isIdempotentConfig || diags.HasError() {
			resp.Diagnostics.Append(diags...)
			return
		}

		// NOTE: If the config is idempotent, we set the plan attributes to the existing state values
		//  except for migration_hash which is set to unknown because the underlying
		//  deployment groups will have changed statuses once the current running configuration reaches
		//  terminal status and the then state upload is performed and needs to be synced via update action
		tflog.Debug(ctx,
			"Prior stack configuration is still running, and no config changes detected, no re-upload needed, continuing to wait for the running configuration to reach terminal status and retry state migration afterwards")
		plan.MigrationHash = types.StringUnknown()
		resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
		return
	}

	// Note: At this point we are sure that neither the configuration ID nor status has changed,
	//  nor the configuration status from the last apply has not errored or cancelled and is not running,
	//  now we need to check if there are any running deployment groups as the configuration status
	//  is `tfe.StackConfigurationStatusCompleted`
	hasRunningDeploymentGroups, err := r.tfeUtil.StackConfigurationHasRunningDeploymentGroups(currentStackConfigID, r.tfeClient)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Checking Running Deployment Groups",
			fmt.Sprintf("Could not check running deployment groups for stack %q in organization %q and project %q: %s", stackName, organizationName, projectName, err.Error()),
		)
		return
	}

	hasSourceConfigHashChanged := state.SourceBundleHash.ValueString() != currentSourceBundleHash
	hasTerraformConfigHashChanged := state.TerraformConfigHash.ValueString() != currentTerraformConfigHash

	// If there are no running deployment groups, we can proceed to re-uploading the configuration
	if !hasRunningDeploymentGroups {
		tflog.Debug(ctx, "No running deployment groups detected for the current stack configuration checking config file changes and deployment group summary for failed deployment groups")
		// Get all the deployment groups summary for the latest stack configuration
		stackDeploymentGroupSummaryListForTheLatestConfigurationId, err := r.tfeUtil.GetDeploymentGroupSummaryByConfigID(currentStackConfigID, r.tfeClient)
		if err != nil {
			resp.Diagnostics.AddError(
				"Error Getting Deployment Group Summary",
				fmt.Sprintf("Could not get deployment group summary for stack %q in organization %q and project %q: %s", stackName, organizationName, projectName, err.Error()),
			)
			return
		}
		successfulDeploymentGroupsCount, failedOrAbandonedDeploymentGroupsCount := getDeploymentGroupCountByTerminalStatus(stackDeploymentGroupSummaryListForTheLatestConfigurationId)

		// NOTE: All deployment groups have succeeded, neither the stack configuration files
		//  nor the terraform configuration files have changed no modification of rescan is needed
		//  NO-OP scenario
		if successfulDeploymentGroupsCount == len(migrationMapFromPlan) && !hasSourceConfigHashChanged && !hasTerraformConfigHashChanged {
			tflog.Debug(ctx, "All deployment groups have succeeded and no configuration changes detected, no resource update needed")
			plan.MigrationHash = state.MigrationHash
			resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
			return
		}

		// NOTE: All deployment groups in the stack configuration have reached terminal status and based on the following data
		//  - whether the stack configuration files have changed
		//  - whether the terraform configuration files have changed
		//  - number of failed or abandoned deployment groups.
		//  We have the following options to choose form:
		//  - if the stack configuration files have changed --> then we need to trigger a new source bundle upload
		//  - if the terraform configuration files have changed --> then we need to trigger a source bundle upload
		//  - if all deployment groups have failed --> then we need to trigger a new source bundle upload
		//  - if all the deployment groups have succeeded and either the stack configuration files or the terraform configuration files have changed --> then we need to trigger a new source bundle upload
		//  - if the deployment groups have one or more failed or abandoned deployments and neither the stack configuration files nor the terraform configuration files have changed --> then we just retry the failed deployment groups without re-uploading the configuration files
		hasAllDeploymentsHaveFailed := failedOrAbandonedDeploymentGroupsCount == len(migrationMapFromPlan)
		if hasSourceConfigHashChanged ||
			hasTerraformConfigHashChanged ||
			hasAllDeploymentsHaveFailed {
			logEntry := fmt.Sprintf("New source bundle upload condition has been met, triggering a new source bundle upload stack %q in organization %q and project %q. hasSourceConfigHashChanged:%t, hasTerraformConfigHashChanged:%t, failedOrAbandonedDeploymentGroupsCount:%d, migrationMapSize: %d, hasAllDeploymentsHaveFailed %t",
				stackName, organizationName, projectName, hasSourceConfigHashChanged, hasTerraformConfigHashChanged, failedOrAbandonedDeploymentGroupsCount, len(migrationMapFromPlan), hasAllDeploymentsHaveFailed)
			tflog.Debug(ctx, logEntry)
			plan.CurrentConfigurationId = types.StringUnknown()
			plan.CurrentConfigurationStatus = types.StringUnknown()
			plan.MigrationHash = types.StringUnknown()
			resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
			return
		}

		// NOTE: At this we are sure of the following:
		//  - The latest stack configuration is not running, has not errored or cancelled but completed
		//  - Neither stack configuration files nor terraform configuration files have changed
		//  - All deployment groups in the stack configuration have reached terminal status but have one or more failed or abandoned deployment groups but not all of them has failed or abandoned
		//  So we have one of the following options to choose from:
		//  - We need to retry the failed or abandoned deployment groups without re-uploading the configuration files.

		tflog.Debug(ctx, "Failed deployment groups detected without configuration changes, no configuration upload needed, failed deployment groups will be retried")
		plan.MigrationHash = types.StringUnknown()
		resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
		return

	}

	// NOTE: If there are running deployment groups, check for idempotency
	//  of the following attributes:
	//  - source_bundle_hash
	//  - terraform_config_hash
	//  - workspace_deployment_mapping
	//  We repeat the idempotency check here because if the configurationID and status are unchanged the
	//  prior idempotency check would have been skipped.
	//  This is because the prior idempotency check is only done when the configuration is in running state.
	//  Here we need to check for idempotency if there are running deployment groups as the
	//  prior configuration is `tfe.StackConfigurationStatusCompleted`
	if isIdempotentConfig, diags := r.isIdempotentConfig(plan, state, currentSourceBundleHash,
		currentTerraformConfigHash); !isIdempotentConfig || diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	// NOTE: At this point we are sure that neither the configuration ID nor status has changed,
	//  nor the configuration status from the last apply has not errored or cancelled and is not running and,
	//  there are running deployment groups, and the configuration is idempotent.
	//  Now we need to check the deployment statuses to determine if there are any failed deployments
	//  that need to be retried without re-uploading the configuration.

	// Get a migration map from the state
	deploymentMappingElements := state.WorkspaceDeploymentMapping.Elements()
	migrationMap := make(map[string]string, len(deploymentMappingElements))
	for key, value := range deploymentMappingElements {
		migrationMap[key] = value.(types.String).ValueString()
	}

	var migrationDataFromState map[string]StackMigrationData
	var currentMigrationData map[string]StackMigrationData

	if migrationDataFromState, err = r.migrationHashService.GetMigrationData(state.MigrationHash.ValueString()); err != nil {
		resp.Diagnostics.AddError(
			"Error Getting Migration Data from State",
			fmt.Sprintf("Could not get migration data from state for stack %q in organization %q and project %s: %s", stackName, organizationName, projectName, err.Error()),
		)
		return
	}

	stackMigrationTrackRequest := StackMigrationTrackRequest{
		MigrationMap: migrationMap,
		OrgName:      organizationName,
		StackId:      r.existingStack.ID,
	}

	if currentMigrationData, err = r.migrationHashService.GenerateMigrationData(stackMigrationTrackRequest); err != nil {
		resp.Diagnostics.AddError(
			"Error Generating Migration Data",
			fmt.Sprintf("Could not generate migration data for stack %q in organization %q and project %s: %s", stackName, organizationName, projectName, err.Error()),
		)
		return
	}

	workspacesToBeRetried, diags := r.getWorkspacesForDeploymentRetry(ctx, currentMigrationData, migrationDataFromState)
	if diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	tflog.Debug(ctx, fmt.Sprintf("=====Deployment Status Diff: %+v======", workspacesToBeRetried))

	if workspacesToBeRetried.Cardinality() > 0 {
		// If there is a partial failure, no changes are allowed
		tflog.Debug(ctx, "Partial deployment group failures detected with running deployment groups and no configuration changes, no configuration upload needed, failed deployment groups will be retried")
		plan.MigrationHash = types.StringUnknown()
		resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
		return
	}

	tflog.Debug(ctx, "Completed ModifyPlan operation for stack migration resource")
}

// Metadata returns the metadata for the stack migration resource.
func (r *stackMigrationResource) Metadata(_ context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + stackMigrationResourceName
}

func (r *stackMigrationResource) getDeploymentNamesFromStackConfigDir(stackConfigDir string) (mapset.Set[string], error) {
	filePathGlobPattern := fmt.Sprintf("%s%s*%s", stackConfigDir, string(os.PathSeparator), stackDeploymentHclFileExt)
	stackFiles, err := filepath.Glob(filePathGlobPattern)
	if err != nil {
		return nil, fmt.Errorf("error while fetching stack files from path %s, err: %w", stackConfigDir, err)
	}

	allDeployments := mapset.NewSet[string]()

	for _, filePath := range stackFiles {
		deployments, err := r.getAllDeployments(filePath)
		if err != nil || deployments == nil || deployments.Cardinality() == 0 {
			return nil, fmt.Errorf("error while getting deployments from file %s, err: %w", filePath, err)
		}

		allDeployments = allDeployments.Union(deployments)
	}

	return allDeployments, nil
}

func (r *stackMigrationResource) getAllDeployments(filePath string) (mapset.Set[string], error) {

	// parse the hcl file at the given filePath
	file, diags := r.hclParser.ParseHCLFile(filePath)
	if diags.HasErrors() {
		return nil, fmt.Errorf("failed to parse HCL file %s, err: %v", filePath, diags.Error())
	}

	// check if the file is nil or has no-body
	if file == nil || file.Body == nil {
		return nil, nil
	}

	// define the stackDeploymentBlockSchema to extract blocks of type "component" with a label "name"
	stackDeploymentBlockSchema := &hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{
				Type:       "deployment",
				LabelNames: []string{"name"},
			},
		},
	}

	// use PartialContent to get the content of the file that matches the stackDeploymentBlockSchema
	// this will return the blocks of type "deployment" with their labels
	// it is important that we use PartialContent here,
	// as the parsed file may contain other blocks that we are not interested in
	content, _, diags := file.Body.PartialContent(stackDeploymentBlockSchema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("failed to get partial content for file %s, err: %v", filePath, diags.Error())
	}

	// check if the content is nil or has no content blocks
	// if so, return nil
	if content == nil || len(content.Blocks) == 0 {
		return nil, nil
	}

	deployments := mapset.NewSet[string]()

	// let us iterate through the blocks and extract the labels
	// we assume that each block of a type "component" has one label (the name)
	// if there are multiple labels, we will only take the first one
	// we also assume that we have exactly one distinct label per component block
	for _, block := range content.Blocks {
		deploymentName := block.Labels[0]
		importValue, err := getImportBlockData(block.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to get import block data for file %s, err: %v", filePath, err)

		}

		if deployments.Contains(deploymentName) {
			return nil, fmt.Errorf("duplicate deployment name found in file %s: %s", filePath, deploymentName)
		}
		deployments.Add(deploymentName)                          // Add the first label of the block to the set of deployments
		r.deploymentStateImportMap[deploymentName] = importValue // Store the import value for the deployment
	}

	return deployments, nil
}

func getImportBlockData(body hcl.Body) (bool, error) {
	attrs, diags := body.JustAttributes()
	attr, ok := attrs["import"]
	if !ok {
		return false, nil
	}

	if diags.HasErrors() {
		return false, fmt.Errorf("failed to get attributes from body: %s", diags.Error())
	}
	if diags.HasErrors() {
		return false, fmt.Errorf("failed to get import value: %s", diags.Error())
	}
	importValue, diags := attr.Expr.Value(nil)

	if diags.HasErrors() || importValue.IsNull() {
		return false, fmt.Errorf("failed to get import value: %v", diags.Error())
	}
	return importValue.True(), nil
}

func validateStateData(state StackMigrationResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if (state.ConfigurationDir.ValueString() == "") ||
		(state.CurrentConfigurationId.ValueString() == "") ||
		(state.CurrentConfigurationStatus.ValueString() == "") ||
		(state.MigrationHash.ValueString() == "") ||
		(state.SourceBundleHash.ValueString() == "") ||
		(state.TerraformConfigDir.ValueString() == "") ||
		(state.TerraformConfigHash.ValueString() == "") ||
		(state.WorkspaceDeploymentMapping.IsNull() || state.WorkspaceDeploymentMapping.IsUnknown() || len(state.WorkspaceDeploymentMapping.Elements()) == 0) {
		diags.AddError(
			"Invalid State Values",
			"One or more required state values are empty or null. Please ensure all required state values are set by running a `terraform refresh` operation before updating the resource.",
		)
	}

	return diags
}

func (r *stackMigrationResource) getWorkspaceToStackStateConversionMetaData(ctx context.Context, workspaceName string, organizationName string) diag.Diagnostics {
	var diags diag.Diagnostics

	// retrieve a state file no make `terraform state list` infallible
	tflog.Debug(ctx, fmt.Sprintf("Retrieving workspace state file for workspace %q in organization %q", workspaceName, organizationName))
	stateFilePath, err := r.tfeUtil.PullAndSaveWorkspaceStateData(organizationName, workspaceName, r.tfeClient)
	if err != nil {
		errorMsg := fmt.Sprintf("Failed to pull and save the workspace state data for organization %q and workspace %q: %s", organizationName, workspaceName, err.Error())
		tflog.Error(ctx, errorMsg)
		diags.AddError(
			"Error Getting Workspace State Data",
			errorMsg,
		)
		return diags
	}
	tflog.Debug(ctx, fmt.Sprintf("Successfully retrieved workspace state file for workspace %q in organization %q to path %s", workspaceName, organizationName, stateFilePath))

	defer func() {
		tflog.Debug(ctx, fmt.Sprintf("Deleting workspace state file for workspace %q in organization %q", workspaceName, organizationName))
		if err := os.Remove(stateFilePath); err != nil {
			tflog.Error(ctx, fmt.Sprintf("Failed to remove temporary state file %q: %s", stateFilePath, err.Error()))
		}
		tflog.Debug(ctx, fmt.Sprintf("Successfully deleted workspace state file for workspace %q in organization %q", workspaceName, organizationName))
	}()

	tflog.Debug(ctx, fmt.Sprintf("Retrieving state resources from workspace state file %s", stateFilePath))
	resourcesFromWorkspaceState, err := r.tfstateUtil.ListAllResourcesFromWorkspaceStateWithStateFile(r.terraformConfigDirAbsPath, stateFilePath)
	if err != nil {
		errorMessage := fmt.Sprintf("Failed to list resources from the workspace state in the directory %q: %s", r.terraformConfigDirAbsPath, err.Error())
		tflog.Error(ctx, errorMessage)
		diags.AddError(
			"Error Listing Resources from Workspace State",
			errorMessage,
		)
		return diags
	}
	tflog.Debug(ctx, fmt.Sprintf("Successfully retrieved state resources from workspace state file %s", stateFilePath))

	r.isStateModular = r.tfstateUtil.IsFullyModular(resourcesFromWorkspaceState)
	tflog.Debug(ctx, fmt.Sprintf("Is workspace state modular: %t", r.isStateModular))

	workspaceToStackAddressMapRequest := tfstateUtil.WorkspaceToStackAddressMapRequest{
		StackSourceBundleAbsPath:    r.stackSourceBundleAbsPath,
		TerraformConfigFilesAbsPath: r.terraformConfigDirAbsPath,
		StateFilePath:               stateFilePath,
	}

	tflog.Debug(ctx, fmt.Sprintf("Retrieving workspace to stack address map for workspace %q in organization %q", workspaceName, organizationName))
	workspaceStackAddressMap, err := r.tfstateUtil.WorkspaceToStackAddressMap(workspaceToStackAddressMapRequest)
	if err != nil {
		errorMessage := fmt.Sprintf("Failed to create workspace to stack map from the Terraform configuration directory %q and stack source bundle directory %q: %s", r.terraformConfigDirAbsPath, r.stackSourceBundleAbsPath, err.Error())
		tflog.Error(ctx, errorMessage)
		diags.AddError(
			"Error Creating Workspace to Stack Map",
			errorMessage,
		)
		return diags
	}
	r.workspaceToStackMap = workspaceStackAddressMap
	tflog.Debug(ctx, fmt.Sprintf("Successfully retrieved workspace to stack address map for workspace %q in organization %q", workspaceName, organizationName))
	return nil
}

func getDeploymentGroupCountByTerminalStatus(list *tfe.StackDeploymentGroupSummaryList) (int, int) {
	successfulDeploymentGroupCount := 0
	failedDeploymentGroupCount := 0

	for _, deploymentGroupSummary := range list.Items {
		deploymentGroupStatus := tfe.DeploymentGroupStatus(deploymentGroupSummary.Status)
		switch deploymentGroupStatus {
		case tfe.DeploymentGroupStatusSucceeded:
			successfulDeploymentGroupCount++
		case tfe.DeploymentGroupStatusFailed, tfe.DeploymentGroupStatusAbandoned:
			failedDeploymentGroupCount++
		}

	}
	return successfulDeploymentGroupCount, failedDeploymentGroupCount
}
