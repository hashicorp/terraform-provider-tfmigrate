package provider

import (
	"context"
	"os"
	"terraform-provider-tfmigrate/internal/terraform"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

type terraformInit struct {
}

var (
	_ resource.Resource = &terraformInit{}
)

func NewTerraformInitResource() resource.Resource {
	return &terraformInit{}
}

type TerraformInitModel struct {
	DirectoryPath types.String `tfsdk:"directory_path"`
	Summary       types.String `tfsdk:"summary"`
}

func (r *terraformInit) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_terraform_init"
}

func (r *terraformInit) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "TFM Migrate directory action resource",
		Attributes: map[string]schema.Attribute{
			"directory_path": schema.StringAttribute{
				MarkdownDescription: "directory_path",
				Required:            true,
			},
			"summary": schema.StringAttribute{
				MarkdownDescription: "summary",
				Computed:            true,
			},
		},
	}
}

func (r *terraformInit) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {

	var data TerraformInitModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	dirPath := data.DirectoryPath.ValueString()
	terraformOperation := &terraform.TerraformOperation{
		DirectoryPath: dirPath,
	}
	_, err := os.Stat(dirPath)
	if err != nil {
		tflog.Error(ctx, "Error executing terraform init: Specified Dir Path doess not exist")
		resp.Diagnostics.AddError("Error executing terraform init: Specified Dir Path doess not exist", "")
		data.Summary = types.StringValue("INIT FAILED")
	}

	tflog.Info(ctx, "Executing terraform init")
	err = terraformOperation.ExecuteTerraformInit(ctx)
	if err != nil {
		tflog.Error(ctx, "Error executing terraform init: "+err.Error())
		resp.Diagnostics.AddError("Error executing terraform init:", err.Error())
		data.Summary = types.StringValue("INIT FAILED")
	} else {
		data.Summary = types.StringValue("Terraform init completed")
		tflog.Trace(ctx, "Terraform init completed")
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *terraformInit) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
}

func (r *terraformInit) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	tflog.Warn(ctx, "Update in the configs detected, But this resource does not support update operation.")
}

func (r *terraformInit) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	tflog.Warn(ctx, "Destroy in the configs detected, But this resource does not support destroy operation.")
}
