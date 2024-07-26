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


resource "tfmigrate_state_migration" "terraform-migration-spikes" {
  local_workspace = "default"
  tfc_workspace   = "default-ws"
  directory_path  = "/Users/jitendra/hashi-code/terraform-migration-spikes/multiple-workspaces"
  org             = "jitendra-org"
}


resource "tfmigrate_directory_actions" "terraform-migration-spikes" {
  org               = "jitendra-org"
  project           = "migration"
  directory_path    = "/Users/jitendra/hashi-code/terraform-migration-spikes"
  backend_file_name = "main.tf"
  workspace_map = {
    default   = "terraform_spike_type_1_default"
    default_2 = "terraform_spike_type_1_default_2"
  }
  tags           = ["terraform_spike_type_1"]
  git_commit_msg = "[SKIP CI] migrated sample-project's default directory"
}