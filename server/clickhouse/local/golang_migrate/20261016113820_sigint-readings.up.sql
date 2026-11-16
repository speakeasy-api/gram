-- create "sigint_readings" table
CREATE TABLE `gram`.`sigint_readings` (
  `id` UUID COMMENT 'Producer-assigned logical reading UUID, preserved across attempts.
@access: confidential',
  `evaluation_attempt_id` UUID COMMENT 'Evaluation attempt UUID, not the replacement key.
@access: confidential',
  `organization_id` String COMMENT 'Organization owning the source event.
@access: confidential',
  `project_id` UUID COMMENT 'Project owning the source event and sensor.
@access: confidential',
  `sensor_id` UUID COMMENT 'Stable sensor resource identity.
@access: confidential',
  `sensor_slug` String COMMENT 'Evaluation-time customer-defined sensor slug.
@access: opaque-confidential',
  `event_kind` LowCardinality(String) COMMENT 'Producer-defined source event namespace.
@access: opaque-confidential',
  `event_id` String COMMENT 'Opaque source event identity, not necessarily a UUID.
@access: opaque-confidential',
  `event_occurred_at` DateTime64(9, 'UTC') COMMENT 'Immutable source occurrence time.
@access: confidential',
  `evaluated_at` DateTime64(9, 'UTC') COMMENT 'Producer completion time and replacement version.
@access: confidential',
  `inserted_at` DateTime64(9, 'UTC') DEFAULT now64(9) COMMENT 'ClickHouse ingestion time for delivery diagnostics.
@access: confidential',
  `status` LowCardinality(String) DEFAULT 'answered' COMMENT 'Evaluation outcome: answered, not_applicable, draft or failed.
@access: confidential',
  `failure_reason` LowCardinality(String) DEFAULT '' COMMENT 'Bounded evaluator reason for failed outcomes, otherwise empty.
@access: confidential',
  `definition_hash` String COMMENT 'Semantic classifier-definition fingerprint.
@access: confidential',
  `configured_model` LowCardinality(String) COMMENT 'Requested classifier model identifier.
@access: opaque-confidential',
  `models` Array(String) COMMENT 'Provider-reported model identifiers.
@access: opaque-confidential',
  `compiler_version` LowCardinality(String) COMMENT 'Classification compilation version.
@access: confidential',
  `provider` LowCardinality(String) COMMENT 'Configured inference transport.
@access: confidential',
  `input_strategy` LowCardinality(String) DEFAULT '' COMMENT 'Configured message context, or empty when no input was supplied.
@access: confidential',
  `input_requested_message_count` UInt8 DEFAULT 0 COMMENT 'Configured message count including the target.
@access: confidential',
  `input_message_count` UInt8 DEFAULT 0 COMMENT 'Messages supplied including the target, after short history or budget truncation.
@access: confidential',
  `input_truncated` Bool DEFAULT false COMMENT 'Whether the content budget excluded available history.
@access: confidential',
  `event_context_type` LowCardinality(String) DEFAULT '' COMMENT 'Source-context variant.
@access: confidential',
  `conversation_id` Nullable(UUID) COMMENT 'Associated Speakeasy conversation identity.
@access: confidential',
  `conversation_role` LowCardinality(String) DEFAULT '' COMMENT 'Source message role.
@access: confidential',
  `tool_name` String DEFAULT '' COMMENT 'Producer-defined tool name.
@access: opaque-confidential',
  `tool_call_id` String DEFAULT '' COMMENT 'Producer protocol call identifier.
@access: opaque-confidential',
  `mcp_server_id` Nullable(UUID) COMMENT 'Associated Speakeasy MCP server identity.
@access: confidential',
  `actor_user_id` String DEFAULT '' COMMENT 'Observed Speakeasy actor identity.
@access: confidential',
  `actor_external_user_id` String DEFAULT '' COMMENT 'Observed source-system person identity.
@access: confidential-pii',
  `actor_user_email` String DEFAULT '' COMMENT 'Email explicitly observed by ingestion.
@access: confidential-pii',
  `billing_user_id` String DEFAULT '' COMMENT 'Explicit Speakeasy usage allocation identity.
@access: confidential',
  `source` LowCardinality(String) DEFAULT '' COMMENT 'Canonical ingestion surface.
@access: confidential',
  `user_account_id` Nullable(UUID) COMMENT 'Resolved external-account resource identity.
@access: confidential',
  `account_type` LowCardinality(String) DEFAULT '' COMMENT 'Ingestion-time account classification.
@access: confidential',
  `billing_mode` LowCardinality(String) DEFAULT '' COMMENT 'External account billing classification.
@access: confidential',
  `assistant_id` Nullable(UUID) COMMENT 'Associated Speakeasy assistant resource identity.
@access: confidential',
  `replayed` Nullable(Bool) COMMENT 'Historical-ingestion evidence with explicit absence.
@access: confidential',
  `attributes` Map(String, String) COMMENT 'Customer string labels captured with this attempt.
@access: opaque-restricted' CODEC(ZSTD(1)),
  `result_type` LowCardinality(String) DEFAULT '' COMMENT 'Sensor result variant, empty unless answered.
@access: confidential',
  `probabilities` Array(Tuple(`signal_id` UUID, `signal_slug` String, `probability` Float64)) COMMENT 'Complete configured order, including customer signal slugs and probabilities.
@access: opaque-confidential' CODEC(ZSTD(1)),
  `selected_signal_id` Nullable(UUID) COMMENT 'Selected signal identity for choice results.
@access: confidential',
  `selected_signal_slug` String DEFAULT '' COMMENT 'Customer-defined slug of the selected signal.
@access: opaque-confidential',
  `expected_index` Nullable(Float64) COMMENT 'Probability-weighted ordered-score index.
@access: confidential',
  `confidence` Nullable(Float64) COMMENT 'Optional classifier confidence.
@access: confidential'
) ENGINE = ReplacingMergeTree(evaluated_at)
PRIMARY KEY (`organization_id`, `project_id`, `sensor_id`, `event_occurred_at`) ORDER BY (`organization_id`, `project_id`, `sensor_id`, `event_occurred_at`, `id`) PARTITION BY (toYYYYMM(event_occurred_at)) TTL toDateTime(event_occurred_at, 'UTC') + toIntervalYear(2) SETTINGS index_granularity = 8192 COMMENT 'Sensor evaluation outcomes with monthly, source-time-stable replacement and two-year retention';
