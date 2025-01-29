// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

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
					resource.TestCheckResourceAttr("tfmigrate_terraform_init.test", "summary", TerraformInitSuccess),
				),
			},
			{
				Config: getInitConfigsForDirPath(invalidInitTestDir),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tfmigrate_terraform_init.test", "directory_path", invalidInitTestDir),
					resource.TestCheckResourceAttr("tfmigrate_terraform_init.test", "summary", UpdateActionNotSupported),
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
				ExpectError: regexp.MustCompile(DirPathDoesNotExist),
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
