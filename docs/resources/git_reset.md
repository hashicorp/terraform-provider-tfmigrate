---
# generated by https://github.com/hashicorp/terraform-plugin-docs
page_title: "tfmigrate_git_reset Resource - tfmigrate"
subcategory: ""
description: |-
  Git Reset Resource: This resource is used to execute git reset command in the said directory.
---

# tfmigrate_git_reset (Resource)

Git Reset Resource: This resource is used to execute git reset command in the said directory.

## Example Usage

```terraform
resource "tfmigrate_git_reset" "reset" {
  directory_path = "/path/to/a/git/repo"
}
```

<!-- schema generated by tfplugindocs -->
## Schema

### Required

- `directory_path` (String) The directory path where git reset needs to be executed.
