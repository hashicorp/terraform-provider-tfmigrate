resource "tfmigrate_github_pr" "sample_pr" {
  repo_identifier = "github_org/project_name"
  pr_title        = "Sample-PR-title"
  pr_body         = "Sample body of PR"
  source_branch   = "feature-branch-name"
  destin_branch   = "base-branch-name"
}
