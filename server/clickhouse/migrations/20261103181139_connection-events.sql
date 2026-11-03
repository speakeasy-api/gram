-- Create "mcp_connection_attempt_summaries" table
CREATE TABLE `gram`.`mcp_connection_attempt_summaries` (
  `organization_id` String COMMENT 'Organization the attempt belongs to. Readers always filter on it.
@access: confidential',
  `project_id` UUID COMMENT 'Project the attempt belongs to, nil when it belongs to none, such as an organization gateway or an identity provider renewal. Must be the same on every step of an attempt. Readers always filter on organization_id.
@access: confidential',
  `server_kind` LowCardinality(String) COMMENT 'Kind of server the attempt reached, such as mcp or meta, or provider_session or identity_provider when it reached no server.
@access: confidential',
  `server_id` String COMMENT 'ID of the MCP server or gateway the attempt reached, empty when it reached none.
@access: confidential',
  `bucket` DateTime('UTC') COMMENT 'UTC 30-minute bucket containing the start of the attempt, not the time of the step.
@access: confidential',
  `step` LowCardinality(String) COMMENT 'Step of the attempt. Step counts are only comparable within one step.
@access: confidential',
  `step_result` LowCardinality(String) COMMENT 'How the step ended.
@access: confidential',
  `outcome` LowCardinality(String) COMMENT 'Overall result of the attempt on final steps, empty on other steps.
@access: confidential',
  `reason` LowCardinality(String) COMMENT 'Reason code for the step result, empty for success.
@access: confidential',
  `who_fixes` LowCardinality(String) COMMENT 'Party that has to act: person, admin, app, provider or speakeasy.
@access: confidential',
  `grant_type` LowCardinality(String) COMMENT 'OAuth grant type for token steps, empty otherwise.
@access: confidential',
  `member_id` String COMMENT 'Gateway member MCP server ID for member-level steps, empty otherwise.
@access: confidential',
  `provider_host` String COMMENT 'Normalized issuer host of the provider. A tenant host can name the customer.
@access: restricted',
  `app_key` String COMMENT 'Host of the client metadata URL of the app, or the client kind for apps without one. A host can name the customer.
@access: restricted',
  `client_family` LowCardinality(String) COMMENT 'Kind of client from the User-Agent, from a fixed list, otherwise other. Empty when no client made the request.
@access: confidential',
  `step_events` SimpleAggregateFunction(sum, UInt64) COMMENT 'Step rows observed. An attempt can count more than once, so this is not a number of attempts.
@access: confidential',
  `attempts_seen` AggregateFunction(uniqExact, UUID) COMMENT 'Distinct attempts with a step here. Abandoned is seen minus ended, exact only per org, project, server and bucket. Per provider, step or person comes from mcp_connection_events at attempt level, for 30 days. A browser attempt can stay live about 60 minutes because each store restarts the 10-minute clock and the code lasts 10 more, so readers wait at least 120 minutes past the bucket start before calling an attempt abandoned.
@access: confidential',
  `attempts_ended` AggregateFunction(uniqExact, UUID) COMMENT 'Distinct attempts with a final step here, however late it arrived. See attempts_seen for how to read abandoned. A lost final row reads as abandoned.
@access: confidential',
  `retries` SimpleAggregateFunction(sum, UInt64) COMMENT 'Retries spent across these steps.
@access: confidential',
  `repeats` SimpleAggregateFunction(sum, UInt64) COMMENT 'Rows the limiter held back, which these rows stand for. Failures for a key are attempts plus repeats.
@access: confidential',
  `recovered` SimpleAggregateFunction(sum, UInt64) COMMENT 'Gateway members listed only after at least one retry.
@access: confidential',
  `first_seen` SimpleAggregateFunction(min, DateTime64(9, 'UTC')) COMMENT 'Time of the earliest step.
@access: confidential',
  `last_seen` SimpleAggregateFunction(max, DateTime64(9, 'UTC')) COMMENT 'Time of the latest step.
@access: confidential'
) ENGINE = AggregatingMergeTree
PRIMARY KEY (`organization_id`, `project_id`, `server_kind`, `server_id`, `bucket`) ORDER BY (`organization_id`, `project_id`, `server_kind`, `server_id`, `bucket`, `step`, `step_result`, `outcome`, `reason`, `who_fixes`, `grant_type`, `member_id`, `provider_host`, `app_key`, `client_family`) PARTITION BY (toMonday(bucket)) TTL bucket + toIntervalDay(90) SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1 COMMENT 'MCP connection attempts per 30-minute bucket of attempt start, for rates and trends. Filled by a view on mcp_connection_events. Abandoned is exact only per org, project, server and bucket. Kept 90 days.';
-- Create "mcp_connection_events" table
CREATE TABLE `gram`.`mcp_connection_events` (
  `organization_id` String COMMENT 'Organization the attempt belongs to. Readers always filter on it.
@access: confidential' CODEC(ZSTD(1)),
  `project_id` UUID COMMENT 'Project the attempt belongs to, nil when it belongs to none, such as an organization gateway or an identity provider renewal. Must be the same on every step of an attempt. Readers always filter on organization_id.
@access: confidential',
  `attempt_id` UUID COMMENT 'Shared by every step of one connection attempt.
@access: confidential',
  `attempt_started_at` DateTime64(9, 'UTC') COMMENT 'Time the attempt started, the same on every step. Partitioning, sort order, retention and rollup buckets all use it, so an attempt stays together.
@access: confidential' CODEC(DoubleDelta, ZSTD(1)),
  `event_time` DateTime64(9, 'UTC') COMMENT 'Time this step ended.
@access: confidential' CODEC(DoubleDelta, ZSTD(1)),
  `server_kind` LowCardinality(String) COMMENT 'Kind of server the attempt reached, such as mcp or meta, or provider_session or identity_provider when it reached no server.
@access: confidential',
  `server_id` String COMMENT 'ID of the MCP server or gateway the attempt reached, empty when it reached none. Must be the same on every step of an attempt.
@access: confidential' CODEC(ZSTD(1)),
  `member_id` String COMMENT 'Gateway member MCP server ID for member-level steps, empty otherwise.
@access: confidential' CODEC(ZSTD(1)),
  `step` LowCardinality(String) COMMENT 'Step this row records, such as discovery_member.
@access: confidential',
  `step_result` LowCardinality(String) COMMENT 'How this step ended, such as listed or needs_sign_in.
@access: confidential',
  `outcome` LowCardinality(String) COMMENT 'Overall result of the attempt, set on its final step only: connected, failed, blocked or declined. Abandoned is never stored, it is worked out when read.
@access: confidential',
  `reason` LowCardinality(String) COMMENT 'Reason code for the step result, empty for success.
@access: confidential',
  `sub_reason` LowCardinality(String) COMMENT 'Finer label within the reason, such as org_lookup_failed for a gate, empty otherwise.
@access: confidential',
  `who_fixes` LowCardinality(String) COMMENT 'Party that has to act: person, admin, app, provider or speakeasy. Empty when nothing needs fixing.
@access: confidential',
  `grant_type` LowCardinality(String) COMMENT 'OAuth grant type for token steps, empty otherwise.
@access: confidential',
  `is_final` Bool COMMENT 'True on the row that ends the attempt. An attempt with no such row is unfinished or abandoned.
@access: confidential',
  `retries` UInt32 COMMENT 'Retries spent within this step.
@access: confidential',
  `auth_client_id` String COMMENT 'ID in mcp_auth_clients, empty when none applies.
@access: confidential' CODEC(ZSTD(1)),
  `remote_session_issuer_id` String COMMENT 'ID in remote_session_issuers of the upstream provider, empty when none applies.
@access: confidential' CODEC(ZSTD(1)),
  `user_session_issuer_id` String COMMENT 'ID in user_session_issuers of the Speakeasy issuer, empty when none applies.
@access: confidential' CODEC(ZSTD(1)),
  `user_session_client_id` String COMMENT 'ID in user_session_clients of the AI app, empty when none applies.
@access: confidential' CODEC(ZSTD(1)),
  `remote_session_client_id` String COMMENT 'ID in remote_session_clients, empty when none applies.
@access: confidential' CODEC(ZSTD(1)),
  `subject_urn` String COMMENT 'Session subject URN of the user, API key, agent or workload, empty before sign-in.
@access: confidential-pii' CODEC(ZSTD(1)),
  `user_session_id` String COMMENT 'ID in user_sessions, empty when none applies.
@access: confidential' CODEC(ZSTD(1)),
  `remote_session_id` String COMMENT 'ID in remote_sessions, empty when none applies.
@access: confidential' CODEC(ZSTD(1)),
  `trace_id` String COMMENT 'Trace ID of the request that recorded this step.
@access: confidential' CODEC(ZSTD(1)),
  `provider_host` String COMMENT 'Normalized issuer host of the provider, shared across orgs. A tenant host can name the customer.
@access: restricted',
  `app_key` String COMMENT 'Host of the client metadata URL of the app, or the client kind for apps without one. A host can name the customer.
@access: restricted',
  `client_family` LowCardinality(String) COMMENT 'Kind of client from the User-Agent, from a fixed list, otherwise other. Empty when no client made the request.
@access: confidential',
  `client_version` LowCardinality(String) COMMENT 'Client version from the User-Agent when it looks like a version, otherwise empty.
@access: confidential',
  `repeats` UInt32 COMMENT 'Rows like this one that the limiter held back, which this row stands for. Failures for a key are attempts plus repeats.
@access: confidential',
  `device_key` String COMMENT 'Keyed hash of the client IP, unique per organization. Cannot be reversed without the key. Identifies a device or network, not a person. Kept 30 days.
@access: confidential-pii' CODEC(ZSTD(1)),
  INDEX `idx_mcp_connection_events_attempt_id` ((attempt_id)) TYPE bloom_filter(0.01),
  INDEX `idx_mcp_connection_events_device_key` ((device_key)) TYPE bloom_filter(0.01),
  INDEX `idx_mcp_connection_events_subject_urn` ((subject_urn)) TYPE bloom_filter(0.01)
) ENGINE = MergeTree
PRIMARY KEY (`organization_id`, `project_id`, `server_kind`, `server_id`, `attempt_started_at`, `attempt_id`, `event_time`) ORDER BY (`organization_id`, `project_id`, `server_kind`, `server_id`, `attempt_started_at`, `attempt_id`, `event_time`) PARTITION BY (toDate(attempt_started_at)) TTL toDateTime(attempt_started_at) + toIntervalDay(30) SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1 COMMENT 'One row per step of an MCP connection attempt, successes included. Used for drill-in on one attempt, person or server. Rows are never updated. Kept 30 days.';
-- Create "mcp_connection_outcomes_daily" table
CREATE TABLE `gram`.`mcp_connection_outcomes_daily` (
  `day` Date COMMENT 'UTC day the attempts started.
@access: confidential',
  `provider` LowCardinality(String) COMMENT 'Known vendor, otherwise other. Only the daily job enforces that a value cannot name a customer.
@access: opaque-confidential',
  `app` LowCardinality(String) COMMENT 'Known app, otherwise other. Only the daily job enforces that a value cannot name a customer.
@access: opaque-confidential',
  `client_family` LowCardinality(String) COMMENT 'Kind of client, from a fixed list, otherwise other. Empty when no client made the request.
@access: confidential',
  `step` LowCardinality(String) COMMENT 'Step the attempt ended at.
@access: confidential',
  `outcome` LowCardinality(String) COMMENT 'Overall result of the attempt, including abandoned.
@access: confidential',
  `reason` LowCardinality(String) COMMENT 'Reason code, empty for success.
@access: confidential',
  `fixed_by` LowCardinality(String) COMMENT 'What fixed the failure: admin_change, system_change, reconnect or retry. Empty when it was not fixed or nothing failed.
@access: confidential',
  `attempts` UInt64 COMMENT 'Attempts counted.
@access: confidential'
) ENGINE = MergeTree
PRIMARY KEY (`provider`, `app`, `client_family`, `step`, `day`, `outcome`, `reason`, `fixed_by`) ORDER BY (`provider`, `app`, `client_family`, `step`, `day`, `outcome`, `reason`, `fixed_by`) PARTITION BY (`day`) TTL day + toIntervalMonth(13) SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1 COMMENT 'Long-term daily counts of connection outcomes per provider and app, for trends beyond raw retention. No org, project or person. A daily job replaces each whole day atomically, so a day always holds one complete result. Kept 13 months.';
-- Create "mcp_connection_outcomes_daily_staging" table
CREATE TABLE `gram`.`mcp_connection_outcomes_daily_staging` (
  `day` Date COMMENT 'UTC day the attempts started.
@access: confidential',
  `provider` LowCardinality(String) COMMENT 'Known vendor, otherwise other. Only the daily job enforces that a value cannot name a customer.
@access: opaque-confidential',
  `app` LowCardinality(String) COMMENT 'Known app, otherwise other. Only the daily job enforces that a value cannot name a customer.
@access: opaque-confidential',
  `client_family` LowCardinality(String) COMMENT 'Kind of client, from a fixed list, otherwise other. Empty when no client made the request.
@access: confidential',
  `step` LowCardinality(String) COMMENT 'Step the attempt ended at.
@access: confidential',
  `outcome` LowCardinality(String) COMMENT 'Overall result of the attempt, including abandoned.
@access: confidential',
  `reason` LowCardinality(String) COMMENT 'Reason code, empty for success.
@access: confidential',
  `fixed_by` LowCardinality(String) COMMENT 'What fixed the failure: admin_change, system_change, reconnect or retry. Empty when it was not fixed or nothing failed.
@access: confidential',
  `attempts` UInt64 COMMENT 'Attempts counted.
@access: confidential'
) ENGINE = MergeTree
PRIMARY KEY (`provider`, `app`, `client_family`, `step`, `day`, `outcome`, `reason`, `fixed_by`) ORDER BY (`provider`, `app`, `client_family`, `step`, `day`, `outcome`, `reason`, `fixed_by`) PARTITION BY (`day`) TTL day + toIntervalMonth(13) SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1 COMMENT 'Holds one day of mcp_connection_outcomes_daily while the daily job builds it. Only that job reads it. Its TTL must match the main table because REPLACE PARTITION copies TTL info onto the main table, so a shorter TTL here deletes main table rows.';
-- Create "mcp_connection_attempt_summaries_mv" view
CREATE MATERIALIZED VIEW `gram`.`mcp_connection_attempt_summaries_mv` TO `gram`.`mcp_connection_attempt_summaries` AS SELECT organization_id, project_id, server_kind, server_id, toStartOfInterval(toDateTime(attempt_started_at, 'UTC'), toIntervalMinute(30)) AS bucket, step, step_result, outcome, reason, who_fixes, grant_type, member_id, provider_host, app_key, client_family, toUInt64(count()) AS step_events, uniqExactState(attempt_id) AS attempts_seen, uniqExactStateIf(attempt_id, is_final) AS attempts_ended, sum(toUInt64(mcp_connection_events.retries)) AS retries, sum(toUInt64(mcp_connection_events.repeats)) AS repeats, toUInt64(countIf((step = 'discovery_member') AND (step_result = 'listed') AND (mcp_connection_events.retries > 0))) AS recovered, min(event_time) AS first_seen, max(event_time) AS last_seen FROM gram.mcp_connection_events GROUP BY organization_id, project_id, server_kind, server_id, bucket, step, step_result, outcome, reason, who_fixes, grant_type, member_id, provider_host, app_key, client_family;
