package provider

import (
	"context"
	"fmt"
	"terraform-provider-tfmigrate/internal/terraform"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

type terraformPlan struct {
}

var (
	_ resource.Resource = &terraformPlan{}
)

func NewTerraformPlanResource() resource.Resource {
	return &terraformPlan{}
}

type TerraformPlanModel struct {
	DirectoryPath types.String `tfsdk:"directory_path"`
	Summary       types.String `tfsdk:"summary"`
}

func (r *terraformPlan) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_terraform_plan"
}

func (r *terraformPlan) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Terraform Init Resource: This resource is used to execute terraform plan command in the said directory.",
		Attributes: map[string]schema.Attribute{
			"directory_path": schema.StringAttribute{
				MarkdownDescription: "The directory path where terraform init needs to be executed.",
				Required:            true,
			},
			"summary": schema.StringAttribute{
				MarkdownDescription: "Capture the summary of the terraform plan executed at the target directory.",
				Computed:            true,
			},
		},
	}
}

func (r *terraformPlan) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {

	var data TerraformPlanModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	dirPath := data.DirectoryPath.ValueString()
	terraformOperation := &terraform.TerraformOperation{
		DirectoryPath: dirPath,
	}
	tflog.Info(ctx, "Executing terraform plan")
	summary, err := terraformOperation.ExecuteTerraformPlan(ctx)
	if err != nil {
		tflog.Error(ctx, "Error executing terraform plan: "+err.Error())
		resp.Diagnostics.AddError("Error executing terraform plan:", err.Error())
		data.Summary = types.StringValue(TERRAFORM_PLAN_FAILED)
	} else {
		result_string := fmt.Sprintf(TERRAFORM_PLAN_SUCCESS, summary.Add, summary.Change, summary.Remove)
		tflog.Info(ctx, "Terraform Plan Summary:"+result_string)
		data.Summary = types.StringValue(result_string)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *terraformPlan) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
}

func (r *terraformPlan) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data TerraformPlanModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.AddWarning(UPDATE_ACTION_NOT_SUPPORTED, UPDATE_ACTION_NOT_SUPPORTED_DETAILED)
	data.Summary = types.StringValue(UPDATE_ACTION_NOT_SUPPORTED)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *terraformPlan) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
}
