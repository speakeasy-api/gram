-- create "telemetry_logs_staging" table
CREATE TABLE `telemetry_logs_staging` (
  `id` UUID DEFAULT generateUUIDv7() COMMENT 'Unique identifier for the log entry, preserved when the row is promoted to telemetry_logs.',
  `time_unix_nano` Int64 COMMENT 'Unix time (ns) when the event occurred measured by the origin clock.' CODEC(Delta(8), ZSTD(1)),
  `observed_time_unix_nano` Int64 COMMENT 'Unix time (ns) when the event was observed by the collection system.' CODEC(Delta(8), ZSTD(1)),
  `observed_timestamp` DateTime64(9) DEFAULT fromUnixTimestamp64Nano(observed_time_unix_nano) COMMENT 'Human-readable timestamp derived from observed_time_unix_nano.',
  `severity_text` LowCardinality(Nullable(String)) COMMENT 'Text representation of severity (DEBUG, INFO, WARN, ERROR, FATAL).',
  `body` String COMMENT 'The primary log message extracted from the log record.' CODEC(ZSTD(1)),
  `trace_id` Nullable(FixedString(32)) COMMENT 'W3C trace ID linking related logs across services.',
  `span_id` Nullable(FixedString(16)) COMMENT 'W3C span ID for specific operation within a trace.',
  `attributes` JSON COMMENT 'Additional attributes about the specific event occurrence.' CODEC(ZSTD(1)),
  `resource_attributes` JSON COMMENT 'Attributes describing the resource that generated this log.' CODEC(ZSTD(1)),
  `gram_project_id` UUID COMMENT 'Project ID (denormalized from resource_attributes).',
  `gram_deployment_id` Nullable(UUID) COMMENT 'Deployment ID (denormalized from resource_attributes).',
  `gram_function_id` Nullable(UUID) COMMENT 'Function ID that generated the log (null for HTTP logs).',
  `gram_urn` String COMMENT 'The Gram URN (e.g. claude-code:otel:logs).',
  `service_name` LowCardinality(String) COMMENT 'Logical service name.',
  `service_version` Nullable(String) COMMENT 'Service version.',
  `gram_chat_id` Nullable(String) COMMENT 'The Chat ID (Claude session id) associated with the log.',
  `chat_id` String MATERIALIZED toString(attributes.gen_ai.conversation.id) COMMENT 'Chat ID (materialized from attributes.gen_ai.conversation.id) — the promotion worker scopes by this.',
  `request_id` String MATERIALIZED toString(attributes.request_id) COMMENT 'Claude API request id (materialized from attributes.request_id) — the attribution join key.'
) ENGINE = MergeTree
PRIMARY KEY (`gram_project_id`, `time_unix_nano`, `id`) ORDER BY (`gram_project_id`, `time_unix_nano`, `id`) PARTITION BY (toYYYYMMDD(fromUnixTimestamp64Nano(time_unix_nano))) TTL fromUnixTimestamp64Nano(observed_time_unix_nano) + toIntervalDay(2) SETTINGS index_granularity = 8192 COMMENT 'Holding pen for Claude OTEL api_request rows with redacted (custom) MCP attribution, awaiting transcript-derived attribution before promotion into telemetry_logs.';
-- create index "idx_telemetry_logs_staging_chat_id" to table: "telemetry_logs_staging"
ALTER TABLE `telemetry_logs_staging` ADD INDEX `idx_telemetry_logs_staging_chat_id` ((chat_id)) TYPE bloom_filter(0.01) GRANULARITY 1;
