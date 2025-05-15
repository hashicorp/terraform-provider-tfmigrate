// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"os"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/zclconf/go-cty/cty"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource = &directoryActions{}
)

// NewDirectoryActionResource is a helper function to simplify the provider implementation.
func NewDirectoryActionResource() resource.Resource {
	return &directoryActions{}
}

// directoryActions is the resource implementation.
type directoryActions struct {
	Hostname string
}

// DirectoryActionResourceModel describes the resource data model.
type DirectoryActionResourceModel struct {
	Org           types.String `tfsdk:"org"`
	Project       types.String `tfsdk:"project"`
	DirectoryPath types.String `tfsdk:"directory_path"`
	BackendFile   types.String `tfsdk:"backend_file_name"`
	WorkspaceMap  types.Map    `tfsdk:"workspace_map"`
	Tags          types.List   `tfsdk:"tags"`
}

// Metadata returns the resource type name.
func (r *directoryActions) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_update_backend"
}

// Schema defines the schema for the resource.
func (r *directoryActions) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "`tfmigrate_update_backend` will remove the existing backend block from the terraform configuration file and add the cloud block with the provided workspace mapping and tags.",

		Attributes: map[string]schema.Attribute{
			"directory_path": schema.StringAttribute{
				MarkdownDescription: "Path where the backend file can be found.",
				Required:            true,
			},
			"backend_file_name": schema.StringAttribute{
				MarkdownDescription: "Name of the file containing the terraform backend configuration.",
				Required:            true,
			},
			"org": schema.StringAttribute{
				MarkdownDescription: "Organization name required in the cloud block.",
				Required:            true,
			},
			"project": schema.StringAttribute{
				MarkdownDescription: "Project Name required in the cloud block.",
				Required:            true,
			},
			"workspace_map": schema.MapAttribute{
				MarkdownDescription: "Terraform cloud workspace to local workspace mapping.",
				ElementType:         types.StringType,
				Required:            true,
			},
			"tags": schema.ListAttribute{
				MarkdownDescription: "Tags used when there are multiple workspaces.",
				ElementType:         types.StringType,
				Required:            true,
			},
		},
	}
}

// Create creates the resource and sets the initial Terraform state.
func (r *directoryActions) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data DirectoryActionResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	RemoveBackendBlock(ctx, data.DirectoryPath.ValueString(), data.BackendFile.ValueString(), resp)
	tflog.Trace(ctx, "Completed Removing backend block.")
	AddCloudBlock(ctx, data, data.BackendFile.ValueString(), r.Hostname, resp)
	tflog.Trace(ctx, "Completed Appending a cloud block.")
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Read refreshes the Terraform state with the latest data.
func (r *directoryActions) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *directoryActions) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data DirectoryActionResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.AddWarning(UpdateActionNotSupported, UpdateActionNotSupportedDetailed)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)

}

// Delete deletes the resource and removes the Terraform state on success.
func (r *directoryActions) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
}

func RemoveBackendBlock(ctx context.Context, dirPath string, backendFile string, resp *resource.CreateResponse) {
	tflog.Info(ctx, "[TFM] Removing backend block")
	filePath := dirPath + "/" + backendFile
	content, err := os.ReadFile(filePath)
	if err != nil {
		tflog.Error(ctx, "[TFM] ERROR while reading terraform config", map[string]any{"error": err})
		resp.Diagnostics.AddError("ERROR while reading terraform config", " Error "+err.Error())
		return
	}
	file, diags := hclwrite.ParseConfig(content, filePath, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		tflog.Error(ctx, "[TFM] ERROR while parsing terraform config", map[string]any{"error": diags.Error()})
		resp.Diagnostics.AddError("ERROR while parsing terraform config", " Error "+diags.Error())
		return
	}
	for _, block := range file.Body().Blocks() {
		if block.Type() == "terraform" {
			for _, bl := range block.Body().Blocks() {
				if bl.Type() == "backend" {
					block.Body().RemoveBlock(bl)
				}
			}
		}
	}
	if err := os.WriteFile(filePath, file.Bytes(), 0644); err != nil {
		tflog.Error(ctx, "[TFM] ERROR while writing terraform config", map[string]any{"error": err})
		resp.Diagnostics.AddError("Error while writing terraform config", " Error "+err.Error())
		return
	}
}

func AddCloudBlock(ctx context.Context, data DirectoryActionResourceModel, backendFile string, hostname string, resp *resource.CreateResponse) {
	tflog.Info(ctx, "[TFM] Adding cloud block")
	filePath := data.DirectoryPath.ValueString() + "/" + backendFile
	content, err := os.ReadFile(filePath)
	if err != nil {
		tflog.Error(ctx, "[TFM] ERROR while reading terraform config", map[string]any{"error": err})
		resp.Diagnostics.AddError("ERROR while reading terraform config", " Error "+err.Error())
		return
	}
	file, diags := hclwrite.ParseConfig(content, filePath, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		tflog.Error(ctx, "[TFM] ERROR while parsing terraform config", map[string]any{"error": diags.Error()})
		resp.Diagnostics.AddError("ERROR while parsing terraform config", " Error "+diags.Error())
		return
	}
	for _, block := range file.Body().Blocks() {
		if block.Type() == "terraform" {
			for _, bl := range block.Body().Blocks() {
				if bl.Type() == "cloud" {
					tflog.Info(ctx, "[TFM] Skipping adding cloud block as already exist")
					return
				}
			}
			cloudBlock := block.Body().AppendNewBlock("cloud", nil)
			cloudBlock.Body().SetAttributeValue("organization", cty.StringVal(data.Org.ValueString()))
			cloudBlock.Body().SetAttributeValue("hostname", cty.StringVal(hostname))

			m, d := data.WorkspaceMap.ToMapValue(ctx)
			if d.HasError() {
				tflog.Error(ctx, "[TFM] ERROR while reading workspace map from terraform config", map[string]any{"error": d.Errors()})
				for _, derr := range d {
					resp.Diagnostics.Append(derr)
				}
				return
			}

			if len(m.Elements()) == 0 {
				tflog.Error(ctx, "[TFM] ERROR empty workspace mapping provided")
				resp.Diagnostics.AddError("ERROR empty workspace mapping provided", " No workspace provided")
				return
			}
			if len(m.Elements()) == 1 {
				workspace, isError := getTFCWorkspace(ctx, m, resp)
				if isError {
					return
				}
				workspacesBlock := cloudBlock.Body().AppendNewBlock("workspaces", nil)
				workspacesBlock.Body().SetAttributeValue("project", cty.StringVal(data.Project.ValueString()))
				workspacesBlock.Body().SetAttributeValue("name", cty.StringVal(workspace))
			}
			//----- multiple workspaces will write tags
			if len(m.Elements()) > 1 {
				tags := make([]cty.Value, len(data.Tags.Elements()))
				for i, v := range data.Tags.Elements() {
					var tag string
					tfValue, err := v.ToTerraformValue(ctx)
					if err != nil {
						tflog.Error(ctx, "[TFM] ERROR while parsing workspace name from  terraform config map", map[string]any{"error": err})
						resp.Diagnostics.AddError("ERROR while parsing workspace name from  terraform config map", " Error "+err.Error())
						return
					}
					err = tfValue.As(&tag)
					if err != nil {
						tflog.Error(ctx, "[TFM] ERROR while reading  workspace name", map[string]any{"error": err})
						resp.Diagnostics.AddError("ERROR while reading  workspace name", " Error "+err.Error())
					}
					tags[i] = cty.StringVal(tag)
				}
				workspacesBlock := cloudBlock.Body().AppendNewBlock("workspaces", nil)
				workspacesBlock.Body().SetAttributeValue("project", cty.StringVal(data.Project.ValueString()))
				workspacesBlock.Body().SetAttributeValue("tags", cty.ListVal(tags))
			}
			break
		}
	}
	if err := os.WriteFile(filePath, file.Bytes(), 0644); err != nil {
		tflog.Error(ctx, "[TFM] ERROR while writing terraform config", map[string]any{"error": err})
		resp.Diagnostics.AddError("Error while writing terraform config", " Error "+err.Error())
		return
	}
}

func getTFCWorkspace(ctx context.Context, m basetypes.MapValue, resp *resource.CreateResponse) (string, bool) {
	workspace := ""
	for _, v := range m.Elements() {
		tfValue, err := v.ToTerraformValue(ctx)
		if err != nil {
			tflog.Error(ctx, "[TFM] ERROR while parsing workspace name from  terraform config map", map[string]any{"error": err})
			resp.Diagnostics.AddError("ERROR while parsing workspace name from  terraform config map", " Error "+err.Error())
			return "", true
		}
		err = tfValue.As(&workspace)
		if err != nil {
			tflog.Error(ctx, "[TFM] ERROR while reading  workspace name", map[string]any{"error": err})
			resp.Diagnostics.AddError("ERROR while reading  workspace name", " Error "+err.Error())
			return "", true
		}
		return workspace, false
	}
	return workspace, false
}

func (r *directoryActions) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerResourceData, ok := req.ProviderData.(ProviderResourceData)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected TF_GIT_PAT_TOKEN Token Found",
			fmt.Sprintf("providerResourceData from context is %v.", providerResourceData),
		)

		return
	}
	r.Hostname = providerResourceData.Hostname
}
