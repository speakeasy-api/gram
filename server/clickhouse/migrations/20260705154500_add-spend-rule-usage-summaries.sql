-- Create "spend_rule_usage_summaries" table
CREATE TABLE `spend_rule_usage_summaries` (
  `gram_project_id` UUID,
  `user_email` String,
  `time_bucket` DateTime('UTC'),
  `total_cost` Float64
) ENGINE = SummingMergeTree
ORDER BY (`gram_project_id`, `user_email`, `time_bucket`) TTL time_bucket + toIntervalDay(400) SETTINGS index_granularity = 8192 COMMENT 'Minute-grained per-user LLM cost rollup for spend-rule evaluation.';
-- Backfill current raw telemetry into the spend-rule rollup
INSERT INTO `spend_rule_usage_summaries` (`gram_project_id`, `user_email`, `time_bucket`, `total_cost`)
WITH
  ((chat_id != '') AND (toString(attributes.prompt.id) != '') AND ((toString(attributes.event.name) = 'api_request') OR (body = 'claude_code.api_request')) AND ((service_name = 'claude-code') OR (toString(resource_attributes.service.name) = 'claude-code') OR startsWith(body, 'claude_code.'))) AS is_claude_api_request,
  (startsWith(gram_urn, 'codex:usage') OR startsWith(gram_urn, 'cursor:usage') OR ((toString(attributes.gen_ai.operation.name) = 'chat') AND (toString(attributes.gen_ai.usage.cost) != '') AND NOT is_claude_api_request AND NOT startsWith(gram_urn, 'claude-code:usage'))) AS is_generic_usage_row
SELECT
  gram_project_id,
  user_email,
  toStartOfMinute(fromUnixTimestamp64Nano(time_unix_nano)) AS time_bucket,
  sum(if(is_claude_api_request, multiIf(toString(attributes.cost_usd) != '', toFloat64OrZero(toString(attributes.cost_usd)), toString(attributes.cost_usd_micros) != '', toFloat64OrZero(toString(attributes.cost_usd_micros)) / 1000000, 0), toFloat64OrZero(toString(attributes.gen_ai.usage.cost)))) AS total_cost
FROM `telemetry_logs`
WHERE (user_email != '') AND (is_claude_api_request OR is_generic_usage_row)
GROUP BY gram_project_id, user_email, time_bucket;
-- Create "spend_rule_usage_summaries_mv" view
CREATE MATERIALIZED VIEW `spend_rule_usage_summaries_mv` TO `spend_rule_usage_summaries` AS
WITH
  ((chat_id != '') AND (toString(attributes.prompt.id) != '') AND ((toString(attributes.event.name) = 'api_request') OR (body = 'claude_code.api_request')) AND ((service_name = 'claude-code') OR (toString(resource_attributes.service.name) = 'claude-code') OR startsWith(body, 'claude_code.'))) AS is_claude_api_request,
  (startsWith(gram_urn, 'codex:usage') OR startsWith(gram_urn, 'cursor:usage') OR ((toString(attributes.gen_ai.operation.name) = 'chat') AND (toString(attributes.gen_ai.usage.cost) != '') AND NOT is_claude_api_request AND NOT startsWith(gram_urn, 'claude-code:usage'))) AS is_generic_usage_row
SELECT
  gram_project_id,
  user_email,
  toStartOfMinute(fromUnixTimestamp64Nano(time_unix_nano)) AS time_bucket,
  sum(if(is_claude_api_request, multiIf(toString(attributes.cost_usd) != '', toFloat64OrZero(toString(attributes.cost_usd)), toString(attributes.cost_usd_micros) != '', toFloat64OrZero(toString(attributes.cost_usd_micros)) / 1000000, 0), toFloat64OrZero(toString(attributes.gen_ai.usage.cost)))) AS total_cost
FROM `telemetry_logs`
WHERE (user_email != '') AND (is_claude_api_request OR is_generic_usage_row)
GROUP BY gram_project_id, user_email, time_bucket;
