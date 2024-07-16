terraform {
  required_providers {
    tfmigrate = {
      source = "hashicorp.com/edu/tfmigrate"
    }
  }
}

provider "tfmigrate" {
  github_token = "token"
}





resource "tfmigrate_state_migration" "test-state" {
  directory_path = "/Users/jitendra/hashi-code/terraform-migration-spikes"
  local_workspace      = "default"
#  tfc_workspace_id = "ws-HKrNcFuuLDpCMLkR"
  tfc_workspace = "prod-ws"
  orgId = "ajajaj"
}



resource "tfm_directory_actions" "terraform-migration-spikes" {
  directory_path = "/Users/jitendra/hashi-code/terraform-migration-spikes"
  backend_file = "main.tf"
  org            = "jitendra-org"
  project        = "migration"
  workspace_map = {'local':'tfc'}
  tag = "common_tag"
  git_commit_msg = "[SKIP CI] migrated sample-project's default directory"
}