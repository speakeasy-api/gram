create table http_requests_raw
(
    ts                  DateTime64(3, 'UTC'),
    organization_id     String,
    project_id          String,
    deployment_id       String,
    tool_id             String,
    tool_type           LowCardinality(String),
    trace_id            FixedString(32),
    span_id             FixedString(16),
    http_method         LowCardinality(String),
    http_route          String,
    status_code         UInt16,
    duration_ms         Float64,
    request_body        Array(UInt8),
    response_body       Array(UInt8),
    request_headers     Array(UInt8),
    response_headers    Array(UInt8),
    request_body_bytes  UInt64,
    response_body_bytes UInt64
) engine = MergeTree
      PARTITION BY toDate(ts)
      ORDER BY (project_id, tool_id, ts)
      TTL ts + toIntervalDay(60)
      SETTINGS index_granularity = 8192
      COMMENT 'Stores raw HTTP tool call requests and responses';

