-- Clear any data the MV captured between its creation and this backfill
TRUNCATE TABLE chat_token_summaries;

-- Backfill chat_token_summaries with existing telemetry_logs data (bounded by
-- the 30-day raw TTL — older history is unrecoverable, which is why this
-- summary table exists going forward)
INSERT INTO chat_token_summaries
SELECT
    gram_project_id,
    toString(attributes.gen_ai.conversation.id) AS chat_id,
    toStartOfDay(fromUnixTimestamp64Nano(time_unix_nano)) AS time_bucket,
    sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gen_ai.usage.total_tokens) != '') AS total_tokens,
    toUInt64(countIf(
        startsWith(gram_urn, 'tools:')
        OR toString(attributes.gram.tool.urn) != ''
        OR toString(attributes.gram.event.source) != ''
        OR toString(attributes.gen_ai.usage.total_tokens) = ''
    )) AS stored_event_count
FROM telemetry_logs
WHERE toString(attributes.gen_ai.conversation.id) != ''
GROUP BY gram_project_id, chat_id, time_bucket;
