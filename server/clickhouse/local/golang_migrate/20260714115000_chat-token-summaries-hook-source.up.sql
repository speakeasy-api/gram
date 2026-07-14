-- Hand-written to mirror migrations/20260709191016_chat-token-summaries-hook-source.sql
-- (ported late: PR #4062 added only the Atlas version, leaving the two
-- migration directories drifted). The timestamp is later than the source
-- because golang-migrate only applies versions above a database's current
-- one. SQL is copied verbatim with the narrative comments stripped (some
-- contained semicolons, which break golang-migrate's naive statement
-- splitter). See the Atlas file for the full rationale, including why the
-- MV is recreated last and why the backfill uses static date windows.
ALTER TABLE `chat_token_summaries`
  ADD COLUMN `hook_source` String,
  MODIFY ORDER BY (`gram_project_id`, `chat_id`, `time_bucket`, `hook_source`);
DROP VIEW `chat_token_summaries_mv`;
ALTER TABLE `chat_token_summaries` DELETE WHERE time_bucket >= toDateTime('2026-05-25 00:00:00', 'UTC') SETTINGS mutations_sync = 2;
INSERT INTO `chat_token_summaries` (gram_project_id, chat_id, time_bucket, hook_source, total_tokens, stored_event_count)
SELECT
    gram_project_id,
    chat_id,
    toStartOfDay(fromUnixTimestamp64Nano(time_unix_nano, 'UTC')) AS time_bucket,
    hook_source,
    sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gen_ai.usage.total_tokens) != '') AS total_tokens,
    toUInt64(countIf(
        startsWith(gram_urn, 'tools:')
        OR urn != ''
        OR event_source != ''
        OR toString(attributes.gen_ai.usage.total_tokens) = ''
    )) AS stored_event_count
FROM telemetry_logs
WHERE chat_id != ''
  AND fromUnixTimestamp64Nano(time_unix_nano, 'UTC') >= toDateTime('2026-05-25 00:00:00', 'UTC')
  AND fromUnixTimestamp64Nano(time_unix_nano, 'UTC') < toDateTime('2026-07-08 00:00:00', 'UTC')
GROUP BY gram_project_id, chat_id, time_bucket, hook_source;
INSERT INTO `chat_token_summaries` (gram_project_id, chat_id, time_bucket, hook_source, total_tokens, stored_event_count)
SELECT
    gram_project_id,
    chat_id,
    toStartOfDay(fromUnixTimestamp64Nano(time_unix_nano, 'UTC')) AS time_bucket,
    hook_source,
    sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gen_ai.usage.total_tokens) != '') AS total_tokens,
    toUInt64(countIf(
        startsWith(gram_urn, 'tools:')
        OR urn != ''
        OR event_source != ''
        OR toString(attributes.gen_ai.usage.total_tokens) = ''
    )) AS stored_event_count
FROM telemetry_logs
WHERE chat_id != ''
  AND fromUnixTimestamp64Nano(time_unix_nano, 'UTC') >= toDateTime('2026-07-08 00:00:00', 'UTC')
GROUP BY gram_project_id, chat_id, time_bucket, hook_source;
CREATE MATERIALIZED VIEW `chat_token_summaries_mv` TO `chat_token_summaries` AS
SELECT
    gram_project_id,
    chat_id,
    toStartOfDay(fromUnixTimestamp64Nano(time_unix_nano, 'UTC')) AS time_bucket,
    hook_source,
    sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gen_ai.usage.total_tokens) != '') AS total_tokens,
    toUInt64(countIf(
        startsWith(gram_urn, 'tools:')
        OR urn != ''
        OR event_source != ''
        OR toString(attributes.gen_ai.usage.total_tokens) = ''
    )) AS stored_event_count
FROM telemetry_logs
WHERE chat_id != ''
GROUP BY gram_project_id, chat_id, time_bucket, hook_source;
