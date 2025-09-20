CREATE TABLE IF NOT EXISTS http_requests_raw
(
    ts                  DateTime64(3, 'UTC'),

    -- required multi-tenant keys
    organization_id     LowCardinality(String),
    project_id          LowCardinality(String),
    deployment_id       LowCardinality(String),
    tool_type           Enum8('http' = 1, 'function' = 2) DEFAULT 'http',
    tool_id             LowCardinality(String),

    -- correlation
    trace_id            FixedString(32),        -- OTel hex (128-bit)
    span_id             FixedString(16),        -- Can also be UUID?
    request_id          UUID,

    -- url identity
    http_role           Enum8('server' = 1, 'client' = 2),
    http_method         Enum8('_OTHER'=0,'GET'=1,'POST'=2,'PUT'=3,'DELETE'=4,'PATCH'=5,'HEAD'=6,'OPTIONS'=7,'TRACE'=8,'CONNECT'=9),
    http_route          LowCardinality(String), -- route template per OTel, not full path
    status_code         UInt16,

    -- sizes & timing
    duration_ms         Float64,                -- per OTel: histogram base unit is seconds; we store ms & convert
    request_body_bytes  UInt64,
    response_body_bytes UInt64,

    -- outcome
    success             UInt8,                  -- 1 if 2xx/3xx and no error classification
    error_type          LowCardinality(String)  -- low-cardinality classifier (timeout, 5xx, etc.)
) ENGINE = MergeTree
      PARTITION BY toDate(ts)
      ORDER BY (organization_id, project_id, deployment_id, tool_type, tool_id, ts)
      TTL ts + toIntervalDay(15); -- 15-day retention