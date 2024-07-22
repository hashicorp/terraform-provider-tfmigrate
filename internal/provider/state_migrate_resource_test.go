package provider

import (
	"bytes"
	"fmt"
	"os/exec"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestStateMigrateResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: getStateMigrationConfig(),
				PreConfig: func() {
					var buffer, errBuffer bytes.Buffer

					cmd := exec.Command("terraform", "init", "-json")
					cmd.Dir = "./test-fixures/terraform-init"
					cmd.Stdout = &buffer
					cmd.Stderr = &errBuffer
					err := cmd.Run()
					if err != nil {
						bufErr := errBuffer.String()
						fmt.Println(bufErr)
						panic(errBuffer.String())
					}
					var buffer1, errBuffer1 bytes.Buffer
					cmd = exec.Command("terraform", "apply", "-auto-approve", "-json")
					cmd.Stdout = &buffer1
					cmd.Stderr = &errBuffer1
					cmd.Dir = "./test-fixures/terraform-init"
					err = cmd.Run()
					if err != nil {
						bufErr := errBuffer1.String()
						fmt.Println(bufErr)
						panic(errBuffer.String())
					}

				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tfmigrate_state_migration.state-migration", "local_workspace", "default"),
				),
			},
		},
	})
}

func getStateMigrationConfig() string {

	return fmt.Sprintf(providerConfig + `

resource "tfmigrate_state_migration" "state-migration" {
  local_workspace = "default"
  tfc_workspace   = "test-workspace"
  directory_path  = "./test-fixures/terraform-init"
  org             = "absl"
}

`)

}
