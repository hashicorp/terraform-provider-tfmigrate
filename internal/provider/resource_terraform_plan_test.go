package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

var (
	validPlanTestDir   = `./test-fixures/terraform-plan/`
	invalidPlanTestDir = `/Some/Invalid/Path`
)

func TestCreateUpdateOnPlanResource_Valid(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: getPlanConfigsForDirPath(validPlanTestDir),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tfmigrate_terraform_plan.test", "directory_path", validPlanTestDir),
					resource.TestCheckResourceAttr("tfmigrate_terraform_plan.test", "summary", "Add 1, Change 0, Remove 0"),
				),
			},
			{
				Config: getPlanConfigsForDirPath(invalidPlanTestDir),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tfmigrate_terraform_plan.test", "directory_path", invalidPlanTestDir),
					resource.TestCheckResourceAttr("tfmigrate_terraform_plan.test", "summary", UPDATE_ACTION_NOT_SUPPORTED),
				),
			},
		},
	})
}

func TestPathOnPlanResource_Invalid(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      getPlanConfigsForDirPath(invalidPlanTestDir),
				ExpectError: regexp.MustCompile(DIR_PATH_DOES_NOT_EXIST),
			},
		},
	})
}

func getPlanConfigsForDirPath(directory string) string {
	return fmt.Sprintf(providerConfig+`

resource "tfmigrate_terraform_init" "test" {
	directory_path = %[1]q
}


resource "tfmigrate_terraform_plan" "test" {
	depends_on = [tfmigrate_terraform_init.test]
	directory_path = %[1]q
}

`, directory)
}
