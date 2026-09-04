-- SPIKE ONLY. This file is not part of the authoritative ClickHouse schema and
-- is not consumed by migration tooling. It demonstrates the intended physical
-- shape for native telemetry signals, one canonical fact, and one rollup.

-- A replacement must keep organization_id, project_id, event_time, and
-- record_id stable because ReplacingMergeTree deduplicates on the complete
-- ORDER BY tuple. Corrections change ingest_version and payload fields only.
CREATE TABLE IF NOT EXISTS telemetry_log_records (
    organization_id UUID,
    project_id UUID,
    event_time DateTime64(9, 'UTC') CODEC(Delta, ZSTD),
    observed_at DateTime64(9, 'UTC') CODEC(Delta, ZSTD),
    record_id UUID,
    ingest_version UInt64,
    schema_version UInt16,
    source LowCardinality(String),
    ingress LowCardinality(String),
    normalizer LowCardinality(String),
    trace_id String DEFAULT '' CODEC(ZSTD),
    span_id String DEFAULT '' CODEC(ZSTD),
    severity_number UInt8 DEFAULT 0,
    severity_text LowCardinality(String) DEFAULT '',
    event_name LowCardinality(String) DEFAULT '',
    body String DEFAULT '' CODEC(ZSTD),
    attributes JSON CODEC(ZSTD),
    source_payload String DEFAULT '' CODEC(ZSTD(3))
) ENGINE = ReplacingMergeTree(ingest_version)
PARTITION BY toYYYYMMDD(event_time)
ORDER BY (organization_id, project_id, event_time, record_id)
TTL event_time + INTERVAL 90 DAY
SETTINGS index_granularity = 8192;

CREATE INDEX IF NOT EXISTS idx_telemetry_log_records_trace_id
ON telemetry_log_records (trace_id)
TYPE bloom_filter(0.01) GRANULARITY 1;

-- start_time is immutable identity metadata for a span replacement.
CREATE TABLE IF NOT EXISTS telemetry_spans (
    organization_id UUID,
    project_id UUID,
    start_time DateTime64(9, 'UTC') CODEC(Delta, ZSTD),
    end_time DateTime64(9, 'UTC') CODEC(Delta, ZSTD),
    observed_at DateTime64(9, 'UTC') CODEC(Delta, ZSTD),
    trace_id FixedString(32),
    span_id FixedString(16),
    parent_span_id String DEFAULT '' CODEC(ZSTD),
    ingest_version UInt64,
    schema_version UInt16,
    source LowCardinality(String),
    ingress LowCardinality(String),
    normalizer LowCardinality(String),
    name LowCardinality(String),
    kind Enum8(
        'unspecified' = 0,
        'internal' = 1,
        'server' = 2,
        'client' = 3,
        'producer' = 4,
        'consumer' = 5
    ),
    status Enum8(
        'unset' = 0,
        'ok' = 1,
        'error' = 2
    ),
    status_message String DEFAULT '' CODEC(ZSTD),
    attributes JSON CODEC(ZSTD),
    source_payload String DEFAULT '' CODEC(ZSTD(3))
) ENGINE = ReplacingMergeTree(ingest_version)
PARTITION BY toYYYYMMDD(start_time)
ORDER BY (organization_id, project_id, start_time, trace_id, span_id)
TTL start_time + INTERVAL 90 DAY
SETTINGS index_granularity = 8192;

CREATE INDEX IF NOT EXISTS idx_telemetry_spans_trace_id
ON telemetry_spans (trace_id)
TYPE bloom_filter(0.01) GRANULARITY 1;

-- point_time is immutable identity metadata for a metric-point replacement.
CREATE TABLE IF NOT EXISTS telemetry_metric_points (
    organization_id UUID,
    project_id UUID,
    point_time DateTime64(9, 'UTC') CODEC(Delta, ZSTD),
    start_time DateTime64(9, 'UTC') CODEC(Delta, ZSTD),
    observed_at DateTime64(9, 'UTC') CODEC(Delta, ZSTD),
    record_id UUID,
    ingest_version UInt64,
    schema_version UInt16,
    source LowCardinality(String),
    ingress LowCardinality(String),
    normalizer LowCardinality(String),
    metric_name LowCardinality(String),
    unit LowCardinality(String) DEFAULT '',
    value_kind Enum8(
        'gauge' = 1,
        'sum' = 2,
        'histogram' = 3
    ),
    aggregation_temporality Enum8(
        'unspecified' = 0,
        'delta' = 1,
        'cumulative' = 2
    ),
    is_monotonic Bool DEFAULT false,
    number_value Float64 DEFAULT 0,
    histogram_count UInt64 DEFAULT 0,
    histogram_sum Float64 DEFAULT 0,
    histogram_bounds Array(Float64) DEFAULT [],
    histogram_bucket_counts Array(UInt64) DEFAULT [],
    attributes JSON CODEC(ZSTD),
    source_payload String DEFAULT '' CODEC(ZSTD(3))
) ENGINE = ReplacingMergeTree(ingest_version)
PARTITION BY toYYYYMMDD(point_time)
ORDER BY (organization_id, project_id, point_time, metric_name, record_id)
TTL point_time + INTERVAL 90 DAY
SETTINGS index_granularity = 8192;

-- event_time is immutable identity metadata for a fact replacement. If the
-- product requires correcting event_time itself, this sorting key must change
-- or the writer must explicitly retract the previous identity.
CREATE TABLE IF NOT EXISTS telemetry_usage_facts (
    organization_id UUID,
    project_id UUID,
    event_time DateTime64(9, 'UTC') CODEC(Delta, ZSTD),
    fact_id UUID,
    fact_version UInt64,
    schema_version UInt16,
    source_record_ids Array(UUID),
    source LowCardinality(String),
    provider LowCardinality(String) DEFAULT '',
    model LowCardinality(String) DEFAULT '',
    billing_mode LowCardinality(String) DEFAULT '',
    user_id String DEFAULT '' CODEC(ZSTD),
    account_id String DEFAULT '' CODEC(ZSTD),
    trace_id String DEFAULT '' CODEC(ZSTD),
    session_id String DEFAULT '' CODEC(ZSTD),
    input_tokens UInt64 DEFAULT 0,
    output_tokens UInt64 DEFAULT 0,
    cache_read_input_tokens UInt64 DEFAULT 0,
    cache_creation_input_tokens UInt64 DEFAULT 0,
    total_tokens UInt64 MATERIALIZED
        input_tokens + output_tokens + cache_read_input_tokens + cache_creation_input_tokens,
    total_cost Decimal(20, 12) DEFAULT 0
) ENGINE = ReplacingMergeTree(fact_version)
PARTITION BY toYYYYMMDD(event_time)
ORDER BY (organization_id, project_id, event_time, fact_id)
TTL event_time + INTERVAL 90 DAY
SETTINGS index_granularity = 8192;

-- Refreshable rather than incremental: the source uses replacement semantics,
-- so an insert-triggered additive MV could permanently double-count a retried
-- or corrected fact. The default refresh mode replaces the complete target.
CREATE MATERIALIZED VIEW IF NOT EXISTS telemetry_usage_daily
REFRESH EVERY 5 MINUTE
ENGINE = MergeTree
PARTITION BY toYYYYMMDD(day)
ORDER BY (organization_id, project_id, day, provider, model, source)
AS SELECT
    organization_id,
    project_id,
    toStartOfDay(event_time) AS day,
    provider,
    model,
    source,
    toUInt64(count()) AS requests,
    sum(input_tokens) AS input_tokens,
    sum(output_tokens) AS output_tokens,
    sum(cache_read_input_tokens) AS cache_read_input_tokens,
    sum(cache_creation_input_tokens) AS cache_creation_input_tokens,
    sum(total_tokens) AS total_tokens,
    sum(total_cost) AS total_cost
FROM telemetry_usage_facts FINAL
WHERE event_time >= now64(9, 'UTC') - INTERVAL 90 DAY
GROUP BY organization_id, project_id, day, provider, model, source;
