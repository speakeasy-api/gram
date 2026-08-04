-- Demo workspace seed — ClickHouse side.
--
-- Regenerates all demo-org telemetry. Every statement is scoped to the fixed
-- demo project UUIDs (telemetry_logs has no org column; the demo project ids
-- ARE the isolation boundary — they must match seed/demo/postgres.sql).
--
-- The MVs (trace/metrics/attribute_metrics/chat_token/chat_session summaries,
-- attribute_keys, spend_rule_usage) fire on INSERT, and a DELETE mutation on
-- telemetry_logs never shrinks their targets — so each target is deleted
-- explicitly below before the fresh insert repopulates it through the MVs.
-- Rows are inserted with recent timestamps (trailing ~12 days), safely past
-- every MV date cutoff, so NO hand-written MV backfill is needed here.
--
-- PROVENANCE MATTERS: attribute_metrics_summaries_mv and
-- chat_session_summaries_mv are provenance-first — they admit ONLY rows from
-- observed agent surfaces (Claude OTEL api_request/tool_result, Codex OTEL,
-- cursor:usage rows, agent PostToolUse hook rows, …). Generic
-- "chat:completion" rows are deliberately excluded. The inserts below emit:
--   * odd-numbered chats  → Claude provenance: claude-code:otel:logs
--     api_request rows (usage/cost) + tool_result rows (tool calls)
--   * even-numbered chats → Cursor provenance: cursor:usage:metrics rows
--     (usage/cost) + PostToolUse hook rows (tool calls)
--   * every chat          → "tools:" urn rows for the tool-logs/traces pages
--     (feed trace_summaries + metrics_summaries only)
--
-- Chat ids reproduce the Postgres formula md5('gram-demo-chat-' || n)::uuid so
-- the ClickHouse telemetry joins the Postgres chats exactly.
--
--   Local: applied by `mise run seed:demo` via clickhouse-client --multiquery.
--   Prod:  run daily by the infra cron AFTER demo.ensure_demo_org() on
--          Postgres (ClickHouse has no procedural functions, hence a script).

SET mutations_sync = 1;

-- Scoped deletes: telemetry source + every MV target.
ALTER TABLE telemetry_logs DELETE WHERE gram_project_id IN
  (toUUID('dec0de00-0000-4000-a000-000000000001'), toUUID('dec0de00-0000-4000-a000-000000000002'));
ALTER TABLE trace_summaries DELETE WHERE gram_project_id IN
  (toUUID('dec0de00-0000-4000-a000-000000000001'), toUUID('dec0de00-0000-4000-a000-000000000002'));
ALTER TABLE metrics_summaries DELETE WHERE gram_project_id IN
  (toUUID('dec0de00-0000-4000-a000-000000000001'), toUUID('dec0de00-0000-4000-a000-000000000002'));
ALTER TABLE attribute_metrics_summaries DELETE WHERE gram_project_id IN
  (toUUID('dec0de00-0000-4000-a000-000000000001'), toUUID('dec0de00-0000-4000-a000-000000000002'));
ALTER TABLE chat_token_summaries DELETE WHERE gram_project_id IN
  (toUUID('dec0de00-0000-4000-a000-000000000001'), toUUID('dec0de00-0000-4000-a000-000000000002'));
ALTER TABLE chat_session_summaries DELETE WHERE gram_project_id IN
  (toUUID('dec0de00-0000-4000-a000-000000000001'), toUUID('dec0de00-0000-4000-a000-000000000002'));
ALTER TABLE spend_rule_usage_summaries DELETE WHERE gram_project_id IN
  (toUUID('dec0de00-0000-4000-a000-000000000001'), toUUID('dec0de00-0000-4000-a000-000000000002'));
ALTER TABLE attribute_keys DELETE WHERE gram_project_id IN
  (toUUID('dec0de00-0000-4000-a000-000000000001'), toUUID('dec0de00-0000-4000-a000-000000000002'));

-- Tool-execution rows for the tool-logs/traces pages: 2 per chat, urn prefixed
-- "tools:". ~7% non-200 for realistic error rates. Not counted by the cost or
-- session MVs (by design).
INSERT INTO telemetry_logs
  (time_unix_nano, observed_time_unix_nano, severity_text, body, trace_id,
   attributes, resource_attributes, gram_project_id, gram_urn, service_name, gram_chat_id)
SELECT
  nano + toUInt64(k * 1000000),
  nano + toUInt64(k * 1000000),
  'INFO',
  concat('Tool call: ', tool_name),
  lower(hex(MD5(concat('gram-demo-trace-', toString(i))))),
  concat(
    '{"gram.tool.urn":"tools:http:acme:', tool_name, '"',
    ',"http.response.status_code":', toString(if((i + k) % 14 = 0, 500, 200)),
    ',"http.server.request.duration":', toString(round(0.05 + (cityHash64(i, k) % 200) / 100, 3)),
    ',"gen_ai.conversation.id":"', chat_id, '"',
    ',"gram.project.id":"', toString(proj), '"',
    ',"user.email":"', email, '"',
    ',"gram.hook.source":"', hook, '"}'
  ),
  '{"gram.deployment.id":"demo-seed"}',
  proj,
  concat('tools:http:acme:', tool_name),
  'gram-mcp-gateway',
  chat_id
FROM (
  SELECT
    number + 1 AS i,
    arrayJoin([1, 2]) AS k,
    lower(hex(MD5(concat('gram-demo-chat-', toString(number + 1))))) AS h,
    concat(substring(h, 1, 8), '-', substring(h, 9, 4), '-', substring(h, 13, 4), '-',
           substring(h, 17, 4), '-', substring(h, 21, 12)) AS chat_id,
    if((number + 1) % 3 = 0,
       toUUID('dec0de00-0000-4000-a000-000000000002'),
       toUUID('dec0de00-0000-4000-a000-000000000001')) AS proj,
    arrayElement(
      ['amara@demo.getgram.ai', 'jonas@demo.getgram.ai', 'priya@demo.getgram.ai',
       'mateo@demo.getgram.ai', 'hana@demo.getgram.ai', 'lucas@demo.getgram.ai'],
      1 + ((number + 1) % 6)) AS email,
    if((number + 1) % 2 = 1, 'claude-code', 'cursor') AS hook,
    arrayElement(['search_logs', 'get_metrics', 'query_db', 'get_customer',
                  'list_deploys', 'process_refund', 'fetch_traces', 'check_health'],
                 1 + (cityHash64('tool', number, arrayJoin([1, 2])) % 8)) AS tool_name,
    toUnixTimestamp64Nano(
      subtractMinutes(subtractHours(now64(9), 5 * toInt64(number + 1)),
                      13 * toInt64((number + 1) % 7))) AS nano
  FROM numbers(60)
);

-- Claude provenance (odd chats): one claude_code.api_request row per turn —
-- urn claude-code:otel:logs + prompt.id + event.name is what the cost and
-- session MVs key usage on. 3 turns per chat.
INSERT INTO telemetry_logs
  (time_unix_nano, observed_time_unix_nano, severity_text, body, trace_id,
   attributes, resource_attributes, gram_project_id, gram_urn, service_name, gram_chat_id)
SELECT
  nano + toUInt64(turn * 60000000000),
  nano + toUInt64(turn * 60000000000),
  'INFO',
  'claude_code.api_request',
  lower(hex(MD5(concat('gram-demo-trace-', toString(i))))),
  concat(
    '{"prompt.id":"demo-prompt-', toString(i), '-', toString(turn), '"',
    ',"event.name":"api_request"',
    ',"input_tokens":', toString(in_tok),
    ',"output_tokens":', toString(out_tok),
    ',"cache_read_tokens":', toString(intDiv(in_tok, 3)),
    ',"cache_creation_tokens":', toString(intDiv(in_tok, 10)),
    ',"cost_usd":', toString(round((in_tok * 3 + out_tok * 15) / 1000000, 6)),
    ',"model":"', model, '"',
    ',"query_source":"user"',
    ',"gen_ai.conversation.id":"', chat_id, '"',
    ',"gram.project.id":"', toString(proj), '"',
    ',"user.email":"', email, '"',
    ',"gram.external_user.id":"', email, '"',
    ',"gram.hook.source":"claude-code"',
    ',"gram.provider":"anthropic"',
    ',"gram.account_type":"team"',
    ',"gram.billing_mode":"metered"}'
  ),
  '{"service.name":"claude-code","gram.deployment.id":"demo-seed"}',
  proj,
  'claude-code:otel:logs',
  'claude-code',
  chat_id
FROM (
  SELECT
    number + 1 AS i,
    arrayJoin([1, 2, 3]) AS turn,
    lower(hex(MD5(concat('gram-demo-chat-', toString(number + 1))))) AS h,
    concat(substring(h, 1, 8), '-', substring(h, 9, 4), '-', substring(h, 13, 4), '-',
           substring(h, 17, 4), '-', substring(h, 21, 12)) AS chat_id,
    if((number + 1) % 3 = 0,
       toUUID('dec0de00-0000-4000-a000-000000000002'),
       toUUID('dec0de00-0000-4000-a000-000000000001')) AS proj,
    arrayElement(
      ['amara@demo.getgram.ai', 'jonas@demo.getgram.ai', 'priya@demo.getgram.ai',
       'mateo@demo.getgram.ai', 'hana@demo.getgram.ai', 'lucas@demo.getgram.ai'],
      1 + ((number + 1) % 6)) AS email,
    if(cityHash64('model', number) % 3 = 0, 'claude-opus-4-5', 'claude-sonnet-4-6') AS model,
    toUInt64(1500 + cityHash64('in', number, arrayJoin([1, 2, 3])) % 6000) AS in_tok,
    toUInt64(200 + cityHash64('out', number, arrayJoin([1, 2, 3])) % 2500) AS out_tok,
    toUnixTimestamp64Nano(
      subtractMinutes(subtractHours(now64(9), 5 * toInt64(number + 1)),
                      13 * toInt64((number + 1) % 7))) AS nano
  FROM numbers(60)
  WHERE (number + 1) % 2 = 1
);

-- Claude provenance (odd chats): one claude_code.tool_result row per tool call
-- (tool_use_id is the dedup identity the MVs count).
INSERT INTO telemetry_logs
  (time_unix_nano, observed_time_unix_nano, severity_text, body, trace_id,
   attributes, resource_attributes, gram_project_id, gram_urn, service_name, gram_chat_id)
SELECT
  nano + toUInt64(k * 61000000000),
  nano + toUInt64(k * 61000000000),
  'INFO',
  'claude_code.tool_result',
  lower(hex(MD5(concat('gram-demo-trace-', toString(i))))),
  concat(
    '{"event.name":"tool_result"',
    ',"tool_use_id":"call_demo_', toString(i), '_', toString(k), '"',
    ',"tool_name":"', tool_name, '"',
    ',"tool_input_size_bytes":', toString(200 + cityHash64('tin', i, k) % 1800),
    ',"tool_result_size_bytes":', toString(500 + cityHash64('tout', i, k) % 8000),
    ',"success":', if((i + k) % 17 = 0, 'false', 'true'),
    ',"gen_ai.conversation.id":"', chat_id, '"',
    ',"gram.project.id":"', toString(proj), '"',
    ',"user.email":"', email, '"',
    ',"gram.hook.source":"claude-code"}'
  ),
  '{"service.name":"claude-code","gram.deployment.id":"demo-seed"}',
  proj,
  'claude-code:otel:logs',
  'claude-code',
  chat_id
FROM (
  SELECT
    number + 1 AS i,
    arrayJoin([1, 2]) AS k,
    lower(hex(MD5(concat('gram-demo-chat-', toString(number + 1))))) AS h,
    concat(substring(h, 1, 8), '-', substring(h, 9, 4), '-', substring(h, 13, 4), '-',
           substring(h, 17, 4), '-', substring(h, 21, 12)) AS chat_id,
    if((number + 1) % 3 = 0,
       toUUID('dec0de00-0000-4000-a000-000000000002'),
       toUUID('dec0de00-0000-4000-a000-000000000001')) AS proj,
    arrayElement(
      ['amara@demo.getgram.ai', 'jonas@demo.getgram.ai', 'priya@demo.getgram.ai',
       'mateo@demo.getgram.ai', 'hana@demo.getgram.ai', 'lucas@demo.getgram.ai'],
      1 + ((number + 1) % 6)) AS email,
    arrayElement(['search_logs', 'get_metrics', 'query_db', 'get_customer',
                  'list_deploys', 'process_refund', 'fetch_traces', 'check_health'],
                 1 + (cityHash64('tool', number, arrayJoin([1, 2])) % 8)) AS tool_name,
    toUnixTimestamp64Nano(
      subtractMinutes(subtractHours(now64(9), 5 * toInt64(number + 1)),
                      13 * toInt64((number + 1) % 7))) AS nano
  FROM numbers(60)
  WHERE (number + 1) % 2 = 1
);

-- Cursor provenance (even chats): one cursor:usage:metrics row per chat —
-- is_agent_usage_row via the urn prefix, usage under gen_ai.usage.*.
INSERT INTO telemetry_logs
  (time_unix_nano, observed_time_unix_nano, severity_text, body, trace_id,
   attributes, resource_attributes, gram_project_id, gram_urn, service_name, gram_chat_id)
SELECT
  nano + 2000000,
  nano + 2000000,
  'INFO',
  'Cursor usage metrics',
  lower(hex(MD5(concat('gram-demo-trace-', toString(i))))),
  concat(
    '{"gen_ai.conversation.id":"', chat_id, '"',
    ',"gen_ai.usage.input_tokens":', toString(in_tok),
    ',"gen_ai.usage.output_tokens":', toString(out_tok),
    ',"gen_ai.usage.cache_read.input_tokens":', toString(intDiv(in_tok, 4)),
    ',"gen_ai.usage.cost":', toString(round((in_tok * 3 + out_tok * 15) / 1000000, 6)),
    ',"gen_ai.response.model":"', model, '"',
    ',"gram.project.id":"', toString(proj), '"',
    ',"user.email":"', email, '"',
    ',"gram.external_user.id":"', email, '"',
    ',"gram.hook.source":"cursor"',
    ',"gram.provider":"anthropic"',
    ',"gram.account_type":"team"',
    ',"gram.billing_mode":"flat_rate"}'
  ),
  '{"gram.deployment.id":"demo-seed"}',
  proj,
  'cursor:usage:metrics',
  '',
  chat_id
FROM (
  SELECT
    number + 1 AS i,
    lower(hex(MD5(concat('gram-demo-chat-', toString(number + 1))))) AS h,
    concat(substring(h, 1, 8), '-', substring(h, 9, 4), '-', substring(h, 13, 4), '-',
           substring(h, 17, 4), '-', substring(h, 21, 12)) AS chat_id,
    if((number + 1) % 3 = 0,
       toUUID('dec0de00-0000-4000-a000-000000000002'),
       toUUID('dec0de00-0000-4000-a000-000000000001')) AS proj,
    arrayElement(
      ['amara@demo.getgram.ai', 'jonas@demo.getgram.ai', 'priya@demo.getgram.ai',
       'mateo@demo.getgram.ai', 'hana@demo.getgram.ai', 'lucas@demo.getgram.ai'],
      1 + ((number + 1) % 6)) AS email,
    if(cityHash64('model', number) % 2 = 0, 'claude-sonnet-4-6', 'gpt-5.6') AS model,
    toUInt64(800 + cityHash64('in', number) % 5000) AS in_tok,
    toUInt64(150 + cityHash64('out', number) % 2000) AS out_tok,
    toUnixTimestamp64Nano(
      subtractMinutes(subtractHours(now64(9), 5 * toInt64(number + 1)),
                      13 * toInt64((number + 1) % 7))) AS nano
  FROM numbers(60)
  WHERE (number + 1) % 2 = 0
);

-- Cursor provenance (even chats): completed tool-call hook rows —
-- hook.source cursor + gram.tool.name + hook.event PostToolUse is the
-- is_agent_tool_call predicate.
INSERT INTO telemetry_logs
  (time_unix_nano, observed_time_unix_nano, severity_text, body, trace_id,
   attributes, resource_attributes, gram_project_id, gram_urn, service_name, gram_chat_id)
SELECT
  nano + toUInt64(k * 31000000000),
  nano + toUInt64(k * 31000000000),
  'INFO',
  concat('Hook: PostToolUse ', tool_name),
  lower(hex(MD5(concat('gram-demo-trace-', toString(i))))),
  concat(
    '{"gram.hook.source":"cursor"',
    ',"gram.hook.event":"', if((i + k) % 19 = 0, 'PostToolUseFailure', 'PostToolUse'), '"',
    ',"gram.tool.name":"', tool_name, '"',
    ',"gen_ai.tool.call.id":"call_demo_', toString(i), '_', toString(k), '"',
    ',"gen_ai.conversation.id":"', chat_id, '"',
    ',"gram.project.id":"', toString(proj), '"',
    ',"user.email":"', email, '"}'
  ),
  '{"gram.deployment.id":"demo-seed"}',
  proj,
  '',
  '',
  chat_id
FROM (
  SELECT
    number + 1 AS i,
    arrayJoin([1, 2]) AS k,
    lower(hex(MD5(concat('gram-demo-chat-', toString(number + 1))))) AS h,
    concat(substring(h, 1, 8), '-', substring(h, 9, 4), '-', substring(h, 13, 4), '-',
           substring(h, 17, 4), '-', substring(h, 21, 12)) AS chat_id,
    if((number + 1) % 3 = 0,
       toUUID('dec0de00-0000-4000-a000-000000000002'),
       toUUID('dec0de00-0000-4000-a000-000000000001')) AS proj,
    arrayElement(
      ['amara@demo.getgram.ai', 'jonas@demo.getgram.ai', 'priya@demo.getgram.ai',
       'mateo@demo.getgram.ai', 'hana@demo.getgram.ai', 'lucas@demo.getgram.ai'],
      1 + ((number + 1) % 6)) AS email,
    arrayElement(['search_logs', 'get_metrics', 'query_db', 'get_customer',
                  'list_deploys', 'process_refund', 'fetch_traces', 'check_health'],
                 1 + (cityHash64('tool', number, arrayJoin([1, 2])) % 8)) AS tool_name,
    toUnixTimestamp64Nano(
      subtractMinutes(subtractHours(now64(9), 5 * toInt64(number + 1)),
                      13 * toInt64((number + 1) % 7))) AS nano
  FROM numbers(60)
  WHERE (number + 1) % 2 = 0
);

-- Postflight asserts: rows landed, the cost/session MVs actually fired, and
-- nothing leaked outside the demo projects. throwIf aborts the script
-- (non-zero exit for the runner) when violated.
SELECT throwIf(
  (SELECT count() FROM telemetry_logs WHERE gram_project_id IN
     (toUUID('dec0de00-0000-4000-a000-000000000001'), toUUID('dec0de00-0000-4000-a000-000000000002'))
   ) < 300,
  'demo seed postflight: expected >= 300 demo telemetry rows');

SELECT throwIf(
  (SELECT count() FROM chat_session_summaries WHERE gram_project_id IN
     (toUUID('dec0de00-0000-4000-a000-000000000001'), toUUID('dec0de00-0000-4000-a000-000000000002'))
   ) = 0,
  'demo seed postflight: chat_session_summaries_mv did not fire');

SELECT throwIf(
  (SELECT count() FROM attribute_metrics_summaries WHERE gram_project_id IN
     (toUUID('dec0de00-0000-4000-a000-000000000001'), toUUID('dec0de00-0000-4000-a000-000000000002'))
   ) = 0,
  'demo seed postflight: attribute_metrics_summaries_mv did not fire');

SELECT throwIf(
  (SELECT count() FROM telemetry_logs
   WHERE toString(resource_attributes.gram.deployment.id) = 'demo-seed'
     AND gram_project_id NOT IN
       (toUUID('dec0de00-0000-4000-a000-000000000001'), toUUID('dec0de00-0000-4000-a000-000000000002'))
   ) > 0,
  'demo seed postflight: demo-seed rows leaked outside demo projects');
