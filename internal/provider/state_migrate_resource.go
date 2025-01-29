// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"crypto/md5"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"terraform-provider-tfmigrate/internal/terraform"

	"github.com/hashicorp/go-tfe"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

type stateMigration struct {
	Hostname string
}

var (
	_ resource.Resource = &stateMigration{}
)

const TfcTokenPath = ".terraform.d/credentials.tfrc.json"
const TfcScheme = "https"

var tfeClient *tfe.Client

func NewStateMigrationResource() resource.Resource {
	return &stateMigration{}
}

type stateMigrationModel struct {
	DirectoryPath  types.String `tfsdk:"directory_path"`
	Org            types.String `tfsdk:"org"`
	LocalWorkspace types.String `tfsdk:"local_workspace"`
	TFCWorkspace   types.String `tfsdk:"tfc_workspace"`
}

func (r *stateMigration) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_state_migration"
}

func (r *stateMigration) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Resource that downloads state from current backend and uploads it to HCP Terraform",
		Attributes: map[string]schema.Attribute{
			"directory_path": schema.StringAttribute{
				MarkdownDescription: "The directory path where terraform root module is located",
				Required:            true,
			},
			"org": schema.StringAttribute{
				MarkdownDescription: "Organization name where the state should be uploaded.",
				Required:            true,
			},
			"local_workspace": schema.StringAttribute{
				MarkdownDescription: "Terraform community workspace name",
				Required:            true,
			},
			"tfc_workspace": schema.StringAttribute{
				MarkdownDescription: "Terraform cloud workspace name",
				Required:            true,
			},
		},
	}
}

func (r *stateMigration) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {

	var data stateMigrationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	dirPath := data.DirectoryPath.ValueString()
	tfOps := &terraform.TerraformOperation{
		DirectoryPath: dirPath,
	}
	_, err := os.Stat(dirPath)
	if err != nil {
		tflog.Error(ctx, DirPathDoesNotExist)
		resp.Diagnostics.AddError(DirPathDoesNotExist, DirPathDoesNotExistDetailed)
		return
	}

	err = tfOps.ExecuteTerraformInit(ctx)
	if err != nil {
		tflog.Error(ctx, "Error initializing terraform ", map[string]any{"error": err})
		resp.Diagnostics.AddError("Error initializing terraform "+data.LocalWorkspace.ValueString(), err.Error())
		return
	}

	err = tfOps.SelectWorkspace(ctx, data.LocalWorkspace.ValueString())
	if err != nil {
		tflog.Error(ctx, "Error selecting workspace ", map[string]any{"error": err})
		resp.Diagnostics.AddError("Error selecting workspace "+data.LocalWorkspace.ValueString(), err.Error())
		return
	}

	state, err := tfOps.StatePull(ctx)
	if err != nil {
		tflog.Error(ctx, "Error downloading state ", map[string]any{"error": err})
		resp.Diagnostics.AddError("Error downloading state "+data.LocalWorkspace.ValueString(), err.Error())
		return
	}
	tflog.Info(ctx, "Migrating state from local ws : "+data.LocalWorkspace.ValueString()+" to tfc : "+data.TFCWorkspace.ValueString(),
		map[string]interface{}{"state": string(state[:])})
	if tfeClient == nil {
		tfeClient, err = newTfeClient(r.Hostname)
		if err != nil {
			tflog.Error(ctx, "Error initializing client", map[string]any{"error": err})
			resp.Diagnostics.AddError("Error initializing client ", err.Error())
			return
		}
	}
	workspace := data.TFCWorkspace.ValueString()
	workspaceDetails, err := tfeClient.Workspaces.Read(ctx, data.Org.ValueString(), workspace)
	if err != nil {
		tflog.Error(ctx, "Error fetching workspace data "+workspace, map[string]any{"error": err})
		resp.Diagnostics.AddError("Error fetching workspace data "+workspace, err.Error())
		return
	}
	workspaceId := workspaceDetails.ID

	err = uploadState(ctx, state, workspaceId, workspace, tfeClient)
	if err != nil {
		tflog.Error(ctx, "Failed to  upload state", map[string]any{"error": err})
		resp.Diagnostics.AddError("Failed to  upload state ", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *stateMigration) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
}

func (r *stateMigration) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data stateMigrationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.AddWarning(UpdateActionNotSupported, UpdateActionNotSupportedDetailed)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *stateMigration) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	tflog.Warn(ctx, DestroyActionNotSupported)
}

func uploadState(ctx context.Context, state []byte, workspaceId string, workspace string, client *tfe.Client) error {

	var meta stateMeta
	if err := json.Unmarshal(state, &meta); err != nil {
		tflog.Error(ctx, "Failed to deserialize  state ")
		return err
	}

	// Lock the workspace
	if _, err := client.Workspaces.Lock(ctx, workspaceId, tfe.WorkspaceLockOptions{}); err != nil {
		tflog.Error(ctx, "Failed to lock workspace")
		return err
	}
	defer func() {
		// Unlock the workspace
		if _, err := client.Workspaces.Unlock(ctx, workspaceId); err != nil {
			tflog.Error(ctx, "Failed to unlock workspace")
		}
	}()

	options := tfe.StateVersionUploadOptions{
		StateVersionCreateOptions: tfe.StateVersionCreateOptions{
			Lineage: tfe.String(meta.Lineage),
			Serial:  tfe.Int64(meta.Serial),
			MD5:     tfe.String(fmt.Sprintf("%x", md5.Sum(state))),
			Force:   tfe.Bool(false),
		},
		RawState: state,
	}

	stateVersion, err := client.StateVersions.Upload(ctx, workspaceId, options)
	if err != nil {
		tflog.Error(ctx, "Failed to upload state")
		return err
	}
	tflog.Info(ctx, "State migrated successfully", map[string]any{"workspace": workspace, "id": stateVersion.ID})
	return nil
}

func newTfeClient(hostname string) (*tfe.Client, error) {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	credsFilePath := filepath.Join(homeDir, TfcTokenPath)
	credsJson, err := os.ReadFile(credsFilePath)
	if err != nil {
		return nil, err
	}

	var tfCredentials TfCredentials
	if err := json.Unmarshal(credsJson, &tfCredentials); err != nil {
		return nil, errors.New("failed to parse credential file" + err.Error())
	}

	tfcConfig := &tfe.Config{
		Address:           TfcScheme + "://" + hostname + "/",
		Token:             tfCredentials.Creds[hostname].Token,
		RetryServerErrors: true,
		HTTPClient:        client,
	}
	return tfe.NewClient(tfcConfig)
}

type TfRemote struct {
	Token string `json:"token"`
}

type TfCredentials struct {
	Creds map[string]TfRemote `json:"credentials"`
}

type stateMeta struct {
	Serial  int64
	Lineage string
}

func (r *stateMigration) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerResourceData, ok := req.ProviderData.(ProviderResourceData)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Github Token Found",
			fmt.Sprintf("providerResourceData from context is %s.", providerResourceData),
		)

		return
	}
	r.Hostname = providerResourceData.Hostname
}
