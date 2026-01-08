locals {
  stack_deployment_details = (
    tfmigrate_stack_migration.stack_migration.migration_hash != "" ?
    provider::tfmigrate::decode_stacks_migration_hash_to_json(
      tfmigrate_stack_migration.stack_migration.migration_hash
    ) : null
  )
}

output "stack_deployment_details" {
  value = local.stack_deployment_details
}