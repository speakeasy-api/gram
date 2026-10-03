-- Queries behind docs/research/2026-09-23-tool-response-scan-volume.md (AIS-723).
--
-- Every query is parameterised on a UTC half-open window and on nothing else,
-- so the same text runs against any environment. Pass the window with
-- clickhouse-client --param_from=... --param_to=..., for example:
--
--   clickhouse-client --database gram \
--     --param_from='2026-09-21 00:00:00' --param_to='2026-09-24 00:00:00' \
--     --queries-file docs/research/2026-09-23-tool-response-scan-volume.sql
--
-- They were validated against the local ClickHouse schema with synthetic rows.
-- The absolute numbers in the write-up come from running them on production.


-- 1. Ledger split by execution path.
--
-- The Presidio meter is written from two independent lanes for the same
-- message: inline_batch by the Go worker and shadow_stream by the pystreams
-- consumer. Any Presidio total that does not group by scan_execution_path is
-- adding two scans of one message together. Start every investigation here.
SELECT
    toDate(occurred_at) AS day,
    attributes['scan_execution_path'] AS execution_path,
    count() AS readings,
    sum(value) AS stokens,
    round(avg(value)) AS mean_stokens,
    quantile(0.99)(value) AS p99_stokens,
    max(value) AS max_stokens
FROM billing_meter_readings_by_time
WHERE meter_id = 'gram.risk.scan.presidio'
  AND occurred_at >= {from:DateTime64(9, 'UTC')}
  AND occurred_at < {to:DateTime64(9, 'UTC')}
GROUP BY day, execution_path
ORDER BY day, stokens DESC;


-- 2. Message-type share within each lane.
--
-- Reproduces the "tool_response is ~85% of Presidio stokens" headline, and
-- shows whether the share is the same on both lanes. It is not: the inline
-- lane never meters a message over 50 KiB, so the large tool responses appear
-- only under shadow_stream.
SELECT
    attributes['scan_execution_path'] AS execution_path,
    attributes['message_type'] AS message_type,
    count() AS readings,
    sum(value) AS stokens,
    round(100 * sum(value) / sum(sum(value)) OVER (PARTITION BY execution_path), 1) AS pct_of_lane
FROM billing_meter_readings_by_time
WHERE meter_id = 'gram.risk.scan.presidio'
  AND occurred_at >= {from:DateTime64(9, 'UTC')}
  AND occurred_at < {to:DateTime64(9, 'UTC')}
GROUP BY execution_path, message_type
ORDER BY execution_path, stokens DESC;


-- 3. Which agent surfaces produce the tool-response volume.
SELECT
    attributes['hook_source'] AS hook_source,
    attributes['scan_execution_path'] AS execution_path,
    count() AS readings,
    sum(value) AS stokens,
    round(avg(value)) AS mean_stokens
FROM billing_meter_readings_by_time
WHERE meter_id = 'gram.risk.scan.presidio'
  AND attributes['message_type'] = 'tool_response'
  AND occurred_at >= {from:DateTime64(9, 'UTC')}
  AND occurred_at < {to:DateTime64(9, 'UTC')}
GROUP BY hook_source, execution_path
ORDER BY stokens DESC;


-- 4. Coverage of the tool_name attribute on tool_response readings.
--
-- Expected result: every tool_response reading has an empty tool_name, because
-- the batch scanner only fills it from a message's own tool_calls array and a
-- tool_response row has none. Per-tool cost analysis has to come from
-- telemetry_logs (queries 5 onward) until that attribution is fixed.
SELECT
    attributes['message_type'] AS message_type,
    attributes['tool_name'] != '' AS has_tool_name,
    count() AS readings,
    sum(value) AS stokens
FROM billing_meter_readings_by_time
WHERE meter_id = 'gram.risk.scan.presidio'
  AND occurred_at >= {from:DateTime64(9, 'UTC')}
  AND occurred_at < {to:DateTime64(9, 'UTC')}
GROUP BY message_type, has_tool_name
ORDER BY message_type, has_tool_name;


-- 5. Tool-response size distribution per tool, from telemetry.
--
-- Hook ingest writes the tool result to gen_ai.tool.call.result without the
-- 64 KiB cap the HTTP/MCP gateway applies, so these lengths are the real
-- stored sizes. `pct_bytes_over_50kib` is the share of a tool's bytes that
-- sits above the inline scanner's cap.
SELECT
    tool_name,
    hook_source,
    count() AS responses,
    quantile(0.5)(length(result)) AS p50_bytes,
    quantile(0.9)(length(result)) AS p90_bytes,
    quantile(0.99)(length(result)) AS p99_bytes,
    max(length(result)) AS max_bytes,
    sum(length(result)) AS total_bytes,
    round(100 * countIf(length(result) > 51200) / count(), 2) AS pct_responses_over_50kib,
    round(100 * sumIf(length(result), length(result) > 51200) / sum(length(result)), 1) AS pct_bytes_over_50kib
FROM (
    SELECT
        tool_name,
        hook_source,
        toString(attributes.gen_ai.tool.call.result) AS result
    FROM telemetry_logs
    WHERE fromUnixTimestamp64Nano(time_unix_nano) >= {from:DateTime64(9, 'UTC')}
      AND fromUnixTimestamp64Nano(time_unix_nano) < {to:DateTime64(9, 'UTC')}
      AND toString(attributes.gram.hook.event) IN ('PostToolUse', 'PostToolUseFailure')
      AND toString(attributes.gen_ai.tool.call.result) != ''
)
GROUP BY tool_name, hook_source
ORDER BY total_bytes DESC
LIMIT 50;


-- 6. Share of every tool-response byte carried by the fields that only restate
--    a file the agent already read.
--
-- Claude Code's Edit response carries `originalFile` and MultiEdit carries
-- `originalFileContents`: the entire pre-edit file, re-sent on every edit.
-- This is the single measurement that decides whether the extraction option in
-- the write-up is worth building.
SELECT
    tool_name,
    count() AS responses,
    sum(length(result)) AS total_bytes,
    sum(length(original_file)) AS echoed_bytes,
    round(100 * sum(length(original_file)) / sum(length(result)), 1) AS pct_echoed
FROM (
    SELECT
        tool_name,
        toString(attributes.gen_ai.tool.call.result) AS result,
        coalesce(
            nullIf(JSONExtractString(result, 'originalFile'), ''),
            nullIf(JSONExtractString(result, 'originalFileContents'), ''),
            ''
        ) AS original_file
    FROM telemetry_logs
    WHERE fromUnixTimestamp64Nano(time_unix_nano) >= {from:DateTime64(9, 'UTC')}
      AND fromUnixTimestamp64Nano(time_unix_nano) < {to:DateTime64(9, 'UTC')}
      AND toString(attributes.gram.hook.event) IN ('PostToolUse', 'PostToolUseFailure')
      AND toString(attributes.gen_ai.tool.call.result) != ''
)
GROUP BY tool_name
HAVING echoed_bytes > 0
ORDER BY echoed_bytes DESC;


-- 7. How much of the volume is literally repeated content.
--
-- Two dedup granularities, both scoped to a chat:
--   whole_message_bytes  keeps one copy of each distinct tool response
--   field_dedup_bytes    keeps every response but one copy of each distinct
--                        originalFile
-- Compare both against total_bytes to size a content-hash cache before
-- building one.
WITH per_row AS (
    SELECT
        chat_id,
        length(result) AS msg_bytes,
        cityHash64(result) AS msg_hash,
        length(echoed) AS echo_bytes,
        cityHash64(echoed) AS echo_hash
    FROM (
        SELECT
            chat_id,
            toString(attributes.gen_ai.tool.call.result) AS result,
            coalesce(
                nullIf(JSONExtractString(result, 'originalFile'), ''),
                nullIf(JSONExtractString(result, 'originalFileContents'), ''),
                ''
            ) AS echoed
        FROM telemetry_logs
        WHERE fromUnixTimestamp64Nano(time_unix_nano) >= {from:DateTime64(9, 'UTC')}
          AND fromUnixTimestamp64Nano(time_unix_nano) < {to:DateTime64(9, 'UTC')}
          AND toString(attributes.gram.hook.event) IN ('PostToolUse', 'PostToolUseFailure')
          AND toString(attributes.gen_ai.tool.call.result) != ''
          AND chat_id != ''
    )
)
SELECT
    totals.total_bytes AS total_bytes,
    messages.distinct_message_bytes AS whole_message_dedup_bytes,
    round(100 * (1 - messages.distinct_message_bytes / totals.total_bytes), 1) AS pct_saved_whole_message,
    totals.total_bytes - totals.echoed_bytes + fields.distinct_echoed_bytes AS field_dedup_bytes,
    round(100 * (totals.echoed_bytes - fields.distinct_echoed_bytes) / totals.total_bytes, 1) AS pct_saved_field_dedup
FROM
    (SELECT sum(msg_bytes) AS total_bytes, sum(echo_bytes) AS echoed_bytes FROM per_row) AS totals
CROSS JOIN
    (SELECT sum(b) AS distinct_message_bytes FROM (SELECT any(msg_bytes) AS b FROM per_row GROUP BY chat_id, msg_hash)) AS messages
CROSS JOIN
    (SELECT sum(b) AS distinct_echoed_bytes FROM (SELECT any(echo_bytes) AS b FROM per_row GROUP BY chat_id, echo_hash)) AS fields;
