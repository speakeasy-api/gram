create table if not exists http_requests_raw
(
    id                  UUID DEFAULT generateUUIDv7(),
    ts                  DateTime64(3, 'UTC'),
    organization_id     UUID,
    project_id          UUID,
    deployment_id       Nullable(UUID),
    tool_id             UUID,
    tool_urn            String,
    tool_type           LowCardinality(String),

    trace_id            FixedString(32),
    span_id             FixedString(16),

    http_method         LowCardinality(String),
    http_server_url     String,
    http_route          String,
    status_code         Int64,
    duration_ms         Float64,
    user_agent          LowCardinality(String),

    request_headers     Map(String, String) CODEC (ZSTD),
    request_body_bytes  Int64,

    response_headers    Map(String, String) CODEC (ZSTD),
    response_body_bytes Int64
) engine = MergeTree
      ORDER BY (toUInt128(project_id), ts)
      TTL ts + toIntervalDay(30)
      SETTINGS index_granularity = 8192
      COMMENT 'Stores raw HTTP tool call requests and responses';

CREATE INDEX IF NOT EXISTS idx_tool_type ON http_requests_raw (tool_type) TYPE set (0) GRANULARITY 4;
CREATE INDEX IF NOT EXISTS idx_status_code ON http_requests_raw (status_code) TYPE set (100) GRANULARITY 4;
CREATE INDEX IF NOT EXISTS idx_tool_urn_exact ON http_requests_raw (tool_urn) TYPE bloom_filter(0.01) GRANULARITY 4;
CREATE INDEX IF NOT EXISTS idx_tool_urn_substring ON http_requests_raw (tool_urn) TYPE ngrambf_v1(4, 30720, 3, 0) GRANULARITY 4;

CREATE TABLE IF NOT EXISTS tool_logs (
    timestamp DateTime64(3, 'UTC') CODEC(Delta, ZSTD),
    instance String CODEC(ZSTD),
    level LowCardinality(String),
    source LowCardinality(String),

    raw_log String CODEC(ZSTD),
    message Nullable(String) CODEC(ZSTD),
    attributes JSON CODEC(ZSTD),

    project_id UUID,
    deployment_id UUID,
    function_id UUID
) ENGINE = MergeTree()
PARTITION BY toYYYYMMDD(timestamp)
ORDER BY (project_id, timestamp, instance)
TTL timestamp + INTERVAL 30 DAY
SETTINGS index_granularity = 8192
COMMENT 'Stores logs from Gram function executions';

ALTER TABLE tool_logs COMMENT COLUMN timestamp 'Timestamp at which log was generated.';
ALTER TABLE tool_logs COMMENT COLUMN instance 'Name of the machine instance that generated the log (e.g. snowy-water-123).';
ALTER TABLE tool_logs COMMENT COLUMN level 'Log level.';
ALTER TABLE tool_logs COMMENT COLUMN source 'The log source (server or user).';
ALTER TABLE tool_logs COMMENT COLUMN raw_log 'Full log sent by the server (function logs are wrapped by this log).';
ALTER TABLE tool_logs COMMENT COLUMN message 'The message output from the function log.';
ALTER TABLE tool_logs COMMENT COLUMN attributes 'The log attributes (extra fields from structured json logs).';
ALTER TABLE tool_logs COMMENT COLUMN project_id 'ID of the project where the gram function ran.';
ALTER TABLE tool_logs COMMENT COLUMN deployment_id 'Deployment ID associated with the gram function run.';
ALTER TABLE tool_logs COMMENT COLUMN function_id 'ID of the gram function.';

CREATE INDEX IF NOT EXISTS idx_project_id ON tool_logs (project_id) TYPE bloom_filter(0.01) GRANULARITY 1;
CREATE INDEX IF NOT EXISTS idx_deployment_id ON tool_logs (deployment_id) TYPE bloom_filter(0.01) GRANULARITY 1;
CREATE INDEX IF NOT EXISTS idx_function_id ON tool_logs (function_id) TYPE bloom_filter(0.01) GRANULARITY 1;
CREATE INDEX IF NOT EXISTS idx_instance ON tool_logs (instance) TYPE bloom_filter(0.01) GRANULARITY 1;
CREATE INDEX IF NOT EXISTS idx_source ON tool_logs (source) TYPE set(0) GRANULARITY 4;
CREATE INDEX IF NOT EXISTS idx_level ON tool_logs (level) TYPE set(0) GRANULARITY 4;