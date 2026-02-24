-- reverse: drop "tool_logs" table
CREATE TABLE `tool_logs` (
  `timestamp` DateTime64(3, 'UTC') COMMENT 'Timestamp at which log was generated.' CODEC(Delta(8), ZSTD(1)),
  `instance` String COMMENT 'Name of the machine instance that generated the log (e.g. snowy-water-123).' CODEC(ZSTD(1)),
  `level` LowCardinality(String) COMMENT 'Log level.',
  `source` LowCardinality(String) COMMENT 'The log source (server or user).',
  `raw_log` String COMMENT 'Full log as sent by the server (function logs are wrapped by this log).' CODEC(ZSTD(1)),
  `message` Nullable(String) COMMENT 'The message output from the function log.' CODEC(ZSTD(1)),
  `attributes` JSON COMMENT 'The log attributes (extra fields from structured json logs).' CODEC(ZSTD(1)),
  `project_id` UUID COMMENT 'ID of the project where the gram function ran.',
  `deployment_id` UUID COMMENT 'Deployment ID associated with the gram function run.',
  `function_id` UUID COMMENT 'ID of the gram function.',
  `id` UUID DEFAULT generateUUIDv7() COMMENT 'Unique identifier for the log entry.'
) ENGINE = MergeTree
PRIMARY KEY (`project_id`, `timestamp`, `instance`) ORDER BY (`project_id`, `timestamp`, `instance`) PARTITION BY (toYYYYMMDD(timestamp)) TTL timestamp + toIntervalDay(30) SETTINGS index_granularity = 8192 COMMENT 'Stores logs from Gram function executions';
