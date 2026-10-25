-- create "mcp_connection_attempt_summaries" table
CREATE TABLE `gram`.`mcp_connection_attempt_summaries` (
  `gram_project_id` UUID COMMENT 'Project the attempt belongs to.
@access: confidential',
  `server_kind` LowCardinality(String) COMMENT 'Kind of server the attempt reached: mcp, meta, or empty when none was resolved.
@access: confidential',
  `server_id` String COMMENT 'ID of the gateway (meta MCP server) or MCP server the attempt reached, empty when none was resolved.
@access: confidential',
  `stage` LowCardinality(String) COMMENT 'Stage the attempt was observed at, such as inbound_token or discovery_member. Rates are computed within one stage.
@access: confidential',
  `bucket` DateTime('UTC') COMMENT 'UTC 30-minute bucket containing the attempt.
@access: confidential',
  `origin` String COMMENT 'Normalized host of the observed endpoint (CIMD document or upstream token endpoint), empty for stages without one.
@access: restricted',
  `subject` String COMMENT 'Subject URN of the authenticated caller, empty before authentication.
@access: confidential-pii',
  `provider_id` String COMMENT 'Remote session issuer ID of the upstream provider, empty when the attempt has none.
@access: confidential',
  `client_key` String COMMENT 'Registered client ID, or a bounded client kind when the client is not registered.
@access: confidential',
  `member_id` String COMMENT 'Gateway member MCP server ID for member-level stages, empty otherwise.
@access: confidential',
  `grant_type` LowCardinality(String) COMMENT 'OAuth grant type for token attempts, empty otherwise.
@access: confidential',
  `outcome` LowCardinality(String) COMMENT 'Outcome of the attempt within its stage.
@access: confidential',
  `failure_class` LowCardinality(String) COMMENT 'Who a failure belongs to: endpoint, configuration, internal or canceled. Empty for success.
@access: confidential',
  `reason` LowCardinality(String) COMMENT 'Sanitized reason code for the outcome, empty for success.
@access: confidential',
  `attempts` SimpleAggregateFunction(sum, UInt64) COMMENT 'Attempts observed.
@access: confidential',
  `retries` SimpleAggregateFunction(sum, UInt64) COMMENT 'Retries spent across these attempts.
@access: confidential',
  `recovered` SimpleAggregateFunction(sum, UInt64) COMMENT 'Gateway members listed only after at least one retry.
@access: confidential',
  `last_seen` SimpleAggregateFunction(max, DateTime64(9, 'UTC')) COMMENT 'Time of the latest attempt.
@access: confidential'
) ENGINE = AggregatingMergeTree
PRIMARY KEY (`gram_project_id`, `server_kind`, `server_id`, `stage`, `bucket`) ORDER BY (`gram_project_id`, `server_kind`, `server_id`, `stage`, `bucket`, `origin`, `subject`, `provider_id`, `client_key`, `member_id`, `grant_type`, `outcome`, `failure_class`, `reason`) PARTITION BY (toYYYYMM(bucket)) TTL bucket + toIntervalDay(90) SETTINGS index_granularity = 8192 COMMENT 'Connection attempt outcomes per 30-minute bucket, from mcp_connection_attempt telemetry rows';
-- create "mcp_connection_attempt_summaries_mv" view
CREATE MATERIALIZED VIEW `gram`.`mcp_connection_attempt_summaries_mv` TO `gram`.`mcp_connection_attempt_summaries` AS SELECT gram_project_id, multiIf(meta_mcp_server_id != '', 'meta', mcp_server_id != '', 'mcp', '') AS server_kind, if(meta_mcp_server_id != '', meta_mcp_server_id, mcp_server_id) AS server_id, toLowCardinality(toString(attributes.gram.connection_attempt.stage)) AS stage, toStartOfInterval(toDateTime(fromUnixTimestamp64Nano(time_unix_nano), 'UTC'), toIntervalMinute(30)) AS bucket, toString(attributes.gram.connection_attempt.origin) AS origin, toString(attributes.gram.connection_attempt.subject) AS subject, toString(attributes.gram.connection_attempt.provider_id) AS provider_id, toString(attributes.gram.connection_attempt.client_key) AS client_key, if(meta_mcp_server_id != '', mcp_server_id, '') AS member_id, toLowCardinality(toString(attributes.gram.connection_attempt.grant_type)) AS grant_type, toLowCardinality(toString(attributes.gram.connection_attempt.outcome)) AS outcome, toLowCardinality(toString(attributes.gram.connection_attempt.failure_class)) AS failure_class, toLowCardinality(toString(attributes.gram.connection_attempt.reason)) AS reason, toUInt64(count()) AS attempts, sum(toUInt64OrZero(toString(attributes.gram.connection_attempt.retries))) AS retries, toUInt64(countIf((stage = 'discovery_member') AND (outcome = 'listed') AND (toUInt64OrZero(toString(attributes.gram.connection_attempt.retries)) > 0))) AS recovered, max(fromUnixTimestamp64Nano(time_unix_nano, 'UTC')) AS last_seen FROM gram.telemetry_logs WHERE (event_urn = 'urn:telemetry:gram_service:log:mcp_connection_attempt') AND (stage != '') GROUP BY gram_project_id, server_kind, server_id, stage, bucket, origin, subject, provider_id, client_key, member_id, grant_type, outcome, failure_class, reason;
