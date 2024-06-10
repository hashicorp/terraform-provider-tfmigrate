package provider

import (
	"context"
	"strconv"
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
		data.Summary = types.StringValue("PLAN FAILED")
	} else {
		result_string := "Add " + strconv.Itoa(summary.Add) + ", Change " + strconv.Itoa(summary.Change) + ", Remove " + strconv.Itoa(summary.Remove)
		tflog.Info(ctx, "\n\n\n Terraform Plan Summary:"+result_string)
		data.Summary = types.StringValue(result_string)
		tflog.Trace(ctx, "Terraform plan completed")
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *terraformPlan) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
}

func (r *terraformPlan) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
}

func (r *terraformPlan) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
}
