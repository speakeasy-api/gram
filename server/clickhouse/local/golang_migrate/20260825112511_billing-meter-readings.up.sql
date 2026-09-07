-- create "billing_meter_readings" table
CREATE TABLE `billing_meter_readings` (
  `id` UUID COMMENT 'Deterministic reading UUID stable across redelivery.',
  `organization_id` String COMMENT 'Organization that owns the workload.',
  `project_id` UUID COMMENT 'Project that owns the workload.',
  `meter_id` LowCardinality(String) COMMENT 'Registered workload meter identifier.',
  `operation_id` String COMMENT 'Domain operation that produced the reading.',
  `unit` LowCardinality(String) COMMENT 'Measurement unit, currently stokens.',
  `value` Int64 COMMENT 'Signed workload value where usage is positive and adjustments may be positive or negative.',
  `occurred_at` DateTime64(9, 'UTC') COMMENT 'Usage-effective UTC time when the metered work executed.',
  `inserted_at` DateTime64(9, 'UTC') DEFAULT now64(9) COMMENT 'ClickHouse ingestion time and ReplacingMergeTree version.',
  `corrects_reading_id` Nullable(UUID) COMMENT 'Original reading corrected by this immutable adjustment.',
  `reading_kind` LowCardinality(String) MATERIALIZED if(isNull(corrects_reading_id), 'usage', 'adjustment') COMMENT 'Derived row kind based on whether the reading corrects an earlier reading.',
  `attributes` Map(String, String) COMMENT 'Additional producer-supplied reading dimensions.',
  `tokenizer_codec` LowCardinality(String) MATERIALIZED attributes['codec'] COMMENT 'Tokenizer codec promoted from attributes for billing analysis.',
  CONSTRAINT `identity_valid` CHECK id != toUUID('00000000-0000-0000-0000-000000000000') AND project_id != toUUID('00000000-0000-0000-0000-000000000000') AND notEmpty(trimBoth(organization_id)) AND notEmpty(trimBoth(meter_id)) AND notEmpty(trimBoth(operation_id)),
  CONSTRAINT `value_kind_valid` CHECK (isNull(corrects_reading_id) AND value > 0) OR (isNotNull(corrects_reading_id) AND value != 0),
  CONSTRAINT `correction_id_valid` CHECK isNull(corrects_reading_id) OR (corrects_reading_id != toUUID('00000000-0000-0000-0000-000000000000') AND corrects_reading_id != id),
  CONSTRAINT `measurement_valid` CHECK unit = 'stokens' AND tokenizer_codec = 'tiktoken_o200k_base',
  INDEX `idx_billing_meter_readings_occurred_at` ((occurred_at)) TYPE minmax GRANULARITY 1
) ENGINE = ReplacingMergeTree(inserted_at)
PRIMARY KEY (`organization_id`, `meter_id`, `project_id`) ORDER BY (`organization_id`, `meter_id`, `project_id`, `id`) PARTITION BY (cityHash64(toString(id)) % 64) SETTINGS index_granularity = 8192 COMMENT 'Raw immutable s-token usage ledger with stable-id convergence and billing reads requiring FINAL or equivalent id deduplication';
