create table if not exists http_requests_raw
(
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
    status_code         UInt16,
    duration_ms         Float64,
    user_agent          LowCardinality(String),
    client_ipv4         IPv4,

    request_headers     Map(String, String) CODEC (ZSTD),
    request_body        Nullable(String) CODEC (ZSTD),
    request_body_skip   Nullable(String),
    request_body_bytes  UInt64,

    response_headers    Map(String, String) CODEC (ZSTD),
    response_body       Nullable(String) CODEC (ZSTD),
    response_body_skip  Nullable(String),
    response_body_bytes UInt64
) engine = MergeTree
      PARTITION BY toDate(ts)
      ORDER BY (toUInt128(project_id), toUInt128(tool_id), ts)
      TTL ts + toIntervalDay(60)
      SETTINGS index_granularity = 8192
      COMMENT 'Stores raw HTTP tool call requests and responses';

