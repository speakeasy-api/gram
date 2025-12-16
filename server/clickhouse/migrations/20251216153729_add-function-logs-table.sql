-- Create "function_logs" table
CREATE TABLE `function_logs` (
  `id` UUID DEFAULT generateUUIDv7() COMMENT 'Unique identifier for the log entry.',
  `time_unix_nano` Int64 COMMENT 'Unix time (ns) when the event occurred measured by the origin clock, i.e. the function log.' CODEC(Delta(8), ZSTD(1)),
  `observed_time_unix_nano` Int64 COMMENT 'Unix time (ns) when the event was observed by the collection system, i.e. the server.' CODEC(Delta(8), ZSTD(1)),
  `severity_text` LowCardinality(Nullable(String)) COMMENT 'Original string representation of the severity as it is known at the source (the function).',
  `body` Nullable(String) COMMENT 'The body of the raw log record as emitted by the server. Can be for example a human-readable string message describing the event in a free form or it can be a structured data composed of arrays and maps of other values.' CODEC(ZSTD(1)),
  `message` Nullable(String) COMMENT 'Message extracted from the log body.' CODEC(ZSTD(1)),
  `trace_id` Nullable(FixedString(32)) COMMENT 'Request trace id. Can be set for logs that are part of request processing and have an assigned trace id.',
  `span_id` Nullable(FixedString(16)) COMMENT 'Can be set for logs that are part of a particular processing span. If SpanId is present TraceId SHOULD be also present.',
  `attributes` JSON COMMENT 'Additional information about the specific event occurrence.' CODEC(ZSTD(1)),
  `resource_service_name` String COMMENT 'Logical name of the service (e.g. gram-function-runner).',
  `resource_service_version` Nullable(String) COMMENT 'The version string of the service API or implementation.',
  `resource_gram_project_id` UUID COMMENT 'The project ID where the function that generated the log ran.',
  `resource_gram_deployment_id` UUID COMMENT 'The deployment ID associated with the log.',
  `resource_gram_function_id` UUID COMMENT 'ID of the function that generated the log.',
  INDEX `idx_function_logs_deployment_id` ((resource_gram_deployment_id)) TYPE bloom_filter(0.01) GRANULARITY 1,
  INDEX `idx_function_logs_function_id` ((resource_gram_function_id)) TYPE bloom_filter(0.01) GRANULARITY 1,
  INDEX `idx_function_logs_project_id` ((resource_gram_project_id)) TYPE bloom_filter(0.01) GRANULARITY 1,
  INDEX `idx_function_logs_service_name` ((resource_service_name)) TYPE bloom_filter(0.01) GRANULARITY 1,
  INDEX `idx_function_logs_severity_text` ((severity_text)) TYPE set(0) GRANULARITY 4
) ENGINE = MergeTree
PRIMARY KEY (`resource_gram_project_id`, `time_unix_nano`, `id`) ORDER BY (`resource_gram_project_id`, `time_unix_nano`, `id`) PARTITION BY (toYYYYMMDD(fromUnixTimestamp64Nano(time_unix_nano))) TTL fromUnixTimestamp64Nano(time_unix_nano) + toIntervalDay(30) SETTINGS index_granularity = 8192 COMMENT 'Stores logs from Gram function executions following OTel specification';
