-- Hand-edited (not atlas's DROP+recreate): add hook_source as a sort-key
-- dimension to "chat_token_summaries" NON-destructively, so billing reads can
-- scope token sums to the registered billed completion surfaces
-- (billing.ModelUsageSources in Go). Assistants completions are tagged
-- "assistants" but deliberately unregistered — Speakeasy covers assistants
-- inference until BYOK, so they must stop counting toward tokens under
-- management. ADD COLUMN and MODIFY ORDER BY must be one ALTER statement —
-- ClickHouse only lets MODIFY ORDER BY extend the key with columns added in
-- the same ALTER. Existing aggregates are preserved (no DROP TABLE).
--
-- Ordering note: the MV stays ABSENT for the whole rewrite below and is
-- recreated LAST. Recreating it first would double-count live traffic into
-- the billing record — the MV materializes rows that the delete mutation
-- (which only covers parts existing at its creation) never removes, and the
-- re-derive INSERT then snapshots those same raw rows again. With the MV
-- absent, the failure direction flips to a bounded UNDERCOUNT (rows ingested
-- between the final INSERT's snapshot and the MV creation — a few seconds —
-- never materialize), which is the safe direction for a billing record. The
-- static event-time split below keeps the expensive scan out of that loss
-- window.
ALTER TABLE `chat_token_summaries`
  ADD COLUMN `hook_source` String,
  MODIFY ORDER BY (`gram_project_id`, `chat_id`, `time_bucket`, `hook_source`);
-- Stop the old (untagged) materialization before touching the window.
DROP VIEW `chat_token_summaries_mv`;
-- Rewrite the recent, still-rebuildable window so pre-migration rows carry
-- hook_source: unsealed billing cycles must stop counting assistants tokens
-- ingested before this migration ran. The static 2026-05-25 boundary covers
-- any unsealed cycle at deploy time (a cycle is at most 31 days plus the 72h
-- finalize grace) and sits safely inside the 90-day raw telemetry TTL until
-- late August; sealed cycles read their finalized Postgres snapshots and are
-- unaffected. Rows older than the boundary keep hook_source '' and are
-- grandfathered as billed by the read path. Static literals (not now())
-- keep the DELETE and the INSERTs describing the exact same window even if
-- statements straddle a midnight. mutations_sync=2 completes the DELETE
-- before the re-derives run.
ALTER TABLE `chat_token_summaries` DELETE WHERE time_bucket >= toDateTime('2026-05-25 00:00:00', 'UTC') SETTINGS mutations_sync = 2;
-- Re-derive pass 1: the bulk of the window, up to a static split shortly
-- before the expected deploy. This is the expensive telemetry_logs scan; it
-- runs while the MV is already gone, so its duration does not extend the
-- loss window at the end.
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
  AND fromUnixTimestamp64Nano(time_unix_nano, 'UTC') < toDateTime('2026-07-10 00:00:00', 'UTC')
GROUP BY gram_project_id, chat_id, time_bucket, hook_source;
-- Re-derive pass 2: everything since the split — a small scan, so the gap
-- between this snapshot and the MV creation below stays in the seconds. The
-- two passes partition rows by event time exactly (< / >= the same literal),
-- so their contributions never overlap; if the deploy slips, this pass just
-- scans a little more.
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
  AND fromUnixTimestamp64Nano(time_unix_nano, 'UTC') >= toDateTime('2026-07-10 00:00:00', 'UTC')
GROUP BY gram_project_id, chat_id, time_bucket, hook_source;
-- Create "chat_token_summaries_mv" view with hook_source in the grouping
-- key — LAST, closing the rewrite (see the ordering note above).
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
