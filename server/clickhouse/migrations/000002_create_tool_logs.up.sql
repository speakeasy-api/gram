CREATE TABLE IF NOT EXISTS tool_logs
(
    ts              DateTime64(3, 'UTC'),
    organization_id LowCardinality(String),
    project_id      LowCardinality(String),
    deployment_id   LowCardinality(String),
    tool_type       Enum8('http'=1,'function'=2),
    tool_id         LowCardinality(String),

    trace_id        FixedString(32),
    span_id         FixedString(16),    -- should we use UUID here?

    level           Enum8('TRACE'=1,'DEBUG'=2,'INFO'=3,'WARN'=4,'ERROR'=5),
    message         String CODEC(ZSTD), -- should this be capped?
    attrs_json      String CODEC(ZSTD), -- can be renamed to metadata_json

    ingest_time     DateTime DEFAULT now()
) ENGINE = MergeTree
PARTITION BY toDate(ts)
ORDER BY (organization_id, project_id, deployment_id, tool_type, tool_id, ts)
TTL ts + toIntervalDay(30);
