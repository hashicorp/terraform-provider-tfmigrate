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
  tfc_workspace_id = "ws-HKrNcFuuLDpCMLkR"
  tfc_workspace = "prod-ws"
}



resource "tfm_directory_actions" "terraform-migration-spikes" {
  directory_path = "/Users/jitendra/hashi-code/terraform-migration-spikes"
  org            = "jitendra-org"
  project        = "migration"
  workspace      = "default"
  git_commit_msg = "[SKIP CI] migrated sample-project's default directory"
}