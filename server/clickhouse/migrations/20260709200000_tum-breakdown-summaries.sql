-- tum_breakdown_summaries: the DIMENSIONED billing aggregate. One row per
-- (project, day, chat, surface, model, user, division, roles) carrying the
-- billed token split, sourced from the same gen_ai completion rows that
-- chat_token_summaries (the billing record) sums. The billing page's
-- breakdowns read this table with the same read-time stored-session
-- qualification (via chat_token_summaries) and the same registry-driven
-- source scoping (billing.ModelUsageSources), so every breakdown slice sums
-- to the billed totals exactly. attribute_metrics_summaries stopped
-- ingesting gram-server completions when it went provenance-first (fleet
-- surfaces only), so this is the only dimensioned home for the billed
-- population. Identity dimensions (email, division, roles) are stamped on
-- completion rows at emit time by the telemetry logger's directory snapshot.
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
-- Backfill BEFORE creating the MV (same MV-last pattern as the
-- chat_token_summaries rewrite: an MV created first would double-count rows
-- ingested while the backfill scans). Pass 1 covers the bulk up to a static
-- split shortly before the expected deploy; its duration does not extend the
-- loss window at the end. The 2026-05-25 start matches the billing rewrite
-- window and covers the June cycle onward; older cycles keep exact totals
-- (snapshots + chat_token_summaries) but have no dimensional breakdowns —
-- their raw logs are past the 90-day TTL and cannot be re-derived.
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
-- Pass 2: everything since the split — a small scan, keeping the gap between
-- this snapshot and the MV creation below in the seconds (a bounded
-- undercount, the safe failure direction for billing data). The two passes
-- partition rows by event time exactly; a slipped deploy just makes this
-- pass scan a little more.
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
-- Create "tum_breakdown_summaries_mv" view — LAST, closing the backfill.
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
