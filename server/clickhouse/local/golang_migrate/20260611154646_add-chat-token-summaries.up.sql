-- drop "chat_token_summaries_mv" view
DROP VIEW `chat_token_summaries_mv`;
-- create "chat_token_summaries_mv" view
CREATE MATERIALIZED VIEW `chat_token_summaries_mv` TO `chat_token_summaries` AS SELECT gram_project_id, chat_id, toStartOfDay(fromUnixTimestamp64Nano(time_unix_nano, 'UTC')) AS time_bucket, sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gen_ai.usage.total_tokens) != '') AS total_tokens, toUInt64(countIf(startsWith(gram_urn, 'tools:') OR (urn != '') OR (event_source != '') OR (toString(attributes.gen_ai.usage.total_tokens) = ''))) AS stored_event_count FROM telemetry_logs WHERE chat_id != '' GROUP BY gram_project_id, chat_id, time_bucket;
