resource "tfmigrate_stack_migration" "stack_migration" {
  config_file_dir      = "absolute/path/to/stack-config-directory"            # Provide absolute path to the directory containing stack config files
  organization         = "example-org"                                        # Replace with your TFE organization name under which the stack exists
  name                 = "example-stack"                                      # Replace with your TFE stack name, the must exist before migration and must be a non vcs stack
  project              = "example-project"                                    # Replace with your TFE project name under which the stack exists, we recommend creating a separate project for stack migrations other than the default project
  terraform_config_dir = "absolute/path/to/terraform-configuration-directory" # Provide absolute path to the directory containing terraform configuration files from which migration configuration is generated
  workspace_deployment_mapping = {                                            # Provide mapping of workspace names to deployment names, workspaces must exist before migration
    dev-workspace  = "dev-deployment"
    prod-workspace = "prod-deployment"
  }
}
