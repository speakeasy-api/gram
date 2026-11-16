-- Organizational Memory: daily LLM cost by org and pipeline stage.
--
-- Each stage tags its completions with its own usage source (hook_source):
--   org-memory-summary  per-chat masked summary judge
--   org-memory-facts    per-chat fact extraction judge
--   org-memory-digest   rollup digests (span many chats, so no chat id)
-- Summary and facts rows carry the judged chat in chat_id, so `chats` counts
-- distinct chats judged per day and `cost_per_chat` is the per-chat unit cost.
--
-- Days are UTC. Read-only. Run against the telemetry_logs database; adjust the window as
-- needed.
SELECT
    toDate(fromUnixTimestamp64Nano(time_unix_nano, 'UTC')) AS day,
    toString(attributes.gram.org.id) AS org_id,
    hook_source AS source,
    count() AS calls,
    uniqExactIf(chat_id, chat_id NOT IN ('', '00000000-0000-0000-0000-000000000000')) AS chats,
    sum(toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens))) AS input_tokens,
    sum(toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens))) AS output_tokens,
    round(sumIf(toFloat64OrZero(toString(attributes.gen_ai.usage.cost)), toString(attributes.gen_ai.usage.cost) != ''), 4) AS cost_usd,
    if(chats > 0, round(cost_usd / chats, 5), NULL) AS cost_per_chat
FROM telemetry_logs
WHERE event_source = 'chat_completion'
  AND hook_source IN ('org-memory-summary', 'org-memory-facts', 'org-memory-digest')
  AND time_unix_nano >= toUnixTimestamp64Nano(toDateTime64(toDate(now('UTC')) - 30, 9, 'UTC'))
GROUP BY day, org_id, source
ORDER BY day DESC, cost_usd DESC;
