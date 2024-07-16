package provider

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hashicorp/go-tfe"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"log"
	"os"
	"strings"
	"terraform-provider-tfmigrate/internal/terraform"
)

type stateMigration struct {
}

var (
	_ resource.Resource = &stateMigration{}
)

const TfcTokenPath = ".terraform.d/credentials.tfrc.json"
const TfcAddress = "https://app.terraform.io"

var tfeClient *tfe.Client

func NewStateMigrationResource() resource.Resource {
	return &stateMigration{}
}

type stateMigrationModel struct {
	DirectoryPath  types.String `tfsdk:"directory_path"`
	Org            types.String `tfsdk:"org"`
	LocalWorkspace types.String `tfsdk:"local_workspace"`
	TFCWorkspaceID types.String `tfsdk:"tfc_workspace_id"`
	TFCWorkspace   types.String `tfsdk:"tfc_workspace"`
}

func (r *stateMigration) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_state_migration"
}

func (r *stateMigration) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "TF-Migrate state migration resource",
		Attributes: map[string]schema.Attribute{
			"directory_path": schema.StringAttribute{
				MarkdownDescription: "The directory path where terraform root module is located",
				Required:            true,
			},
			"org": schema.StringAttribute{
				MarkdownDescription: "Org name",
				Required:            true,
			},
			"local_workspace": schema.StringAttribute{
				MarkdownDescription: "Terraform community workspace name",
				Required:            true,
			},
			"tfc_workspace_id": schema.StringAttribute{
				MarkdownDescription: "Terraform cloud workspace id",
				Required:            false,
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
		tflog.Error(ctx, DIR_PATH_DOES_NOT_EXIST)
		resp.Diagnostics.AddError(DIR_PATH_DOES_NOT_EXIST, DIR_PATH_DOES_NOT_EXIST_DETAILED)
		return
	}
	tflog.Info(ctx, "Migrating state")
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

	log.Println("State ", string(state[:]))

	if tfeClient == nil {
		tfeClient, err = newTfeClient()
		if err != nil {
			tflog.Error(ctx, "Error initializing client", map[string]any{"error": err})
			resp.Diagnostics.AddError("Error initializing client ", err.Error())
			return
		}
	}
	workspaceId := data.TFCWorkspaceID.ValueString()
	workspace := data.TFCWorkspace.ValueString()
	if data.TFCWorkspaceID.IsNull() || data.TFCWorkspaceID.IsUnknown() {
		workspaceDetails, err := tfeClient.Workspaces.Read(ctx, data.Org.ValueString(), workspace)
		if err != nil {
			tflog.Error(ctx, "Error fetching workspace data "+workspace, map[string]any{"error": err})
			resp.Diagnostics.AddError("Error fetching workspace data "+workspace, err.Error())
		}
		workspaceId = workspaceDetails.ID
	}

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

}

func (r *stateMigration) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	tflog.Warn(ctx, DESTROY_ACTION_NOT_SUPPORTED)
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

func newTfeClient() (*tfe.Client, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	credsFilePath := strings.Join([]string{homeDir, TfcTokenPath}, "/") //nolint:typecheck
	credsJson, err := os.ReadFile(credsFilePath)
	if err != nil {
		return nil, err
	}

	var tfeCredentials TFECredentials
	if err := json.Unmarshal(credsJson, &tfeCredentials); err != nil {
		return nil, errors.New("failed to parse credential file" + err.Error())
	}

	tfcConfig := &tfe.Config{
		Address:           TfcAddress,
		Token:             tfeCredentials.Credentials.AppTerraformIo.Token,
		RetryServerErrors: true,
	}
	return tfe.NewClient(tfcConfig)
}

type TFECredentials struct {
	Credentials struct {
		AppTerraformIo struct {
			Token string `json:"token"`
		} `json:"app.terraform.io"`
	} `json:"credentials"`
}

type stateMeta struct {
	Serial  int64
	Lineage string
}
