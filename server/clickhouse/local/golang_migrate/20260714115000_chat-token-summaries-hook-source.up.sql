-- Hand-written to mirror migrations/20260709191016_chat-token-summaries-hook-source.sql
-- (ported late: PR #4062 added only the Atlas version, leaving the two
-- migration directories drifted). The timestamp is later than the source
-- because golang-migrate only applies versions above a database's current
-- one. SQL is copied from the Atlas file with the narrative comments
-- stripped (some contained semicolons, which break golang-migrate's naive
-- statement splitter) and one behavioral deviation described below. See the
-- Atlas file for the full rationale, including why the MV is recreated last.
--
-- Deviation: the rewrite window's lower bound is
-- greatest(static boundary, reconstructibility horizon) rather than the bare
-- 2026-05-25 literal. The Atlas migration ran in production on a known date
-- where that literal sat safely inside the 90-day telemetry_logs TTL, but
-- this file runs whenever a local database happens to upgrade -- possibly
-- long after raw rows for the window have expired -- while
-- chat_token_summaries (730-day TTL) deliberately outlives the raw logs.
-- An unscoped DELETE would then permanently erase summaries the INSERTs can
-- no longer rebuild. The horizon uses 89 days to keep a one-day margin from
-- the TTL edge, and toStartOfDay keeps it day-aligned so the bucket filter
-- and the event-time filters partition rows identically. Buckets older than
-- the bound keep hook_source '' and are grandfathered by the read path,
-- exactly like rows older than the static boundary. The static 2026-07-08
-- split between the two re-derive passes is likewise replaced with
-- toStartOfDay(now('UTC')): rows ingested after the final pass's snapshot
-- and before the MV recreation are never materialized, so a stale split
-- would let that MV-off loss window grow with every month of lateness,
-- while the dynamic split caps the final pass at one day of raw rows.
-- (If UTC midnight passes between statements a single day-bucket is lost --
-- bounded, undercounting, local-only -- the price of not being able to pin
-- now() across golang-migrate's separately-executed statements. Undercount
-- is the safe failure direction for a billing record, per the Atlas
-- rationale.)
ALTER TABLE `chat_token_summaries`
  ADD COLUMN `hook_source` String,
  MODIFY ORDER BY (`gram_project_id`, `chat_id`, `time_bucket`, `hook_source`);
DROP VIEW `chat_token_summaries_mv`;
ALTER TABLE `chat_token_summaries` DELETE WHERE time_bucket >= greatest(toDateTime('2026-05-25 00:00:00', 'UTC'), toStartOfDay(now('UTC')) - INTERVAL 89 DAY) SETTINGS mutations_sync = 2;
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
  AND fromUnixTimestamp64Nano(time_unix_nano, 'UTC') >= greatest(toDateTime('2026-05-25 00:00:00', 'UTC'), toStartOfDay(now('UTC')) - INTERVAL 89 DAY)
  AND fromUnixTimestamp64Nano(time_unix_nano, 'UTC') < toStartOfDay(now('UTC'))
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
  AND fromUnixTimestamp64Nano(time_unix_nano, 'UTC') >= toStartOfDay(now('UTC'))
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
