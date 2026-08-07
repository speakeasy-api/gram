-- create "skill_session_versions" table
CREATE TABLE `skill_session_versions` (
  `id` UUID COMMENT 'Source observation identifier.',
  `created_at` DateTime64(9) COMMENT 'Time the mapping was created.' CODEC(DoubleDelta, ZSTD(1)),
  `inserted_at` DateTime64(9) DEFAULT now64(9) COMMENT 'Time the mapping was inserted.' CODEC(DoubleDelta, ZSTD(1)),
  `seen_at` DateTime64(9) COMMENT 'Time the skill version became active in the session.' CODEC(DoubleDelta, ZSTD(1)),
  `organization_id` String COMMENT 'Organization the session belongs to.' CODEC(ZSTD(1)),
  `project_id` UUID COMMENT 'Project the session belongs to.',
  `session_id` String COMMENT 'Session identifier.' CODEC(ZSTD(1)),
  `skill_id` UUID COMMENT 'Observed skill identifier.',
  `skill_version_id` UUID COMMENT 'Observed skill version identifier.',
  `canonical_sha256` String COMMENT 'Canonical SHA-256 digest of the observed skill.' CODEC(ZSTD(1)),
  `surface` LowCardinality(String) COMMENT 'Observation surface: dev | assistant.'
) ENGINE = MergeTree
PRIMARY KEY (`project_id`, `session_id`, `skill_version_id`, `seen_at`, `id`) ORDER BY (`project_id`, `session_id`, `skill_version_id`, `seen_at`, `id`) PARTITION BY (toYYYYMM(seen_at)) TTL toDateTime(seen_at) + toIntervalDay(730) SETTINGS index_granularity = 8192 COMMENT 'Resolved skill versions observed in sessions.';
