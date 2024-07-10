
resource "tfmigrate_git_commit_push" "create_commit" {
  directory_path = "/path/to/directory"
  commit_message = "This is a sample Commit message"
  branch_name    = "feature-branch-name"
  remote_name    = "origin"
  enable_push    = true
}
