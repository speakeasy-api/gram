-- reverse: drop "http_requests_raw" table
CREATE TABLE `http_requests_raw` (
  `id` UUID DEFAULT generateUUIDv7(),
  `ts` DateTime64(3, 'UTC'),
  `organization_id` UUID,
  `project_id` UUID,
  `deployment_id` Nullable(UUID),
  `tool_id` UUID,
  `tool_urn` String,
  `tool_type` LowCardinality(String),
  `trace_id` FixedString(32),
  `span_id` FixedString(16),
  `http_method` LowCardinality(String),
  `http_server_url` String,
  `http_route` String,
  `status_code` Int64,
  `duration_ms` Float64,
  `user_agent` LowCardinality(String),
  `request_headers` Map(String, String) CODEC(ZSTD(1)),
  `request_body_bytes` Int64,
  `response_headers` Map(String, String) CODEC(ZSTD(1)),
  `response_body_bytes` Int64,
  INDEX `idx_status_code` ((status_code)) TYPE set(100) GRANULARITY 4,
  INDEX `idx_tool_type` ((tool_type)) TYPE set(0) GRANULARITY 4,
  INDEX `idx_tool_urn_exact` ((tool_urn)) TYPE bloom_filter(0.01) GRANULARITY 4,
  INDEX `idx_tool_urn_substring` ((tool_urn)) TYPE ngrambf_v1(4, 30720, 3, 0) GRANULARITY 4
) ENGINE = MergeTree
PRIMARY KEY ((toUInt128(project_id)), `ts`) ORDER BY (toUInt128(project_id), `ts`) TTL ts + toIntervalDay(30) SETTINGS index_granularity = 8192 COMMENT 'Stores raw HTTP tool call requests and responses';
