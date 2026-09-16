-- reverse: drop "chat_token_summaries" table
CREATE TABLE `gram`.`chat_token_summaries` (
  `gram_project_id` UUID,
  `chat_id` String,
  `time_bucket` DateTime('UTC'),
  `total_tokens` SimpleAggregateFunction(sum, Int64),
  `stored_event_count` SimpleAggregateFunction(sum, UInt64),
  `hook_source` String
) ENGINE = AggregatingMergeTree
PRIMARY KEY (`gram_project_id`, `chat_id`, `time_bucket`) ORDER BY (`gram_project_id`, `chat_id`, `time_bucket`, `hook_source`) TTL time_bucket + toIntervalDay(730) SETTINGS index_granularity = 8192 COMMENT 'Per-chat daily token usage and stored-session evidence, retained beyond the raw telemetry TTL to support tokens-under-management billing across historical billing cycles';
-- reverse: drop "billing_meter_readings" table
CREATE TABLE `gram`.`billing_meter_readings` (
  `id` UUID COMMENT 'Deterministic reading UUID stable across redelivery.',
  `organization_id` String COMMENT 'Organization that owns the workload.',
  `project_id` UUID COMMENT 'Project that owns the workload.',
  `meter_id` LowCardinality(String) COMMENT 'Registered workload meter identifier.',
  `operation_id` String COMMENT 'Domain operation that produced the reading.',
  `unit` LowCardinality(String) COMMENT 'Measurement unit.',
  `value` Int64 COMMENT 'Signed workload value where usage is positive and adjustments may be positive or negative.',
  `occurred_at` DateTime64(9, 'UTC') COMMENT 'Usage-effective UTC time when the metered work executed and the timestamp used for billing periods.',
  `produced_at` DateTime64(9, 'UTC') COMMENT 'UTC time when the producer created this reading variant and the ReplacingMergeTree version.',
  `inserted_at` DateTime64(9, 'UTC') DEFAULT now64(9) COMMENT 'UTC time when ClickHouse received the row for delivery-lag diagnostics.',
  `corrects_reading_id` Nullable(UUID) COMMENT 'Original reading corrected by this immutable adjustment.',
  `reading_kind` LowCardinality(String) MATERIALIZED if(corrects_reading_id IS NULL, 'usage', 'adjustment') COMMENT 'Derived row kind based on whether the reading corrects an earlier reading.',
  `attributes` Map(String, String) COMMENT 'Additional producer-supplied reading dimensions.',
  `tokenizer_codec` LowCardinality(String) MATERIALIZED attributes['codec'] COMMENT 'Tokenizer codec promoted from attributes for billing analysis.',
  `measurement_method` LowCardinality(String) DEFAULT if(unit = 'stokens', 'tiktoken_o200k_base', '') COMMENT 'Rating-critical measurement implementation.',
  INDEX `idx_billing_meter_readings_occurred_at` ((occurred_at)) TYPE minmax,
  CONSTRAINT `identity_valid` CHECK ((id != toUUID('00000000-0000-0000-0000-000000000000')) AND (project_id != toUUID('00000000-0000-0000-0000-000000000000')) AND notEmpty(trimBoth(organization_id)) AND notEmpty(trimBoth(meter_id)) AND notEmpty(trimBoth(operation_id))),
  CONSTRAINT `value_kind_valid` CHECK (((corrects_reading_id IS NULL) AND (value > 0)) OR ((corrects_reading_id IS NOT NULL) AND (value != 0))),
  CONSTRAINT `correction_id_valid` CHECK ((corrects_reading_id IS NULL) OR ((corrects_reading_id != toUUID('00000000-0000-0000-0000-000000000000')) AND (corrects_reading_id != id)))
) ENGINE = ReplacingMergeTree(produced_at)
PRIMARY KEY (`organization_id`, `meter_id`, `project_id`) ORDER BY (`organization_id`, `meter_id`, `project_id`, `id`) PARTITION BY (cityHash64(toString(id)) % 64) SETTINGS index_granularity = 8192 COMMENT 'Raw usage ledger with producer-time stable-id convergence and billing reads requiring FINAL or equivalent id deduplication';
-- reverse: drop "chat_token_summaries_mv" view
CREATE MATERIALIZED VIEW `gram`.`chat_token_summaries_mv` TO `gram`.`chat_token_summaries` AS SELECT gram_project_id, chat_id, toStartOfDay(fromUnixTimestamp64Nano(time_unix_nano, 'UTC')) AS time_bucket, hook_source, sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gen_ai.usage.total_tokens) != '') AS total_tokens, toUInt64(countIf(startsWith(gram_urn, 'tools:') OR (urn != '') OR (event_source != '') OR (toString(attributes.gen_ai.usage.total_tokens) = ''))) AS stored_event_count FROM gram.telemetry_logs WHERE chat_id != '' GROUP BY gram_project_id, chat_id, time_bucket, hook_source;
