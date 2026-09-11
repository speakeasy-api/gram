-- create "billing_meter_readings_by_time" table
CREATE TABLE `gram`.`billing_meter_readings_by_time` (
  `id` UUID COMMENT 'Deterministic reading UUID stable across redelivery.',
  `organization_id` String COMMENT 'Organization that owns the workload.',
  `project_id` UUID COMMENT 'Project that owns the workload.',
  `meter_id` LowCardinality(String) COMMENT 'Registered workload meter identifier.',
  `operation_id` String COMMENT 'Domain operation that produced the reading.',
  `unit` LowCardinality(String) COMMENT 'Measurement unit.',
  `measurement_method` LowCardinality(String) DEFAULT if(unit = 'stokens', 'tiktoken_o200k_base', '') COMMENT 'Rating-critical measurement implementation.',
  `value` Int64 COMMENT 'Signed workload value where usage is positive and adjustments may be positive or negative.',
  `occurred_at` DateTime64(9, 'UTC') COMMENT 'Usage-effective UTC time when the metered work executed and the timestamp used for billing periods.',
  `produced_at` DateTime64(9, 'UTC') COMMENT 'UTC time when the producer created the accepted reading.',
  `inserted_at` DateTime64(9, 'UTC') DEFAULT now64(9) COMMENT 'UTC time when ClickHouse received the row for delivery-lag diagnostics.',
  `corrects_reading_id` Nullable(UUID) COMMENT 'Original reading corrected by this immutable adjustment.',
  `reading_kind` LowCardinality(String) MATERIALIZED if(corrects_reading_id IS NULL, 'usage', 'adjustment') COMMENT 'Derived row kind based on whether the reading corrects an earlier reading.',
  `attributes` Map(String, String) COMMENT 'Additional producer-supplied reading dimensions frozen at acceptance.',
  `tokenizer_codec` LowCardinality(String) MATERIALIZED attributes['codec'] COMMENT 'Tokenizer codec promoted from attributes for billing analysis.',
  CONSTRAINT `identity_valid` CHECK ((id != toUUID('00000000-0000-0000-0000-000000000000')) AND (project_id != toUUID('00000000-0000-0000-0000-000000000000')) AND notEmpty(trimBoth(organization_id)) AND notEmpty(trimBoth(meter_id)) AND notEmpty(trimBoth(operation_id))),
  CONSTRAINT `value_kind_valid` CHECK (((corrects_reading_id IS NULL) AND (value > 0)) OR ((corrects_reading_id IS NOT NULL) AND (value != 0))),
  CONSTRAINT `correction_id_valid` CHECK ((corrects_reading_id IS NULL) OR ((corrects_reading_id != toUUID('00000000-0000-0000-0000-000000000000')) AND (corrects_reading_id != id)))
) ENGINE = ReplacingMergeTree
PRIMARY KEY (`organization_id`, `meter_id`, `occurred_at`) ORDER BY (`organization_id`, `meter_id`, `occurred_at`, `project_id`, `id`) PARTITION BY (toYYYYMM(occurred_at)) SETTINGS index_granularity = 8192 COMMENT 'Time-windowed usage ledger with redelivery convergence requiring FINAL before aggregation';
