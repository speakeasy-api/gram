-- Hand-edited (not atlas's DROP+recreate): add hook_source as a sort-key
-- dimension to "chat_token_summaries" NON-destructively, so billing reads can
-- scope token sums to the registered billed completion surfaces
-- (billing.ModelUsageSources in Go). Assistants completions are tagged
-- "assistants" but deliberately unregistered — Speakeasy covers assistants
-- inference until BYOK, so they must stop counting toward tokens under
-- management. ADD COLUMN and MODIFY ORDER BY must be one ALTER statement —
-- ClickHouse only lets MODIFY ORDER BY extend the key with columns added in
-- the same ALTER. Existing aggregates are preserved (no DROP TABLE).
ALTER TABLE `chat_token_summaries`
  ADD COLUMN `hook_source` String,
  MODIFY ORDER BY (`gram_project_id`, `chat_id`, `time_bucket`, `hook_source`);
-- Drop "chat_token_summaries_mv" view
DROP VIEW `chat_token_summaries_mv`;
-- Create "chat_token_summaries_mv" view with hook_source in the grouping key
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
-- Rewrite the recent, still-rebuildable window so pre-migration rows carry
-- hook_source: unsealed billing cycles must stop counting assistants tokens
-- ingested before this migration ran. 45 days covers any unsealed cycle
-- (a cycle is at most 31 days plus the 72h finalize grace) and sits safely
-- inside the 90-day raw telemetry TTL; sealed cycles read their finalized
-- Postgres snapshots and are unaffected. Rows older than the window keep
-- hook_source '' and are grandfathered as billed by the read path.
-- mutations_sync=2 makes the DELETE complete before the INSERT re-derives.
ALTER TABLE `chat_token_summaries` DELETE WHERE time_bucket >= toStartOfDay(now('UTC') - INTERVAL 45 DAY) SETTINGS mutations_sync = 2;
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
  AND fromUnixTimestamp64Nano(time_unix_nano, 'UTC') >= toStartOfDay(now('UTC') - INTERVAL 45 DAY)
GROUP BY gram_project_id, chat_id, time_bucket, hook_source;
