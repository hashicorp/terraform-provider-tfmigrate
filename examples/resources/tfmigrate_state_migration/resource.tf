resource "tfmigrate_state_migration" "state-migration" {
  local_workspace = "default"
  tfc_workspace   = "default"
  directory_path  = "/Users/example/terraform/directory"
  org             = "Name-Of-HCP-Terraform-Organization"
}
