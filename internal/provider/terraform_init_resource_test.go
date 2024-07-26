package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

var (
	validInitTestDir   = `./test-fixures/terraform-init/`
	invalidInitTestDir = `/Some/Invalid/Path`
)

func TestCreateUpdateOnInitResource_Valid(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: getInitConfigsForDirPath(validInitTestDir),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tfmigrate_terraform_init.test", "directory_path", validInitTestDir),
					resource.TestCheckResourceAttr("tfmigrate_terraform_init.test", "summary", TERRAFORM_INIT_SUCCESS),
				),
			},
			{
				Config: getInitConfigsForDirPath(invalidInitTestDir),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tfmigrate_terraform_init.test", "directory_path", invalidInitTestDir),
					resource.TestCheckResourceAttr("tfmigrate_terraform_init.test", "summary", UPDATE_ACTION_NOT_SUPPORTED),
				),
			},
		},
	})
}

func TestPathOnInitResource_Invalid(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      getInitConfigsForDirPath(invalidInitTestDir),
				ExpectError: regexp.MustCompile(DIR_PATH_DOES_NOT_EXIST),
			},
		},
	})
}

func getInitConfigsForDirPath(directory string) string {
	return fmt.Sprintf(providerConfig+`
resource "tfmigrate_terraform_init" "test" {
	directory_path = %[1]q
}
`, directory)
}
