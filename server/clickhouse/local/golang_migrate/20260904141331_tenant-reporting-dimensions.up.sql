-- create "organization_metadata" table
CREATE TABLE `organization_metadata` (
  `id` String COMMENT 'Gram organization identifier.',
  `slug` String COMMENT 'Current Gram organization slug.',
  `account_type` LowCardinality(String) COMMENT 'Gram account type, sourced from Postgres gram_account_type.',
  `workos_id` Nullable(String) COMMENT 'WorkOS organization identifier used for cross-system reporting.',
  `workos_updated_at` Nullable(DateTime64(6, 'UTC')) COMMENT 'Timestamp of the latest applied WorkOS organization update.',
  `webhooks_enabled` Nullable(Bool) COMMENT 'Whether outbound organization webhooks are enabled. Null means not recorded.',
  `scim_enabled` Nullable(Bool) COMMENT 'Whether SCIM directory sync is enabled. Null means not recorded.',
  `sso_enabled` Nullable(Bool) COMMENT 'Whether SSO is enabled. Null means not recorded.',
  `whitelisted` Bool COMMENT 'Whether the organization is allowed to use Gram.',
  `free_trial_started_at` DateTime64(6, 'UTC') COMMENT 'Start of the organization metadata free-trial window.',
  `free_trial_ends_at` DateTime64(6, 'UTC') COMMENT 'End of the organization metadata free-trial window.',
  `trial_tier` Nullable(String) COMMENT 'Enterprise trial tier from the trials lifecycle table.',
  `trial_ends_at` Nullable(DateTime64(6, 'UTC')) COMMENT 'Current enterprise trial end time.',
  `trial_converted_at` Nullable(DateTime64(6, 'UTC')) COMMENT 'When the enterprise trial converted.',
  `trial_demoted_at` Nullable(DateTime64(6, 'UTC')) COMMENT 'When the enterprise trial was demoted.',
  `trial_created_at` Nullable(DateTime64(6, 'UTC')) COMMENT 'When the enterprise trial lifecycle was created.',
  `trial_updated_at` Nullable(DateTime64(6, 'UTC')) COMMENT 'When the enterprise trial lifecycle was last updated.',
  `created_at` DateTime64(6, 'UTC') COMMENT 'When the organization was created in Gram.',
  `updated_at` DateTime64(6, 'UTC') COMMENT 'When the organization metadata was last updated in Gram.',
  `disabled_at` Nullable(DateTime64(6, 'UTC')) COMMENT 'When the organization was disabled.'
) ENGINE = MergeTree
PRIMARY KEY (`id`) ORDER BY (`id`) SETTINGS index_granularity = 8192 COMMENT 'Current organization reporting dimensions synced from Postgres.';
-- create "organization_metadata_staging" table
CREATE TABLE `organization_metadata_staging` (
  `id` String COMMENT 'Gram organization identifier.',
  `slug` String COMMENT 'Current Gram organization slug.',
  `account_type` LowCardinality(String) COMMENT 'Gram account type, sourced from Postgres gram_account_type.',
  `workos_id` Nullable(String) COMMENT 'WorkOS organization identifier used for cross-system reporting.',
  `workos_updated_at` Nullable(DateTime64(6, 'UTC')) COMMENT 'Timestamp of the latest applied WorkOS organization update.',
  `webhooks_enabled` Nullable(Bool) COMMENT 'Whether outbound organization webhooks are enabled. Null means not recorded.',
  `scim_enabled` Nullable(Bool) COMMENT 'Whether SCIM directory sync is enabled. Null means not recorded.',
  `sso_enabled` Nullable(Bool) COMMENT 'Whether SSO is enabled. Null means not recorded.',
  `whitelisted` Bool COMMENT 'Whether the organization is allowed to use Gram.',
  `free_trial_started_at` DateTime64(6, 'UTC') COMMENT 'Start of the organization metadata free-trial window.',
  `free_trial_ends_at` DateTime64(6, 'UTC') COMMENT 'End of the organization metadata free-trial window.',
  `trial_tier` Nullable(String) COMMENT 'Enterprise trial tier from the trials lifecycle table.',
  `trial_ends_at` Nullable(DateTime64(6, 'UTC')) COMMENT 'Current enterprise trial end time.',
  `trial_converted_at` Nullable(DateTime64(6, 'UTC')) COMMENT 'When the enterprise trial converted.',
  `trial_demoted_at` Nullable(DateTime64(6, 'UTC')) COMMENT 'When the enterprise trial was demoted.',
  `trial_created_at` Nullable(DateTime64(6, 'UTC')) COMMENT 'When the enterprise trial lifecycle was created.',
  `trial_updated_at` Nullable(DateTime64(6, 'UTC')) COMMENT 'When the enterprise trial lifecycle was last updated.',
  `created_at` DateTime64(6, 'UTC') COMMENT 'When the organization was created in Gram.',
  `updated_at` DateTime64(6, 'UTC') COMMENT 'When the organization metadata was last updated in Gram.',
  `disabled_at` Nullable(DateTime64(6, 'UTC')) COMMENT 'When the organization was disabled.'
) ENGINE = MergeTree
PRIMARY KEY (`id`) ORDER BY (`id`) SETTINGS index_granularity = 8192 COMMENT 'Staging generation for the organization reporting dimensions.';
-- create "projects" table
CREATE TABLE `projects` (
  `id` UUID COMMENT 'Gram project identifier.',
  `organization_id` String COMMENT 'Organization that owns the project.',
  `slug` String COMMENT 'Current project slug within its organization.',
  `created_at` DateTime64(6, 'UTC') COMMENT 'When the project was created.',
  `updated_at` DateTime64(6, 'UTC') COMMENT 'When the project was last updated.',
  `deleted_at` Nullable(DateTime64(6, 'UTC')) COMMENT 'When the project was soft-deleted.'
) ENGINE = MergeTree
PRIMARY KEY (`organization_id`, `id`) ORDER BY (`organization_id`, `id`) SETTINGS index_granularity = 8192 COMMENT 'Current project reporting dimensions synced from Postgres.';
-- create "projects_staging" table
CREATE TABLE `projects_staging` (
  `id` UUID COMMENT 'Gram project identifier.',
  `organization_id` String COMMENT 'Organization that owns the project.',
  `slug` String COMMENT 'Current project slug within its organization.',
  `created_at` DateTime64(6, 'UTC') COMMENT 'When the project was created.',
  `updated_at` DateTime64(6, 'UTC') COMMENT 'When the project was last updated.',
  `deleted_at` Nullable(DateTime64(6, 'UTC')) COMMENT 'When the project was soft-deleted.'
) ENGINE = MergeTree
PRIMARY KEY (`organization_id`, `id`) ORDER BY (`organization_id`, `id`) SETTINGS index_granularity = 8192 COMMENT 'Staging generation for the project reporting dimensions.';
