-- Hand-written to mirror migrations/20260709200000_tum-breakdown-summaries.sql
-- (ported late: PR #4062 added only the Atlas version, leaving the two
-- migration directories drifted). The timestamp is later than the source
-- because golang-migrate only applies versions above a database's current
-- one. SQL is copied verbatim with the narrative comments stripped (some
-- contained semicolons, which break golang-migrate's naive statement
-- splitter). See the Atlas file for the full rationale, including why the
-- MV is created after the backfill and why the windows use static dates.
CREATE TABLE `tum_breakdown_summaries` (
    `gram_project_id` UUID,
    `chat_id` String,
    `time_bucket` DateTime('UTC'),
    `hook_source` String,
    `model` String,
    `user_email` String,
    `division_name` String,
    `roles` Array(String),
    `input_tokens` SimpleAggregateFunction(sum, Int64),
    `output_tokens` SimpleAggregateFunction(sum, Int64),
    `total_tokens` SimpleAggregateFunction(sum, Int64)
) ENGINE = AggregatingMergeTree
ORDER BY (`gram_project_id`, `time_bucket`, `chat_id`, `hook_source`, `model`, `user_email`, `division_name`, `roles`)
TTL time_bucket + INTERVAL 730 DAY
SETTINGS index_granularity = 8192
COMMENT 'Per-chat daily billed token usage broken down by consuming surface and user identity, retained beyond the raw telemetry TTL to power the billing page breakdowns across historical billing cycles';
INSERT INTO `tum_breakdown_summaries` (gram_project_id, chat_id, time_bucket, hook_source, model, user_email, division_name, roles, input_tokens, output_tokens, total_tokens)
SELECT
    gram_project_id,
    chat_id,
    toStartOfDay(fromUnixTimestamp64Nano(time_unix_nano, 'UTC')) AS time_bucket,
    hook_source,
    toString(attributes.gen_ai.response.model) AS model,
    user_email,
    toString(attributes.user.attributes.division_name) AS division_name,
    arraySort(JSONExtract(ifNull(toJSONString(attributes.user.roles), '[]'), 'Array(String)')) AS roles,
    sum(toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens))) AS input_tokens,
    sum(toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens))) AS output_tokens,
    sum(toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens))) AS total_tokens
FROM telemetry_logs
WHERE chat_id != ''
  AND toString(attributes.gen_ai.usage.total_tokens) != ''
  AND fromUnixTimestamp64Nano(time_unix_nano, 'UTC') >= toDateTime('2026-05-25 00:00:00', 'UTC')
  AND fromUnixTimestamp64Nano(time_unix_nano, 'UTC') < toDateTime('2026-07-08 00:00:00', 'UTC')
GROUP BY gram_project_id, chat_id, time_bucket, hook_source, model, user_email, division_name, roles;
INSERT INTO `tum_breakdown_summaries` (gram_project_id, chat_id, time_bucket, hook_source, model, user_email, division_name, roles, input_tokens, output_tokens, total_tokens)
SELECT
    gram_project_id,
    chat_id,
    toStartOfDay(fromUnixTimestamp64Nano(time_unix_nano, 'UTC')) AS time_bucket,
    hook_source,
    toString(attributes.gen_ai.response.model) AS model,
    user_email,
    toString(attributes.user.attributes.division_name) AS division_name,
    arraySort(JSONExtract(ifNull(toJSONString(attributes.user.roles), '[]'), 'Array(String)')) AS roles,
    sum(toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens))) AS input_tokens,
    sum(toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens))) AS output_tokens,
    sum(toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens))) AS total_tokens
FROM telemetry_logs
WHERE chat_id != ''
  AND toString(attributes.gen_ai.usage.total_tokens) != ''
  AND fromUnixTimestamp64Nano(time_unix_nano, 'UTC') >= toDateTime('2026-07-08 00:00:00', 'UTC')
GROUP BY gram_project_id, chat_id, time_bucket, hook_source, model, user_email, division_name, roles;
CREATE MATERIALIZED VIEW `tum_breakdown_summaries_mv` TO `tum_breakdown_summaries` AS
SELECT
    gram_project_id,
    chat_id,
    toStartOfDay(fromUnixTimestamp64Nano(time_unix_nano, 'UTC')) AS time_bucket,
    hook_source,
    toString(attributes.gen_ai.response.model) AS model,
    user_email,
    toString(attributes.user.attributes.division_name) AS division_name,
    arraySort(JSONExtract(ifNull(toJSONString(attributes.user.roles), '[]'), 'Array(String)')) AS roles,
    sum(toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens))) AS input_tokens,
    sum(toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens))) AS output_tokens,
    sum(toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens))) AS total_tokens
FROM telemetry_logs
WHERE chat_id != ''
  AND toString(attributes.gen_ai.usage.total_tokens) != ''
GROUP BY gram_project_id, chat_id, time_bucket, hook_source, model, user_email, division_name, roles;
