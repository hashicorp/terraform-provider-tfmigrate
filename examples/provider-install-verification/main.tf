terraform {
  required_providers {
    tfm = {
      source = "hashicorp.com/edu/tfm"
    }
  }
}
provider "tfm" {
  github_token = "token"
}


resource "tfm_directory_actions" "terraform-migration-spikes" {
  directory_path = "/Users/jitendra/hashi-code/terraform-migration-spikes"
  org = "jitendra-org"
  project = "migration"
  workspace = "default"
  git_commit_msg = "[SKIP CI] migrated sample-project's default directory"
}