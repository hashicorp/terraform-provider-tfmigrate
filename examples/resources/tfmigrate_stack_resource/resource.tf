# Example usage of tfmigrate_stack_migration
resource "tfmigrate_stack_migration" "example" {
  config_file_dir = "/absolute/path/to/config/files"
  name            = "example-stack"
  organization    = "example-organization"
  project         = "example-project"
}