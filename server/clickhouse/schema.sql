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
    id UUID DEFAULT generateUUIDv7() COMMENT 'Unique identifier for the log entry.',
    timestamp DateTime64(3, 'UTC') COMMENT 'Timestamp at which log was generated.' CODEC(Delta, ZSTD),
    instance String COMMENT 'Name of the machine instance that generated the log (e.g. snowy-water-123).' CODEC(ZSTD),
    level LowCardinality(String) COMMENT 'Log level.',
    source LowCardinality(String) COMMENT 'The log source (server or user).',

    raw_log String COMMENT 'Full log as sent by the server (function logs are wrapped by this log).' CODEC(ZSTD),
    message Nullable(String) COMMENT 'The message output from the function log.' CODEC(ZSTD),
    attributes JSON COMMENT 'The log attributes (extra fields from structured json logs).' CODEC(ZSTD),

    project_id UUID COMMENT 'ID of the project where the gram function ran.',
    deployment_id UUID COMMENT 'Deployment ID associated with the gram function run.',
    function_id UUID COMMENT 'ID of the gram function.'
) ENGINE = MergeTree()
PARTITION BY toYYYYMMDD(timestamp)
ORDER BY (project_id, timestamp, instance)
TTL timestamp + INTERVAL 30 DAY
SETTINGS index_granularity = 8192
COMMENT 'Stores logs from Gram function executions';

CREATE INDEX IF NOT EXISTS idx_project_id ON tool_logs (project_id) TYPE bloom_filter(0.01) GRANULARITY 1;
CREATE INDEX IF NOT EXISTS idx_deployment_id ON tool_logs (deployment_id) TYPE bloom_filter(0.01) GRANULARITY 1;
CREATE INDEX IF NOT EXISTS idx_function_id ON tool_logs (function_id) TYPE bloom_filter(0.01) GRANULARITY 1;
CREATE INDEX IF NOT EXISTS idx_instance ON tool_logs (instance) TYPE bloom_filter(0.01) GRANULARITY 1;
CREATE INDEX IF NOT EXISTS idx_source ON tool_logs (source) TYPE set(0) GRANULARITY 4;
CREATE INDEX IF NOT EXISTS idx_level ON tool_logs (level) TYPE set(0) GRANULARITY 4;

CREATE TABLE IF NOT EXISTS telemetry_logs (
    -- OTel Log Record Identity
    id UUID DEFAULT generateUUIDv7() COMMENT 'Unique identifier for the log entry.',

    -- OTel Timestamp fields
    time_unix_nano Int64 COMMENT 'Unix time (ns) when the event occurred measured by the origin clock.' CODEC(Delta, ZSTD),
    observed_time_unix_nano Int64 COMMENT 'Unix time (ns) when the event was observed by the collection system.' CODEC(Delta, ZSTD),
    observed_timestamp DateTime64(9) DEFAULT fromUnixTimestamp64Nano(observed_time_unix_nano) COMMENT 'Human-readable timestamp derived from observed_time_unix_nano.',

    -- OTel Severity
    severity_text LowCardinality(Nullable(String)) COMMENT 'Text representation of severity (DEBUG, INFO, WARN, ERROR, FATAL).',

    -- OTel Body (the actual log content/message)
    body String COMMENT 'The primary log message extracted from the log record. For structured logs, this is the human-readable message component.' CODEC(ZSTD),

    -- OTel Trace Context (for distributed tracing)
    trace_id Nullable(FixedString(32)) COMMENT 'W3C trace ID linking related logs across services.',
    span_id Nullable(FixedString(16)) COMMENT 'W3C span ID for specific operation within a trace.',

    -- OTel Attributes (log-level structured data - WHAT happened)
    attributes JSON COMMENT 'Additional attributes about the specific event occurrence.' CODEC(ZSTD),

    -- OTel Resource Attributes (WHO/WHERE generated this log)
    resource_attributes JSON COMMENT 'Attributes describing the resource that generated this log.' CODEC(ZSTD),

    -- Denormalized Gram Fields (for fast filtering)
    gram_project_id UUID COMMENT 'Project ID (denormalized from resource_attributes).',
    gram_deployment_id Nullable(UUID) COMMENT 'Deployment ID (denormalized from resource_attributes).',
    gram_function_id Nullable(UUID) COMMENT 'Function ID that generated the log (null for HTTP logs).',
    gram_urn String COMMENT 'The Gram URN (e.g. tools:function:my-source:my-tool).',
    service_name LowCardinality(String) COMMENT 'Logical service name (e.g., gram-functions, gram-http-gateway).',
    service_version Nullable(String) COMMENT 'Service version.',
    gram_chat_id Nullable(String) COMMENT 'The Chat ID associated with the log (if generated by chat).',

    -- Materialized fields (auto-extracted from JSON for fast filtering)
    -- These duplicate the above denormalized fields but are auto-computed from JSON at insert time.
    -- New code should prefer these over the gram_* columns above.
    -- Note: We avoid Nullable for observability data per ClickHouse best practices (storage overhead, query performance).
    -- Note: ClickHouse auto-unflattens dotted keys (e.g. "gram.project.id" becomes nested {gram:{project:{id:...}}})
    --       so we use dot notation to access nested paths. See: https://github.com/ClickHouse/ClickHouse/issues/69846
    project_id String MATERIALIZED toString(attributes.gram.project.id) COMMENT 'Project ID (materialized from attributes.gram.project.id).',
    deployment_id String MATERIALIZED toString(resource_attributes.gram.deployment.id) COMMENT 'Deployment ID (materialized from resource_attributes.gram.deployment.id).',
    function_id String MATERIALIZED toString(attributes.gram.function.id) COMMENT 'Function ID (materialized from attributes.gram.function.id).',
    urn String MATERIALIZED toString(attributes.gram.tool.urn) COMMENT 'Tool URN (materialized from attributes.gram.tool.urn).',
    chat_id String MATERIALIZED toString(attributes.gen_ai.conversation.id) COMMENT 'Chat ID (materialized from attributes.gen_ai.conversation.id).',
    user_id String MATERIALIZED toString(attributes.user.id) COMMENT 'User ID (materialized from attributes.user.id).',
    external_user_id String MATERIALIZED toString(attributes.gram.external_user.id) COMMENT 'External user ID (materialized from attributes.gram.external_user.id).',
    api_key_id String MATERIALIZED toString(attributes.gram.api_key.id) COMMENT 'API key ID (materialized from attributes.gram.api_key.id).',
    evaluation_score_label String MATERIALIZED toString(attributes.gen_ai.evaluation.score.label) COMMENT 'Evaluation result label (success, failure, partial, abandoned).'
) ENGINE = MergeTree
PARTITION BY toYYYYMMDD(fromUnixTimestamp64Nano(time_unix_nano))
ORDER BY (gram_project_id, time_unix_nano, id)
TTL fromUnixTimestamp64Nano(time_unix_nano) + INTERVAL 30 DAY
SETTINGS index_granularity = 8192
COMMENT 'Unified OTel-compatible telemetry logs from all Gram sources (HTTP requests, function logs, etc.)';

-- Note: gram_project_id is already first in ORDER BY, so no bloom filter index needed for it
-- Primary query patterns
CREATE INDEX IF NOT EXISTS idx_telemetry_logs_gram_urn ON telemetry_logs (gram_urn) TYPE bloom_filter(0.01) GRANULARITY 1;
CREATE INDEX IF NOT EXISTS idx_telemetry_logs_trace_id ON telemetry_logs (trace_id) TYPE bloom_filter(0.01) GRANULARITY 1;
CREATE INDEX IF NOT EXISTS idx_telemetry_logs_deployment_id ON telemetry_logs (gram_deployment_id) TYPE bloom_filter(0.01) GRANULARITY 1;
-- Secondary filters
CREATE INDEX IF NOT EXISTS idx_telemetry_logs_function_id ON telemetry_logs (gram_function_id) TYPE bloom_filter(0.01) GRANULARITY 1;
CREATE INDEX IF NOT EXISTS idx_telemetry_logs_severity ON telemetry_logs (severity_text) TYPE set(0) GRANULARITY 4;
CREATE INDEX IF NOT EXISTS idx_telemetry_logs_chat_id ON telemetry_logs (gram_chat_id) TYPE bloom_filter(0.01) GRANULARITY 1;
-- Indexes for materialized columns (used by new code)
CREATE INDEX IF NOT EXISTS idx_telemetry_logs_mat_deployment_id ON telemetry_logs (deployment_id) TYPE bloom_filter(0.01) GRANULARITY 1;
CREATE INDEX IF NOT EXISTS idx_telemetry_logs_mat_function_id ON telemetry_logs (function_id) TYPE bloom_filter(0.01) GRANULARITY 1;
CREATE INDEX IF NOT EXISTS idx_telemetry_logs_mat_urn ON telemetry_logs (urn) TYPE bloom_filter(0.01) GRANULARITY 1;
CREATE INDEX IF NOT EXISTS idx_telemetry_logs_mat_chat_id ON telemetry_logs (chat_id) TYPE bloom_filter(0.01) GRANULARITY 1;
CREATE INDEX IF NOT EXISTS idx_telemetry_logs_mat_user_id ON telemetry_logs (user_id) TYPE bloom_filter(0.01) GRANULARITY 1;
CREATE INDEX IF NOT EXISTS idx_telemetry_logs_mat_external_user_id ON telemetry_logs (external_user_id) TYPE bloom_filter(0.01) GRANULARITY 1;
CREATE INDEX IF NOT EXISTS idx_telemetry_logs_mat_api_key_id ON telemetry_logs (api_key_id) TYPE bloom_filter(0.01) GRANULARITY 1;
CREATE INDEX IF NOT EXISTS idx_telemetry_logs_mat_evaluation_score_label ON telemetry_logs (evaluation_score_label) TYPE bloom_filter(0.01) GRANULARITY 1;

CREATE TABLE IF NOT EXISTS trace_summaries (
    -- Key cols
    trace_id FixedString(32),
    gram_project_id UUID,

    -- Filter cols. Plain vailes, filterable using WHERE
    gram_deployment_id SimpleAggregateFunction(any, Nullable(UUID)),
    gram_function_id SimpleAggregateFunction(any, Nullable(UUID)),
    gram_urn SimpleAggregateFunction(any, String),

    -- Aggregates
    start_time_unix_nano SimpleAggregateFunction(min, Int64),
    log_count SimpleAggregateFunction(sum, UInt64),

    http_status_code AggregateFunction(anyIf, Nullable(Int32), UInt8)
) ENGINE = AggregatingMergeTree
ORDER BY (gram_project_id, trace_id)
TTL fromUnixTimestamp64Nano(start_time_unix_nano) + INTERVAL 30 DAY
SETTINGS index_granularity = 8192
COMMENT 'Pre-aggregated trace summaries for fast trace-level queries without needing to scan all logs';

create MATERIALIZED VIEW IF NOT EXISTS trace_summaries_mv TO trace_summaries AS
SELECT
    trace_id,
    gram_project_id,
    any(gram_deployment_id) AS gram_deployment_id,
    any(gram_function_id) AS gram_function_id,
    any(gram_urn) AS gram_urn,
    min(time_unix_nano) AS start_time_unix_nano,
    toUInt64(count(*)) AS log_count,
    anyIfState(
        toInt32OrNull(toString(attributes.http.response.status_code)),
        toString(attributes.http.response.status_code) != ''
    ) AS http_status_code
FROM telemetry_logs
WHERE trace_id IS NOT NULL AND trace_id != ''
GROUP BY trace_id, gram_project_id;
