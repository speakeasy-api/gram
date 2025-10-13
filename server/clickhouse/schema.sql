USE gram;

create table if not exists http_requests_raw
(
    id                  UUID DEFAULT generateUUIDv7(),
    ts                  DateTime64(3, 'UTC'),
    organization_id     UUID,
    project_id          UUID,
    deployment_id       UUID,
    tool_id             UUID,
    tool_urn            String,
    tool_type           LowCardinality(String),

    trace_id            FixedString(32),
    span_id             FixedString(16),

    http_method         LowCardinality(String),
    http_route          String,
    status_code         Int64,
    duration_ms         Float64,
    user_agent          LowCardinality(String),

    request_headers     Map(String, String) CODEC (ZSTD),
    request_body_bytes  Int64,

    response_headers    Map(String, String) CODEC (ZSTD),
    response_body_bytes Int64
) engine = MergeTree
      PARTITION BY toDate(ts)
      ORDER BY (toUInt128(project_id), ts)
      TTL ts + toIntervalDay(60)
      SETTINGS index_granularity = 8192
      COMMENT 'Stores raw HTTP tool call requests and responses';

