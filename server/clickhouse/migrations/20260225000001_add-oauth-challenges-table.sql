-- OAuth challenges tracking for billing and analytics.
-- Records each OAuth authorization flow (challenge) initiated via the DCR proxy.
-- Used for tracking usage per organization for enterprise billing.
CREATE TABLE IF NOT EXISTS `oauth_challenges` (
  `id` UUID DEFAULT generateUUIDv7(),
  `organization_id` String,
  `project_id` UUID,
  `user_id` String,
  `toolset_id` UUID,

  -- OAuth provider information
  `oauth_server_issuer` String,
  `provider_name` String,

  -- Challenge status: initiated, completed, failed, expired
  `status` LowCardinality(String) DEFAULT 'initiated',

  -- Error information (populated on failure)
  `error_code` Nullable(String),
  `error_description` Nullable(String),

  -- Timestamps
  `initiated_at` DateTime('UTC') DEFAULT now(),
  `completed_at` Nullable(DateTime('UTC'))
) ENGINE = MergeTree
PRIMARY KEY (`organization_id`, `initiated_at`, `id`)
PARTITION BY toYYYYMM(initiated_at)
ORDER BY (`organization_id`, `initiated_at`, `id`)
TTL initiated_at + INTERVAL 365 DAY;
