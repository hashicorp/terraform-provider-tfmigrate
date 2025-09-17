## 1.3.0-alpha20250917 (17th Sep 2025)

### NOTES
This is an alpha release for testing HCP Terraform Workpsaces to HCP Terraform Stacks migration support.


### Bugs and Security Fixes
- update terraform-migrate-utility and fixed issues with terraform state list command [PR](https://github.com/hashicorp/terraform-provider-tfmigrate/pull/219)

More details will come in the actual GA release.

## 1.3.0-alpha20250916 (16th Sep 2025)

### NOTES
This is an alpha release for testing HCP Terraform Workpsaces to HCP Terraform Stacks migration support.
- Added support for migrating HCP Terraform Workspaces to HCP Terraform Stacks. [PR](https://github.com/hashicorp/terraform-provider-tfmigrate/pull/193)
- Updated Go version to 1.24.6 [COMMIT](https://github.com/hashicorp/terraform-provider-tfmigrate/commit/3e0b328977e7ccc12ad5d3b4719b4c69d91b9fe0)
- Fixed security vulnerabilities and bumped dependencies.

More details will come in the actual GA release.

## 1.2.0 (19th Aug 2025)

### NOTES

**Bitbucket Integration Support:**

This release introduces full Bitbucket support in the terraform-provider-tfmigrate, extending compatibility alongside GitHub and GitLab. [PR](https://github.com/hashicorp/terraform-provider-tfmigrate/pull/176)

- Execute Git operations on Bitbucket-hosted repositories.
- Create pull requests with title, description, and source/destination branch configuration.  

**Token Validation:**

- Validates Bitbucket Repository Access Tokens.  
- Enforces required scopes:  
  - `repository:write` — for branch creation and pushes.  
  - `pullrequest:write` — for pull request creation.  
- If scope `pullrequest:write` is not enabled then the tool sets `create_pr` to false and skips pull request creation.
- Handles Bitbucket-specific token types and error responses.

## 1.1.0 (21st May 2025)

### NOTES

**VCS driven Workspaces:**

This provider now supports creation of VCS driven workspaces if CE workspace terraform configuration is hosted on any supported VCS.

**Granular Control Over Git Operations:**

Introduced two new provider-level attributes `allow_commit_push` and `create_pr` for granular control over git operations:

- `allow_commit_push`: Enables committing and pushing changes even if no migration branch is created. [PR](https://github.com/hashicorp/terraform-provider-tfmigrate/pull/125)
- `create_pr`: Allows users to automatically trigger a PR as part of the migration process. [PR](https://github.com/hashicorp/terraform-provider-tfmigrate/pull/125)

**Direct Git Control via Provider:**

These attributes give users granular control over Git behaviour from the provider itself, extending functionality even when Git operations are decoupled from the CLI.

**Token Validation Based on Git Actions:**

The provider now performs token validation **only if required**:

- If `create_pr` is `true`, the validation is done during the execute step.
- If Git operations are skipped entirely, no token validation is performed.

#### Bugs and Security Fixes

**Token Validation Fix:**

We have fixed one major bug related to git token used in terraform-provider-tfmigrate provider configuration.

- You can read more about the bug [here](https://github.com/hashicorp/terraform-provider-tfmigrate/issues/117).
- The fix is [here](https://github.com/hashicorp/terraform-provider-tfmigrate/pull/125).

**Security Fixes:**

- [GO-2025-3420](https://osv.dev/vulnerability/GO-2025-3420)
- [GO-2024-3107](https://osv.dev/vulnerability/GO-2024-3107)
- [GO-2024-3106](https://osv.dev/vulnerability/GO-2024-3106)
- [GO-2024-3105](https://osv.dev/vulnerability/GO-2024-3105)
- [GO-2024-2963](https://osv.dev/vulnerability/GO-2024-2963)
- [GO-2025-3373](https://osv.dev/vulnerability/GO-2025-3373)
- [GO-2025-3563](https://osv.dev/vulnerability/GO-2025-3563)

## 1.1.0-alpha20250520 (20th May 2025)

NOTES:

- Added `allow_commit_push` and `create_pr` attributes for Git control. [PR](https://github.com/hashicorp/terraform-provider-tfmigrate/pull/125)
- Token validation now conditional on Git usage. [PR](https://github.com/hashicorp/terraform-provider-tfmigrate/pull/125)
- Improved error handling and CLI alignment. [PR](https://github.com/hashicorp/terraform-provider-tfmigrate/pull/125)
- Fixed git_pat_token validation bug. [PR](https://github.com/hashicorp/terraform-provider-tfmigrate/pull/125)

More details will come in the actual GA release.

## 1.0.0 (27th February 2025)

NOTES:

- Gitlab support [PR](https://github.com/hashicorp/terraform-provider-tfmigrate/pull/53)
- Added generic name for git operations env variable - TF_GIT_PAT_TOKEN [PR](https://github.com/hashicorp/terraform-provider-tfmigrate/pull/53)
- This release contains functionality changes to validate pat token used by the customers for git operations [PR](https://github.com/hashicorp/terraform-provider-tfmigrate/pull/53)
- Fixed Git Token validation issue where the folder is a non-git repo and the user chooses to skip the git operations all together [PR](https://github.com/hashicorp/terraform-provider-tfmigrate/pull/81)
- It is released using new build and release Actions using CRT. [PR](https://github.com/hashicorp/terraform-provider-tfmigrate/pull/80)
- Bumped up dependecies.

## 1.0.0-alpha20250221 (21st February 2025)

NOTES:

- This release contains functionality changes to validate pat token used by the customers for git operations [PR](https://github.com/hashicorp/terraform-provider-tfmigrate/pull/53)
- Fixed Git Token validation issue where the folder is a non-git repo and the user chooses to skip the git operations all together [PR](https://github.com/hashicorp/terraform-provider-tfmigrate/pull/81)
- It is released using new build and release Actions using CRT. [PR](https://github.com/hashicorp/terraform-provider-tfmigrate/pull/80)

More details will come in the actual GA release.

## 0.1.3 (02 October 2024)

## What's Changed

- updated go.mod by @sujaysamanta in <https://github.com/hashicorp/terraform-provider-tfmigrate/pull/43>

## New Contributors

- @sujaysamanta made their first contribution in <https://github.com/hashicorp/terraform-provider-tfmigrate/pull/43>

**Full Changelog**: <https://github.com/hashicorp/terraform-provider-tfmigrate/compare/v0.2.0...v0.1.3>

## 0.0.1 (16th June 2024)

FEATURES:

- Publishing version 1 of the provider.
