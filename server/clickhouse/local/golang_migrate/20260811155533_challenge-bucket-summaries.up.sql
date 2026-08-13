-- create "authz_challenge_bucket_summaries" table
CREATE TABLE `authz_challenge_bucket_summaries` (
  `challenge_date` Date COMMENT 'UTC date of the summarized challenge rows.',
  `organization_id` String COMMENT 'Organization the principal was acting in.' CODEC(ZSTD(1)),
  `project_id` String COMMENT 'Project the check was scoped to.' CODEC(ZSTD(1)),
  `outcome` LowCardinality(String) COMMENT 'Decision outcome.',
  `scope` LowCardinality(String) COMMENT 'Scope of the focus check.',
  `principal_urn` String COMMENT 'Primary principal URN.' CODEC(ZSTD(1)),
  `user_id_filter` String COMMENT 'User ID used to suppress principals outside the organization.' CODEC(ZSTD(1)),
  `resource_kind` LowCardinality(String) COMMENT 'Resource kind of the focus check.',
  `resource_id` String COMMENT 'Resource ID of the focus check.' CODEC(ZSTD(1)),
  `representative_id` AggregateFunction(argMax, UUID, DateTime64(9)),
  `principal_type` AggregateFunction(argMax, String, DateTime64(9)),
  `user_id` AggregateFunction(argMax, Nullable(String), DateTime64(9)),
  `user_email` AggregateFunction(argMax, Nullable(String), DateTime64(9)),
  `operation` AggregateFunction(argMax, String, DateTime64(9)),
  `reason` AggregateFunction(argMax, String, DateTime64(9)),
  `role_slugs` AggregateFunction(argMax, Array(String), DateTime64(9)),
  `evaluated_grant_count` AggregateFunction(argMax, UInt32, DateTime64(9)),
  `matched_grant_count` SimpleAggregateFunction(max, UInt64),
  `challenge_ids` AggregateFunction(groupUniqArray, UUID),
  `first_seen` SimpleAggregateFunction(min, DateTime64(9)),
  `last_seen` SimpleAggregateFunction(max, DateTime64(9))
) ENGINE = AggregatingMergeTree
PRIMARY KEY (`organization_id`, `project_id`, `outcome`, `scope`, `challenge_date`, `principal_urn`, `user_id_filter`, `resource_kind`, `resource_id`) ORDER BY (`organization_id`, `project_id`, `outcome`, `scope`, `challenge_date`, `principal_urn`, `user_id_filter`, `resource_kind`, `resource_id`) PARTITION BY (toYYYYMM(challenge_date)) TTL challenge_date + toIntervalDay(90) SETTINGS index_granularity = 8192 COMMENT 'Daily pre-aggregated authz challenge buckets for the Challenge UI';
-- create "authz_challenge_bucket_summaries_mv" view
CREATE MATERIALIZED VIEW `authz_challenge_bucket_summaries_mv` TO `authz_challenge_bucket_summaries` AS SELECT toDate(timestamp, 'UTC') AS challenge_date, organization_id, project_id, outcome, scope, principal_urn, ifNull(authz_challenges.user_id, '') AS user_id_filter, resource_kind, resource_id, argMaxState(id, timestamp) AS representative_id, argMaxState(toString(principal_type), timestamp) AS principal_type, argMaxState(authz_challenges.user_id, timestamp) AS user_id, argMaxState(authz_challenges.user_email, timestamp) AS user_email, argMaxState(toString(operation), timestamp) AS operation, argMaxState(toString(reason), timestamp) AS reason, argMaxState(arrayMap(x -> toString(x), role_slugs), timestamp) AS role_slugs, argMaxState(evaluated_grant_count, timestamp) AS evaluated_grant_count, max(toUInt64(length(matched_grants.scope))) AS matched_grant_count, groupUniqArrayState(id) AS challenge_ids, min(timestamp) AS first_seen, max(timestamp) AS last_seen FROM authz_challenges WHERE (scope != '') AND (resource_kind != '') AND (resource_id != '') GROUP BY challenge_date, organization_id, project_id, outcome, scope, principal_urn, user_id_filter, resource_kind, resource_id;
