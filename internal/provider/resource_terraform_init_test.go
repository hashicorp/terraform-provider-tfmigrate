package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

var (
	validInitTestDir   = `./../../UnitTestWorkingDir`
	invalidInitTestDir = `/Some/Invalid/Path`
)

func TestValidInitResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: getInitConfigsForDirPath(validInitTestDir),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tfmigrate_terraform_init.test", "directory_path", validInitTestDir),
					resource.TestCheckResourceAttr("tfmigrate_terraform_init.test", "summary", "Terraform init completed"),
				),
			},
		},
	})
}

func TestInvalidInitResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      getInitConfigsForDirPath(invalidInitTestDir),
				ExpectError: regexp.MustCompile(`Error executing terraform init: Specified Dir Path doess not exist`),
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
