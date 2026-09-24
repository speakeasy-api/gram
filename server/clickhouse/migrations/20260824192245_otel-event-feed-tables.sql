-- Create "otel_logs" table
CREATE TABLE `otel_logs` (
  `organization_id` String COMMENT 'Organization the record belongs to, from the provenance stamped at the ingest edge.' CODEC(ZSTD(1)),
  `project_id` String COMMENT 'Project the record was ingested under, from the provenance stamped at the ingest edge.' CODEC(ZSTD(1)),
  `time_unix_nano` Int64 COMMENT 'Unix time (ns) when the event occurred. The writer substitutes observed_time_unix_nano when the producer sent 0 and drops records with neither, so this is never epoch zero.' CODEC(Delta(8), ZSTD(1)),
  `observed_time_unix_nano` Int64 COMMENT 'Unix time (ns) when the event was observed by the collection system. 0 when the producer omitted it.' CODEC(Delta(8), ZSTD(1)),
  `timestamp` DateTime64(9) DEFAULT fromUnixTimestamp64Nano(time_unix_nano) COMMENT 'Human-readable timestamp derived from time_unix_nano.',
  `source` LowCardinality(String) COMMENT 'Canonicalized producer surface derived from resource service.name at write time (e.g. claude-code, litellm). unknown when the resource carries no service.name.',
  `trace_id` String COMMENT 'Hex-encoded W3C trace id (32 chars). Empty when the record has no span context.' CODEC(ZSTD(1)),
  `span_id` String COMMENT 'Hex-encoded span id (16 chars). Empty when the record has no span context.' CODEC(ZSTD(1)),
  `event_name` String COMMENT 'Event name for records that represent a named event (OTLP 1.5+). Empty otherwise.' CODEC(ZSTD(1)),
  `severity_text` LowCardinality(String) COMMENT 'Producer-supplied severity text (DEBUG, INFO, WARN, ERROR, FATAL). Empty when unclassified.',
  `severity_number` Int32 COMMENT 'OTLP SeverityNumber enum value (1-24). 0 when unspecified.',
  `body` String COMMENT 'Log body. String bodies are stored verbatim and structured bodies are JSON-encoded.' CODEC(ZSTD(1)),
  `log_attributes` JSON COMMENT 'Log record attributes, including Gram enrichments applied by the transform pipeline.' CODEC(ZSTD(1)),
  `flags` UInt32 COMMENT 'W3C trace flags for the emitting span context.',
  `resource_attributes` JSON COMMENT 'Attributes of the resource that produced the record.' CODEC(ZSTD(1)),
  `resource_schema_url` String COMMENT 'Schema URL of the resource. Empty when not reported.' CODEC(ZSTD(1)),
  `scope_name` LowCardinality(String) COMMENT 'Instrumentation scope name. The transform pipeline rewrites this to com.speakeasy.ai.logging and keeps the producer scope in log_attributes under speakeasy.original_instrumentation_scope.name.',
  `scope_version` String COMMENT 'Instrumentation scope version. Empty when not reported.' CODEC(ZSTD(1)),
  `scope_attributes` JSON COMMENT 'Instrumentation scope attributes.' CODEC(ZSTD(1)),
  INDEX `idx_otel_logs_event_name` ((event_name)) TYPE bloom_filter(0.01) GRANULARITY 1,
  INDEX `idx_otel_logs_severity` ((severity_text)) TYPE set(0) GRANULARITY 4,
  INDEX `idx_otel_logs_source` ((source)) TYPE set(0) GRANULARITY 4,
  INDEX `idx_otel_logs_trace_id` ((trace_id)) TYPE bloom_filter(0.01) GRANULARITY 1
) ENGINE = MergeTree
PRIMARY KEY (`organization_id`, `time_unix_nano`) ORDER BY (`organization_id`, `time_unix_nano`) PARTITION BY (toYYYYMMDD(fromUnixTimestamp64Nano(time_unix_nano))) TTL fromUnixTimestamp64Nano(time_unix_nano) + toIntervalDay(90) SETTINGS index_granularity = 8192 COMMENT 'Normalized OTel log records teed off the gram.otel.v1.LogRecord topic after transform/enrichment, powering the org-scoped Event Feed. Ingestion is at-least-once, so Pub/Sub redelivery can produce duplicate rows and readers must tolerate them.';
-- Create "otel_traces" table
CREATE TABLE `otel_traces` (
  `organization_id` String COMMENT 'Organization the span belongs to, from the provenance stamped at the ingest edge.' CODEC(ZSTD(1)),
  `project_id` String COMMENT 'Project the span was ingested under, from the provenance stamped at the ingest edge.' CODEC(ZSTD(1)),
  `time_unix_nano` Int64 COMMENT 'Span start time as Unix time (ns). Validated non-zero at the ingest edge.' CODEC(Delta(8), ZSTD(1)),
  `timestamp` DateTime64(9) DEFAULT fromUnixTimestamp64Nano(time_unix_nano) COMMENT 'Human-readable timestamp derived from time_unix_nano.',
  `duration_nano` Int64 COMMENT 'Span duration in nanoseconds (end time minus start time).',
  `source` LowCardinality(String) COMMENT 'Canonicalized producer surface derived from resource service.name at write time (e.g. claude-code, litellm). unknown when the resource carries no service.name.',
  `trace_id` String COMMENT 'Hex-encoded W3C trace id (32 chars).' CODEC(ZSTD(1)),
  `span_id` String COMMENT 'Hex-encoded span id (16 chars).' CODEC(ZSTD(1)),
  `parent_span_id` String COMMENT 'Hex-encoded parent span id. Empty for root spans.' CODEC(ZSTD(1)),
  `span_name` String COMMENT 'Span name.' CODEC(ZSTD(1)),
  `span_kind` LowCardinality(String) COMMENT 'OTLP span kind: unspecified | internal | server | client | producer | consumer.',
  `status_code` LowCardinality(String) COMMENT 'OTLP status code: unspecified | ok | error.',
  `status_message` String COMMENT 'Status message, set only when status_code is error.' CODEC(ZSTD(1)),
  `trace_state` String COMMENT 'W3C tracestate header value. Empty when not reported.' CODEC(ZSTD(1)),
  `span_attributes` JSON COMMENT 'Span attributes, including Gram enrichments applied by the transform pipeline.' CODEC(ZSTD(1)),
  `resource_attributes` JSON COMMENT 'Attributes of the resource that produced the span.' CODEC(ZSTD(1)),
  `resource_schema_url` String COMMENT 'Schema URL of the resource. Empty when not reported.' CODEC(ZSTD(1)),
  `scope_name` LowCardinality(String) COMMENT 'Instrumentation scope name. The transform pipeline rewrites this to com.speakeasy.ai.logging and keeps the producer scope in span_attributes under speakeasy.original_instrumentation_scope.name.',
  `scope_version` String COMMENT 'Instrumentation scope version. Empty when not reported.' CODEC(ZSTD(1)),
  `scope_attributes` JSON COMMENT 'Instrumentation scope attributes.' CODEC(ZSTD(1)),
  INDEX `idx_otel_traces_source` ((source)) TYPE set(0) GRANULARITY 4,
  INDEX `idx_otel_traces_span_name` ((span_name)) TYPE bloom_filter(0.01) GRANULARITY 1,
  INDEX `idx_otel_traces_trace_id` ((trace_id)) TYPE bloom_filter(0.01) GRANULARITY 1
) ENGINE = MergeTree
PRIMARY KEY (`organization_id`, `time_unix_nano`) ORDER BY (`organization_id`, `time_unix_nano`) PARTITION BY (toYYYYMMDD(fromUnixTimestamp64Nano(time_unix_nano))) TTL fromUnixTimestamp64Nano(time_unix_nano) + toIntervalDay(90) SETTINGS index_granularity = 8192 COMMENT 'Normalized OTel spans teed off the gram.otel.v1.Span topic after transform/enrichment, powering the org-scoped Event Feed. Ingestion is at-least-once, so Pub/Sub redelivery can produce duplicate rows and readers must tolerate them.';
