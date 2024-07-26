resource "tfmigrate_update_backend" "update-backend" {
  org               = "HCP-Terraform-Organization"
  project           = "Project-Name"
  directory_path    = "/Users/examples/resources/terraform-configs"
  backend_file_name = "terraform.tf"
  workspace_map = {
    "default" = "default"
  }
  tags = ["project-name", "environment-name"]
}
