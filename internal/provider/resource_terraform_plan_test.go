package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

var (
	validPlanTestDir   = `./../../UnitTestWorkingDir`
	invalidPlanTestDir = `/Some/Invalid/Path`
)

func TestValidPlanResource(t *testing.T) {
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
		},
	})
}

func TestInvalidPlanResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      getPlanConfigsForDirPath(invalidPlanTestDir),
				ExpectError: regexp.MustCompile(`Error executing terraform init: Specified Dir Path doess not exist`),
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
