-- reverse: drop the billing_mode MV first so nothing references the table or the
-- telemetry_logs.billing_mode column while we tear them down.
DROP VIEW `attribute_metrics_summaries_mv`;
-- reverse: drop the materialized billing_mode column + its index from telemetry_logs.
ALTER TABLE `telemetry_logs` DROP INDEX `idx_telemetry_logs_mat_billing_mode`;
ALTER TABLE `telemetry_logs` DROP COLUMN `billing_mode`;
-- reverse: recreate "attribute_metrics_summaries" WITHOUT billing_mode. A sort-key
-- extension (MODIFY ORDER BY) cannot be reversed via ALTER (it only extends the
-- key), so the only way back to the account_type/provider table is drop + recreate.
-- This wipes aggregated history on rollback. Done BEFORE the MV is recreated so the
-- MV is always created against the restored table (never a replaced destination).
DROP TABLE `attribute_metrics_summaries`;
CREATE TABLE `attribute_metrics_summaries` (
  `gram_project_id` UUID,
  `time_bucket` DateTime('UTC'),
  `department_name` String,
  `job_title` String,
  `employee_type` String,
  `division_name` String,
  `cost_center_name` String,
  `user_email` String,
  `model` String,
  `hook_source` String,
  `roles` Array(String),
  `groups` Array(String),
  `total_chats` AggregateFunction(uniqExactIf, String, UInt8),
  `total_input_tokens` AggregateFunction(sumIf, Int64, UInt8),
  `total_output_tokens` AggregateFunction(sumIf, Int64, UInt8),
  `total_tokens` AggregateFunction(sumIf, Int64, UInt8),
  `cache_read_input_tokens` AggregateFunction(sumIf, Int64, UInt8),
  `cache_creation_input_tokens` AggregateFunction(sumIf, Int64, UInt8),
  `total_cost` AggregateFunction(sumIf, Float64, UInt8),
  `total_tool_calls` AggregateFunction(countIf, UInt8),
  `account_type` String,
  `provider` String
) ENGINE = AggregatingMergeTree
PRIMARY KEY (`gram_project_id`, `time_bucket`, `department_name`, `job_title`, `employee_type`, `division_name`, `cost_center_name`, `user_email`, `model`, `hook_source`, `roles`, `groups`) ORDER BY (`gram_project_id`, `time_bucket`, `department_name`, `job_title`, `employee_type`, `division_name`, `cost_center_name`, `user_email`, `model`, `hook_source`, `roles`, `groups`, `account_type`, `provider`) TTL time_bucket + toIntervalDay(30) SETTINGS index_granularity = 8192 COMMENT 'Pre-aggregated cost/token/usage metrics broken down by user-identity and request dimensions, powering the generic telemetry.query analytics endpoint.';
-- reverse: recreate the MV WITHOUT billing_mode (account_type + provider only),
-- now that the target table is back to its pre-billing_mode shape.
CREATE MATERIALIZED VIEW `attribute_metrics_summaries_mv` TO `attribute_metrics_summaries` AS WITH toUnixTimestamp64Nano(toDateTime64('2026-06-20 00:00:00', 9, 'UTC')) AS attribute_metrics_cutoff_unix_nano SELECT gram_project_id, toStartOfHour(fromUnixTimestamp64Nano(time_unix_nano)) AS time_bucket, toString(attributes.user.attributes.department_name) AS department_name, toString(attributes.user.attributes.job_title) AS job_title, toString(attributes.user.attributes.employee_type) AS employee_type, toString(attributes.user.attributes.division_name) AS division_name, toString(attributes.user.attributes.cost_center_name) AS cost_center_name, user_email AS user_email, toString(attributes.gen_ai.response.model) AS model, hook_source, arraySort(JSONExtract(ifNull(toJSONString(attributes.user.roles), '[]'), 'Array(String)')) AS roles, arraySort(JSONExtract(ifNull(toJSONString(attributes.user.groups), '[]'), 'Array(String)')) AS groups, uniqExactIfState(toString(attributes.gen_ai.conversation.id), toString(attributes.gen_ai.conversation.id) != '') AS total_chats, sumIfState(toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens)), toString(attributes.gen_ai.usage.input_tokens) != '') AS total_input_tokens, sumIfState(toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens)), toString(attributes.gen_ai.usage.output_tokens) != '') AS total_output_tokens, sumIfState(toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gen_ai.usage.total_tokens) != '') AS total_tokens, sumIfState(toInt64OrZero(toString(attributes.gen_ai.usage.cache_read.input_tokens)), toString(attributes.gen_ai.usage.cache_read.input_tokens) != '') AS cache_read_input_tokens, sumIfState(toInt64OrZero(toString(attributes.gen_ai.usage.cache_creation.input_tokens)), toString(attributes.gen_ai.usage.cache_creation.input_tokens) != '') AS cache_creation_input_tokens, sumIfState(toFloat64OrZero(toString(attributes.gen_ai.usage.cost)), toString(attributes.gen_ai.usage.cost) != '') AS total_cost, countIfState((toString(attributes.gram.tool.name) != '') AND (toString(attributes.gram.tool.name) NOT IN ('claude-code', 'codex', 'cursor')) AND (toString(attributes.gram.hook.event) IN ('PostToolUse', 'PostToolUseFailure'))) AS total_tool_calls, account_type, provider FROM telemetry_logs WHERE (time_unix_nano >= attribute_metrics_cutoff_unix_nano) AND (startsWith(gram_urn, 'claude-code:usage') OR startsWith(gram_urn, 'codex:usage') OR startsWith(gram_urn, 'cursor:usage') OR ((toString(attributes.gen_ai.operation.name) = 'chat') AND (toString(attributes.gen_ai.usage.cost) != '')) OR ((toString(attributes.gram.tool.name) != '') AND (toString(attributes.gram.tool.name) NOT IN ('claude-code', 'codex', 'cursor')))) GROUP BY gram_project_id, time_bucket, department_name, job_title, employee_type, division_name, cost_center_name, user_email, model, hook_source, roles, groups, account_type, provider;
