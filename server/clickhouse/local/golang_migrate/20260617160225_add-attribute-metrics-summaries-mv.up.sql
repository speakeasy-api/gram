-- create "attribute_metrics_summaries" table
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
  `provider` String,
  `roles` Array(String),
  `groups` Array(String),
  `total_chats` AggregateFunction(uniqExactIf, String, UInt8),
  `total_input_tokens` AggregateFunction(sumIf, Int64, UInt8),
  `total_output_tokens` AggregateFunction(sumIf, Int64, UInt8),
  `total_tokens` AggregateFunction(sumIf, Int64, UInt8),
  `cache_read_input_tokens` AggregateFunction(sumIf, Int64, UInt8),
  `cache_creation_input_tokens` AggregateFunction(sumIf, Int64, UInt8),
  `total_cost` AggregateFunction(sumIf, Float64, UInt8),
  `total_tool_calls` AggregateFunction(countIf, UInt8)
) ENGINE = AggregatingMergeTree
PRIMARY KEY (`gram_project_id`, `time_bucket`, `department_name`, `job_title`, `employee_type`, `division_name`, `cost_center_name`, `user_email`, `model`, `provider`, `roles`, `groups`) ORDER BY (`gram_project_id`, `time_bucket`, `department_name`, `job_title`, `employee_type`, `division_name`, `cost_center_name`, `user_email`, `model`, `provider`, `roles`, `groups`) TTL time_bucket + toIntervalDay(30) SETTINGS index_granularity = 8192 COMMENT 'Pre-aggregated cost/token/usage metrics broken down by user-identity and request dimensions, powering the generic telemetry.query analytics endpoint.';
-- create "attribute_metrics_summaries_mv" view
CREATE MATERIALIZED VIEW `attribute_metrics_summaries_mv` TO `attribute_metrics_summaries` AS SELECT gram_project_id, toStartOfHour(fromUnixTimestamp64Nano(time_unix_nano)) AS time_bucket, toString(attributes.user.attributes.department_name) AS department_name, toString(attributes.user.attributes.job_title) AS job_title, toString(attributes.user.attributes.employee_type) AS employee_type, toString(attributes.user.attributes.division_name) AS division_name, toString(attributes.user.attributes.cost_center_name) AS cost_center_name, user_email AS user_email, toString(attributes.gen_ai.response.model) AS model, hook_source AS provider, CAST(attributes.user.roles, 'Array(String)') AS roles, CAST(attributes.user.groups, 'Array(String)') AS groups, uniqExactIfState(toString(attributes.gen_ai.conversation.id), toString(attributes.gen_ai.conversation.id) != '') AS total_chats, sumIfState(toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens)), toString(attributes.gen_ai.usage.input_tokens) != '') AS total_input_tokens, sumIfState(toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens)), toString(attributes.gen_ai.usage.output_tokens) != '') AS total_output_tokens, sumIfState(toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gen_ai.usage.total_tokens) != '') AS total_tokens, sumIfState(toInt64OrZero(toString(attributes.gen_ai.usage.cache_read.input_tokens)), toString(attributes.gen_ai.usage.cache_read.input_tokens) != '') AS cache_read_input_tokens, sumIfState(toInt64OrZero(toString(attributes.gen_ai.usage.cache_creation.input_tokens)), toString(attributes.gen_ai.usage.cache_creation.input_tokens) != '') AS cache_creation_input_tokens, sumIfState(toFloat64OrZero(toString(attributes.gen_ai.usage.cost)), toString(attributes.gen_ai.usage.cost) != '') AS total_cost, countIfState(startsWith(toString(attributes.gram.tool.urn), 'tools:')) AS total_tool_calls FROM telemetry_logs GROUP BY gram_project_id, time_bucket, department_name, job_title, employee_type, division_name, cost_center_name, user_email, model, provider, roles, groups;
