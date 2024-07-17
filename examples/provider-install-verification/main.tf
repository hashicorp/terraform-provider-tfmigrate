terraform {
  required_providers {
    tfmigrate = {
      source = "hashicorp.com/terraform/tfmigrate"
    }
  }
}

provider "tfmigrate" {
  github_token = "token"
}




#
#resource "tfmigrate_state_migration" "test-state" {
#  directory_path = "/Users/jitendra/hashi-code/terraform-migration-spikes"
#  local_workspace      = "default"
##  tfc_workspace_id = "ws-HKrNcFuuLDpCMLkR"
#  tfc_workspace = "prod-ws"
#  orgId = "ajajaj"
#}

resource "tfmigrate_state_migration" "terraform-migration-spikes" {
  local_workspace = "default"
  tfc_workspace = "default-ws"
  directory_path = "/Users/jitendra/hashi-code/terraform-migration-spikes/multiple-workspaces"
  org            = "jitendra-org"
}


#resource "tfmigrate_directory_actions" "terraform-migration-spikes" {
#  org            = "jitendra-org"
#  project        = "migration"
#  directory_path = "/Users/jitendra/hashi-code/terraform-migration-spikes"
#  backend_file_name = "main.tf"
#  workspace_map = {
#    default = "terraform_spike_type_1_default"
#    default_2 = "terraform_spike_type_1_default_2"
#  }
#  tags               = ["terraform_spike_type_1"]
#  git_commit_msg = "[SKIP CI] migrated sample-project's default directory"
#}

#resource "tfmigrate_directory_actions" "terraform-migration-spikes" {
#  org            = var.organization_id
#  project        = data.tfe_project.project.name
#  directory_path = var.working_directory
#  backend_file = var.backend_file_name
#  workspace_map      = var.workspace_map
#  tags = var.tags
#  git_commit_msg = ""
#}