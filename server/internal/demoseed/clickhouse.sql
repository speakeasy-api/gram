-- Demo workspace seed — ClickHouse side.
--
-- Regenerates all demo-org telemetry. Every statement is scoped to the fixed
-- demo project UUIDs / demo org id (telemetry_logs has no org column; the
-- demo project ids ARE the isolation boundary — they must match
-- seed/demo/postgres.sql).
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
-- observed agent surfaces. Generic "chat:completion" rows are deliberately
-- excluded. The inserts below emit:
--   * odd-numbered chats  → Claude provenance: claude-code:otel:logs
--     api_request rows (usage/cost, per-turn prompt.id) + tool_result rows
--     (tool calls, tool_use_id call_demo_<i>_<k>, payload sizes)
--   * even-numbered chats → Cursor provenance: cursor:usage:metrics rows
--     (usage/cost) + PostToolUse hook rows (tool calls, hook branch of the
--     insights CTE via gram.event.source=hook)
--   * every chat          → "tools:" urn rows with gram.toolset.slug so the
--     Insights direct branch classifies them as hosted MCP traffic
--   * odd chats           → one Skill hook row (Insights skill panels)
--   * every chat          → one chat_analysis:work_units:score row (Costs
--     "Efficiency" dataset)
--   * shadow MCP inventory + hook telemetry, authz challenges, and the
--     risk_findings mirror of the Postgres findings
--
-- Every row also carries the WorkOS-style user identity attributes
-- (user.attributes.*, user.roles, user.groups, gram.hook.hostname) — the cost
-- page HIDES any pivot whose key never appears in attribute_keys, so these
-- must be present, not merely non-empty.
--
-- Chat ids reproduce the Postgres formula demo.det_uuid('gram-demo-chat-' || n) (md5 with version nibble '5', variant '8') so
-- the ClickHouse telemetry joins the Postgres chats exactly. Trace ids are
-- unique per surface (tooltrace/hooktrace/skilltrace/...): trace_summaries
-- collapses per trace_id, so sharing one trace across row types would merge
-- them into a single unclassifiable trace.
--
--   Local: applied by `mise run seed:demo` via clickhouse-client --multiquery.
--   Prod:  run daily by the infra cron AFTER demo.ensure_demo_org() on
--          Postgres (ClickHouse has no procedural functions, hence a script).

SET mutations_sync = 1;

-- Scoped deletes: telemetry source + every MV target + the org-keyed tables.
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
ALTER TABLE shadow_mcp_inventory_urls DELETE WHERE gram_project_id IN
  (toUUID('dec0de00-0000-4000-a000-000000000001'), toUUID('dec0de00-0000-4000-a000-000000000002'));
ALTER TABLE authz_challenges DELETE WHERE organization_id = 'org_gram_demo_workspace';
ALTER TABLE risk_findings DELETE WHERE organization_id = 'org_gram_demo_workspace';

-- Tool-execution rows: 2 per chat. gram.toolset.slug makes the Insights CTE's
-- direct branch classify each trace as hosted MCP traffic; unique per-call
-- trace ids keep trace_summaries from merging them. ~7% non-200.
INSERT INTO telemetry_logs
  (time_unix_nano, observed_time_unix_nano, severity_text, body, trace_id,
   attributes, resource_attributes, gram_project_id, gram_urn, service_name, gram_chat_id)
SELECT
  nano + toUInt64(k * 1000000),
  nano + toUInt64(k * 1000000),
  'INFO',
  concat('Tool call: ', tool_name),
  lower(hex(MD5(concat('gram-demo-tooltrace-', toString(i), '-', toString(k))))),
  concat(
    '{"gram.tool.urn":"tools:http:acme:', tool_name, '"',
    ',"gram.tool.name":"', tool_name, '"',
    ',"gram.toolset.slug":"', if(i % 3 = 0, 'acme-ops', 'acme-support-tools'), '"',
    ',"http.response.status_code":', toString(if((i + k) % 14 = 0, 500, 200)),
    ',"http.server.request.duration":', toString(round(0.05 + (cityHash64(i, k) % 200) / 100, 3)),
    ',"gen_ai.conversation.id":"', chat_id, '"',
    ',"gram.project.id":"', toString(proj), '"',
    ',"user.email":"', email, '"',
    ',"user.attributes.division_name":"', division, '"',
    ',"user.attributes.department_name":"', department, '"',
    ',"user.attributes.job_title":"', title, '"',
    ',"user.attributes.employee_type":"', etype, '"',
    ',"user.attributes.cost_center_name":"', cc, '"',
    ',"user.roles":', rolesjson,
    ',"user.groups":["', team, '"]',
    ',"gram.hook.hostname":"', hostname, '"',
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
    1 + ((number + 1) % 6) AS uidx,
    lower(hex(MD5(concat('gram-demo-chat-', toString(number + 1))))) AS h,
    concat(substring(h, 1, 8), '-', substring(h, 9, 4), '-5', substring(h, 14, 3), '-8',
           substring(h, 18, 3), '-', substring(h, 21, 12)) AS chat_id,
    if((number + 1) % 3 = 0,
       toUUID('dec0de00-0000-4000-a000-000000000002'),
       toUUID('dec0de00-0000-4000-a000-000000000001')) AS proj,
    arrayElement(['amara@demo.getgram.ai', 'jonas@demo.getgram.ai', 'priya@demo.getgram.ai',
                  'mateo@demo.getgram.ai', 'hana@demo.getgram.ai', 'lucas@demo.getgram.ai'], uidx) AS email,
    arrayElement(['Customer Experience', 'Customer Experience', 'R&D', 'R&D', 'Customer Experience', 'R&D'], uidx) AS division,
    arrayElement(['Support Engineering', 'Support Engineering', 'Platform Engineering', 'Platform Engineering', 'Billing Operations', 'Engineering Leadership'], uidx) AS department,
    arrayElement(['Support Engineer', 'Senior Support Engineer', 'Platform Engineer', 'Site Reliability Engineer', 'Billing Analyst', 'Engineering Manager'], uidx) AS title,
    arrayElement(['full-time', 'full-time', 'full-time', 'contractor', 'part-time', 'full-time'], uidx) AS etype,
    arrayElement(['CC-SUP-4100', 'CC-SUP-4100', 'CC-ENG-2200', 'CC-ENG-2200', 'CC-OPS-3300', 'CC-ENG-2200'], uidx) AS cc,
    arrayElement(['Frontline Support', 'Frontline Support', 'Infra', 'Reliability', 'Billing Ops', 'Leadership'], uidx) AS team,
    arrayElement(['["developer","viewer"]', '["developer"]', '["admin","developer"]', '["developer"]', '["analyst","viewer"]', '["admin","viewer"]'], uidx) AS rolesjson,
    arrayElement(['amara-mbp.local', 'jonas-mbp.local', 'priya-mbp.local', 'mateo-mbp.local', 'hana-mbp.local', 'lucas-mbp.local'], uidx) AS hostname,
    if((number + 1) % 2 = 1, 'claude-code', 'cursor') AS hook,
    arrayElement(['search_logs', 'get_metrics', 'query_db', 'get_customer',
                  'list_deploys', 'process_refund', 'fetch_traces', 'check_health'],
                 1 + (cityHash64('tool', number, arrayJoin([1, 2])) % 8)) AS tool_name,
    toUnixTimestamp64Nano(
      subtractMinutes(subtractHours(now64(9), 5 * toInt64(number + 1)),
                      13 * toInt64((number + 1) % 7))) AS nano
  FROM numbers(60)
);

-- Claude provenance (odd chats): one claude_code.api_request row per turn.
-- prompt.id demo-prompt-<i>-<turn> joins the Postgres user messages; the
-- skill/agent/mcp attribution keys light up the Costs Skills/Subagents/MCP
-- datasets (only read on api_request rows).
INSERT INTO telemetry_logs
  (time_unix_nano, observed_time_unix_nano, severity_text, body, trace_id,
   attributes, resource_attributes, gram_project_id, gram_urn, service_name, gram_chat_id)
SELECT
  nano + toUInt64(turn * 60000000000),
  nano + toUInt64(turn * 60000000000),
  'INFO',
  'claude_code.api_request',
  lower(hex(MD5(concat('gram-demo-apitrace-', toString(i), '-', toString(turn))))),
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
    ',"skill.name":"', arrayElement(['', 'triage-incident', 'support-refunds'], 1 + (cityHash64('skill', i, turn) % 3)), '"',
    ',"agent.name":"', arrayElement(['', 'explore', 'general-purpose'], 1 + (cityHash64('agent', i, turn) % 3)), '"',
    ',"mcp_server.name":"acme-support-mcp"',
    ',"mcp_tool.name":"', arrayElement(['search_logs', 'get_customer', 'process_refund'], 1 + (cityHash64('mtool', i, turn) % 3)), '"',
    ',"gen_ai.conversation.id":"', chat_id, '"',
    ',"gram.project.id":"', toString(proj), '"',
    ',"user.email":"', email, '"',
    ',"gram.external_user.id":"', email, '"',
    ',"user.attributes.division_name":"', division, '"',
    ',"user.attributes.department_name":"', department, '"',
    ',"user.attributes.job_title":"', title, '"',
    ',"user.attributes.employee_type":"', etype, '"',
    ',"user.attributes.cost_center_name":"', cc, '"',
    ',"user.roles":', rolesjson,
    ',"user.groups":["', team, '"]',
    ',"gram.hook.hostname":"', hostname, '"',
    ',"gram.hook.source":"claude-code"',
    ',"gram.provider":"anthropic"',
    ',"gram.account_type":"', if(email = 'mateo@demo.getgram.ai', 'personal', 'team'), '"',
    ',"gram.billing_mode":"', if(email = 'mateo@demo.getgram.ai', 'flat_rate', 'metered'), '"}'
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
    1 + ((number + 1) % 6) AS uidx,
    lower(hex(MD5(concat('gram-demo-chat-', toString(number + 1))))) AS h,
    concat(substring(h, 1, 8), '-', substring(h, 9, 4), '-5', substring(h, 14, 3), '-8',
           substring(h, 18, 3), '-', substring(h, 21, 12)) AS chat_id,
    if((number + 1) % 3 = 0,
       toUUID('dec0de00-0000-4000-a000-000000000002'),
       toUUID('dec0de00-0000-4000-a000-000000000001')) AS proj,
    arrayElement(['amara@demo.getgram.ai', 'jonas@demo.getgram.ai', 'priya@demo.getgram.ai',
                  'mateo@demo.getgram.ai', 'hana@demo.getgram.ai', 'lucas@demo.getgram.ai'], uidx) AS email,
    arrayElement(['Customer Experience', 'Customer Experience', 'R&D', 'R&D', 'Customer Experience', 'R&D'], uidx) AS division,
    arrayElement(['Support Engineering', 'Support Engineering', 'Platform Engineering', 'Platform Engineering', 'Billing Operations', 'Engineering Leadership'], uidx) AS department,
    arrayElement(['Support Engineer', 'Senior Support Engineer', 'Platform Engineer', 'Site Reliability Engineer', 'Billing Analyst', 'Engineering Manager'], uidx) AS title,
    arrayElement(['full-time', 'full-time', 'full-time', 'contractor', 'part-time', 'full-time'], uidx) AS etype,
    arrayElement(['CC-SUP-4100', 'CC-SUP-4100', 'CC-ENG-2200', 'CC-ENG-2200', 'CC-OPS-3300', 'CC-ENG-2200'], uidx) AS cc,
    arrayElement(['Frontline Support', 'Frontline Support', 'Infra', 'Reliability', 'Billing Ops', 'Leadership'], uidx) AS team,
    arrayElement(['["developer","viewer"]', '["developer"]', '["admin","developer"]', '["developer"]', '["analyst","viewer"]', '["admin","viewer"]'], uidx) AS rolesjson,
    arrayElement(['amara-mbp.local', 'jonas-mbp.local', 'priya-mbp.local', 'mateo-mbp.local', 'hana-mbp.local', 'lucas-mbp.local'], uidx) AS hostname,
    if(cityHash64('model', number) % 3 = 0, 'claude-opus-4-5', 'claude-sonnet-4-6') AS model,
    toUInt64(1500 + cityHash64('in', number, arrayJoin([1, 2, 3])) % 6000) AS in_tok,
    toUInt64(200 + cityHash64('out', number, arrayJoin([1, 2, 3])) % 2500) AS out_tok,
    toUnixTimestamp64Nano(
      subtractMinutes(subtractHours(now64(9), 5 * toInt64(number + 1)),
                      13 * toInt64((number + 1) % 7))) AS nano
  FROM numbers(60)
  WHERE (number + 1) % 2 = 1
);

-- Claude provenance (odd chats): one claude_code.tool_result row per tool
-- call. tool_use_id joins the Postgres tool messages (call_demo_<i>_<k>);
-- prompt.id is REQUIRED here too — the tool-payload-size query filters on it.
INSERT INTO telemetry_logs
  (time_unix_nano, observed_time_unix_nano, severity_text, body, trace_id,
   attributes, resource_attributes, gram_project_id, gram_urn, service_name, gram_chat_id)
SELECT
  nano + toUInt64(k * 61000000000),
  nano + toUInt64(k * 61000000000),
  'INFO',
  'claude_code.tool_result',
  lower(hex(MD5(concat('gram-demo-apitrace-', toString(i), '-', toString(k))))),
  concat(
    '{"event.name":"tool_result"',
    ',"tool_use_id":"call_demo_', toString(i), '_', toString(k), '"',
    ',"prompt.id":"demo-prompt-', toString(i), '-', toString(k), '"',
    ',"tool_name":"', tool_name, '"',
    ',"tool_input_size_bytes":', toString(200 + cityHash64('tin', i, k) % 1800),
    ',"tool_result_size_bytes":', toString(500 + cityHash64('tout', i, k) % 8000),
    ',"success":', if((i + k) % 17 = 0, 'false', 'true'),
    ',"gen_ai.conversation.id":"', chat_id, '"',
    ',"gram.project.id":"', toString(proj), '"',
    ',"user.email":"', email, '"',
    ',"user.attributes.division_name":"', division, '"',
    ',"user.attributes.department_name":"', department, '"',
    ',"user.attributes.job_title":"', title, '"',
    ',"user.attributes.employee_type":"', etype, '"',
    ',"user.attributes.cost_center_name":"', cc, '"',
    ',"user.roles":', rolesjson,
    ',"user.groups":["', team, '"]',
    ',"gram.hook.hostname":"', hostname, '"',
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
    1 + ((number + 1) % 6) AS uidx,
    lower(hex(MD5(concat('gram-demo-chat-', toString(number + 1))))) AS h,
    concat(substring(h, 1, 8), '-', substring(h, 9, 4), '-5', substring(h, 14, 3), '-8',
           substring(h, 18, 3), '-', substring(h, 21, 12)) AS chat_id,
    if((number + 1) % 3 = 0,
       toUUID('dec0de00-0000-4000-a000-000000000002'),
       toUUID('dec0de00-0000-4000-a000-000000000001')) AS proj,
    arrayElement(['amara@demo.getgram.ai', 'jonas@demo.getgram.ai', 'priya@demo.getgram.ai',
                  'mateo@demo.getgram.ai', 'hana@demo.getgram.ai', 'lucas@demo.getgram.ai'], uidx) AS email,
    arrayElement(['Customer Experience', 'Customer Experience', 'R&D', 'R&D', 'Customer Experience', 'R&D'], uidx) AS division,
    arrayElement(['Support Engineering', 'Support Engineering', 'Platform Engineering', 'Platform Engineering', 'Billing Operations', 'Engineering Leadership'], uidx) AS department,
    arrayElement(['Support Engineer', 'Senior Support Engineer', 'Platform Engineer', 'Site Reliability Engineer', 'Billing Analyst', 'Engineering Manager'], uidx) AS title,
    arrayElement(['full-time', 'full-time', 'full-time', 'contractor', 'part-time', 'full-time'], uidx) AS etype,
    arrayElement(['CC-SUP-4100', 'CC-SUP-4100', 'CC-ENG-2200', 'CC-ENG-2200', 'CC-OPS-3300', 'CC-ENG-2200'], uidx) AS cc,
    arrayElement(['Frontline Support', 'Frontline Support', 'Infra', 'Reliability', 'Billing Ops', 'Leadership'], uidx) AS team,
    arrayElement(['["developer","viewer"]', '["developer"]', '["admin","developer"]', '["developer"]', '["analyst","viewer"]', '["admin","viewer"]'], uidx) AS rolesjson,
    arrayElement(['amara-mbp.local', 'jonas-mbp.local', 'priya-mbp.local', 'mateo-mbp.local', 'hana-mbp.local', 'lucas-mbp.local'], uidx) AS hostname,
    arrayElement(['search_logs', 'get_metrics', 'query_db', 'get_customer',
                  'list_deploys', 'process_refund', 'fetch_traces', 'check_health'],
                 1 + (cityHash64('tool', number, arrayJoin([1, 2])) % 8)) AS tool_name,
    toUnixTimestamp64Nano(
      subtractMinutes(subtractHours(now64(9), 5 * toInt64(number + 1)),
                      13 * toInt64((number + 1) % 7))) AS nano
  FROM numbers(60)
  WHERE (number + 1) % 2 = 1
);

-- Odd chats: one Skill hook row each — feeds the Insights "Skill Usage" /
-- "Users per Skill" panels (skill_name materializes only when
-- gram.tool.name = 'Skill').
INSERT INTO telemetry_logs
  (time_unix_nano, observed_time_unix_nano, severity_text, body, trace_id,
   attributes, resource_attributes, gram_project_id, gram_urn, service_name, gram_chat_id)
SELECT
  nano + 45000000000,
  nano + 45000000000,
  'INFO',
  'Hook: Skill invoked',
  lower(hex(MD5(concat('gram-demo-skilltrace-', toString(i))))),
  concat(
    '{"gram.event.source":"hook"',
    ',"gram.hook.source":"claude-code"',
    ',"gram.hook.event":"PostToolUse"',
    ',"gram.tool.name":"Skill"',
    ',"gen_ai.tool.call.arguments":"{\\"skill\\":\\"', skill, '\\"}"',
    ',"gen_ai.tool.call.result":"ok"',
    ',"gen_ai.conversation.id":"', chat_id, '"',
    ',"gram.project.id":"', toString(proj), '"',
    ',"user.email":"', email, '"',
    ',"gram.hook.hostname":"', hostname, '"}'
  ),
  '{"gram.deployment.id":"demo-seed"}',
  proj,
  '',
  '',
  chat_id
FROM (
  SELECT
    number + 1 AS i,
    1 + ((number + 1) % 6) AS uidx,
    lower(hex(MD5(concat('gram-demo-chat-', toString(number + 1))))) AS h,
    concat(substring(h, 1, 8), '-', substring(h, 9, 4), '-5', substring(h, 14, 3), '-8',
           substring(h, 18, 3), '-', substring(h, 21, 12)) AS chat_id,
    if((number + 1) % 3 = 0,
       toUUID('dec0de00-0000-4000-a000-000000000002'),
       toUUID('dec0de00-0000-4000-a000-000000000001')) AS proj,
    arrayElement(['amara@demo.getgram.ai', 'jonas@demo.getgram.ai', 'priya@demo.getgram.ai',
                  'mateo@demo.getgram.ai', 'hana@demo.getgram.ai', 'lucas@demo.getgram.ai'], uidx) AS email,
    arrayElement(['amara-mbp.local', 'jonas-mbp.local', 'priya-mbp.local', 'mateo-mbp.local', 'hana-mbp.local', 'lucas-mbp.local'], uidx) AS hostname,
    arrayElement(['support-refunds', 'triage-incident', 'runbook'], 1 + (cityHash64('skl', number) % 3)) AS skill,
    toUnixTimestamp64Nano(
      subtractMinutes(subtractHours(now64(9), 5 * toInt64(number + 1)),
                      13 * toInt64((number + 1) % 7))) AS nano
  FROM numbers(60)
  WHERE (number + 1) % 2 = 1
);

-- Cursor provenance (even chats): one cursor:usage:metrics row per chat.
-- Provider follows the model (gpt-5.6 -> openai).
INSERT INTO telemetry_logs
  (time_unix_nano, observed_time_unix_nano, severity_text, body, trace_id,
   attributes, resource_attributes, gram_project_id, gram_urn, service_name, gram_chat_id)
SELECT
  nano + 2000000,
  nano + 2000000,
  'INFO',
  'Cursor usage metrics',
  lower(hex(MD5(concat('gram-demo-usagetrace-', toString(i))))),
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
    ',"user.attributes.division_name":"', division, '"',
    ',"user.attributes.department_name":"', department, '"',
    ',"user.attributes.job_title":"', title, '"',
    ',"user.attributes.employee_type":"', etype, '"',
    ',"user.attributes.cost_center_name":"', cc, '"',
    ',"user.roles":', rolesjson,
    ',"user.groups":["', team, '"]',
    ',"gram.hook.hostname":"', hostname, '"',
    ',"gram.hook.source":"cursor"',
    ',"gram.provider":"', if(model = 'gpt-5.6', 'openai', 'anthropic'), '"',
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
    1 + ((number + 1) % 6) AS uidx,
    lower(hex(MD5(concat('gram-demo-chat-', toString(number + 1))))) AS h,
    concat(substring(h, 1, 8), '-', substring(h, 9, 4), '-5', substring(h, 14, 3), '-8',
           substring(h, 18, 3), '-', substring(h, 21, 12)) AS chat_id,
    if((number + 1) % 3 = 0,
       toUUID('dec0de00-0000-4000-a000-000000000002'),
       toUUID('dec0de00-0000-4000-a000-000000000001')) AS proj,
    arrayElement(['amara@demo.getgram.ai', 'jonas@demo.getgram.ai', 'priya@demo.getgram.ai',
                  'mateo@demo.getgram.ai', 'hana@demo.getgram.ai', 'lucas@demo.getgram.ai'], uidx) AS email,
    arrayElement(['Customer Experience', 'Customer Experience', 'R&D', 'R&D', 'Customer Experience', 'R&D'], uidx) AS division,
    arrayElement(['Support Engineering', 'Support Engineering', 'Platform Engineering', 'Platform Engineering', 'Billing Operations', 'Engineering Leadership'], uidx) AS department,
    arrayElement(['Support Engineer', 'Senior Support Engineer', 'Platform Engineer', 'Site Reliability Engineer', 'Billing Analyst', 'Engineering Manager'], uidx) AS title,
    arrayElement(['full-time', 'full-time', 'full-time', 'contractor', 'part-time', 'full-time'], uidx) AS etype,
    arrayElement(['CC-SUP-4100', 'CC-SUP-4100', 'CC-ENG-2200', 'CC-ENG-2200', 'CC-OPS-3300', 'CC-ENG-2200'], uidx) AS cc,
    arrayElement(['Frontline Support', 'Frontline Support', 'Infra', 'Reliability', 'Billing Ops', 'Leadership'], uidx) AS team,
    arrayElement(['["developer","viewer"]', '["developer"]', '["admin","developer"]', '["developer"]', '["analyst","viewer"]', '["admin","viewer"]'], uidx) AS rolesjson,
    arrayElement(['amara-mbp.local', 'jonas-mbp.local', 'priya-mbp.local', 'mateo-mbp.local', 'hana-mbp.local', 'lucas-mbp.local'], uidx) AS hostname,
    if(cityHash64('model', number) % 2 = 0, 'claude-sonnet-4-6', 'gpt-5.6') AS model,
    toUInt64(800 + cityHash64('in', number) % 5000) AS in_tok,
    toUInt64(150 + cityHash64('out', number) % 2000) AS out_tok,
    toUnixTimestamp64Nano(
      subtractMinutes(subtractHours(now64(9), 5 * toInt64(number + 1)),
                      13 * toInt64((number + 1) % 7))) AS nano
  FROM numbers(60)
  WHERE (number + 1) % 2 = 0
);

-- Cursor provenance (even chats): completed tool-call hook rows. Unique
-- hooktrace ids + gram.event.source=hook put them on the Insights hook
-- branch as shadow-MCP-sourced traffic; result/error attrs drive
-- success/failure classification.
INSERT INTO telemetry_logs
  (time_unix_nano, observed_time_unix_nano, severity_text, body, trace_id,
   attributes, resource_attributes, gram_project_id, gram_urn, service_name, gram_chat_id)
SELECT
  nano + toUInt64(k * 31000000000),
  nano + toUInt64(k * 31000000000),
  'INFO',
  concat('Hook: PostToolUse ', tool_name),
  lower(hex(MD5(concat('gram-demo-hooktrace-', toString(i), '-', toString(k))))),
  concat(
    '{"gram.event.source":"hook"',
    ',"gram.hook.source":"cursor"',
    ',"gram.hook.event":"', if((i + k) % 19 = 0, 'PostToolUseFailure', 'PostToolUse'), '"',
    ',"gram.tool.name":"', tool_name, '"',
    ',"gram.tool_call.source":"acme-internal-mcp"',
    if((i + k) % 19 = 0,
       ',"gram.hook.error":"tool execution failed"',
       ',"gen_ai.tool.call.result":"ok"'),
    ',"gen_ai.tool.call.id":"call_demo_', toString(i), '_', toString(k), '"',
    ',"gen_ai.conversation.id":"', chat_id, '"',
    ',"gram.project.id":"', toString(proj), '"',
    ',"user.email":"', email, '"',
    ',"user.attributes.division_name":"', division, '"',
    ',"user.attributes.department_name":"', department, '"',
    ',"user.attributes.job_title":"', title, '"',
    ',"user.attributes.employee_type":"', etype, '"',
    ',"user.attributes.cost_center_name":"', cc, '"',
    ',"user.roles":', rolesjson,
    ',"user.groups":["', team, '"]',
    ',"gram.hook.hostname":"', hostname, '"}'
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
    1 + ((number + 1) % 6) AS uidx,
    lower(hex(MD5(concat('gram-demo-chat-', toString(number + 1))))) AS h,
    concat(substring(h, 1, 8), '-', substring(h, 9, 4), '-5', substring(h, 14, 3), '-8',
           substring(h, 18, 3), '-', substring(h, 21, 12)) AS chat_id,
    if((number + 1) % 3 = 0,
       toUUID('dec0de00-0000-4000-a000-000000000002'),
       toUUID('dec0de00-0000-4000-a000-000000000001')) AS proj,
    arrayElement(['amara@demo.getgram.ai', 'jonas@demo.getgram.ai', 'priya@demo.getgram.ai',
                  'mateo@demo.getgram.ai', 'hana@demo.getgram.ai', 'lucas@demo.getgram.ai'], uidx) AS email,
    arrayElement(['Customer Experience', 'Customer Experience', 'R&D', 'R&D', 'Customer Experience', 'R&D'], uidx) AS division,
    arrayElement(['Support Engineering', 'Support Engineering', 'Platform Engineering', 'Platform Engineering', 'Billing Operations', 'Engineering Leadership'], uidx) AS department,
    arrayElement(['Support Engineer', 'Senior Support Engineer', 'Platform Engineer', 'Site Reliability Engineer', 'Billing Analyst', 'Engineering Manager'], uidx) AS title,
    arrayElement(['full-time', 'full-time', 'full-time', 'contractor', 'part-time', 'full-time'], uidx) AS etype,
    arrayElement(['CC-SUP-4100', 'CC-SUP-4100', 'CC-ENG-2200', 'CC-ENG-2200', 'CC-OPS-3300', 'CC-ENG-2200'], uidx) AS cc,
    arrayElement(['Frontline Support', 'Frontline Support', 'Infra', 'Reliability', 'Billing Ops', 'Leadership'], uidx) AS team,
    arrayElement(['["developer","viewer"]', '["developer"]', '["admin","developer"]', '["developer"]', '["analyst","viewer"]', '["admin","viewer"]'], uidx) AS rolesjson,
    arrayElement(['amara-mbp.local', 'jonas-mbp.local', 'priya-mbp.local', 'mateo-mbp.local', 'hana-mbp.local', 'lucas-mbp.local'], uidx) AS hostname,
    arrayElement(['search_logs', 'get_metrics', 'query_db', 'get_customer',
                  'list_deploys', 'process_refund', 'fetch_traces', 'check_health'],
                 1 + (cityHash64('tool', number, arrayJoin([1, 2])) % 8)) AS tool_name,
    toUnixTimestamp64Nano(
      subtractMinutes(subtractHours(now64(9), 5 * toInt64(number + 1)),
                      13 * toInt64((number + 1) % 7))) AS nano
  FROM numbers(60)
  WHERE (number + 1) % 2 = 0
);

-- One chat-analysis work-units score row per chat: feeds the Costs
-- "Efficiency" dataset (total_work_units / scored_cost / scored_tokens are
-- summed only from these synthetic rows).
INSERT INTO telemetry_logs
  (time_unix_nano, observed_time_unix_nano, severity_text, body, trace_id,
   attributes, resource_attributes, gram_project_id, gram_urn, service_name, gram_chat_id)
SELECT
  nano + 3000000,
  nano + 3000000,
  'INFO',
  'Chat analysis: work units',
  lower(hex(MD5(concat('gram-demo-wutrace-', toString(i))))),
  concat(
    '{"gram.chat_analysis.work_units":', toString(1 + cityHash64('wu', i) % 5),
    ',"gram.chat_analysis.scored_cost":', toString(round((5000 + cityHash64('sc', i) % 90000) / 1000000, 6)),
    ',"gram.chat_analysis.scored_tokens":', toString(2000 + cityHash64('st', i) % 20000),
    ',"gen_ai.conversation.id":"', chat_id, '"',
    ',"gen_ai.response.model":"', if(i % 2 = 1, 'claude-sonnet-4-6', 'gpt-5.6'), '"',
    ',"gram.project.id":"', toString(proj), '"',
    ',"user.email":"', email, '"',
    ',"gram.hook.source":"', if(i % 2 = 1, 'claude-code', 'cursor'), '"}'
  ),
  '{"gram.deployment.id":"demo-seed"}',
  proj,
  'chat_analysis:work_units:score',
  '',
  chat_id
FROM (
  SELECT
    number + 1 AS i,
    1 + ((number + 1) % 6) AS uidx,
    lower(hex(MD5(concat('gram-demo-chat-', toString(number + 1))))) AS h,
    concat(substring(h, 1, 8), '-', substring(h, 9, 4), '-5', substring(h, 14, 3), '-8',
           substring(h, 18, 3), '-', substring(h, 21, 12)) AS chat_id,
    if((number + 1) % 3 = 0,
       toUUID('dec0de00-0000-4000-a000-000000000002'),
       toUUID('dec0de00-0000-4000-a000-000000000001')) AS proj,
    arrayElement(['amara@demo.getgram.ai', 'jonas@demo.getgram.ai', 'priya@demo.getgram.ai',
                  'mateo@demo.getgram.ai', 'hana@demo.getgram.ai', 'lucas@demo.getgram.ai'], uidx) AS email,
    toUnixTimestamp64Nano(
      subtractMinutes(subtractHours(now64(9), 5 * toInt64(number + 1)),
                      13 * toInt64((number + 1) % 7))) AS nano
  FROM numbers(60)
);

-- Shadow MCP inventory + companion hook telemetry (Shadow MCP page list,
-- call/user counts).
INSERT INTO shadow_mcp_inventory_urls
  (gram_project_id, canonical_server_url, url_host, server_name, first_seen, last_seen, updated_at)
VALUES
  (toUUID('dec0de00-0000-4000-a000-000000000001'), 'https://mcp.internal.acme.example/mcp',
   'mcp.internal.acme.example', 'acme-internal-mcp',
   now64(9) - INTERVAL 10 DAY, now64(9) - INTERVAL 2 HOUR, now64(9)),
  (toUUID('dec0de00-0000-4000-a000-000000000001'), 'https://tools.vendor-x.example/sse',
   'tools.vendor-x.example', 'vendor-x-tools',
   now64(9) - INTERVAL 6 DAY, now64(9) - INTERVAL 3 HOUR, now64(9)),
  (toUUID('dec0de00-0000-4000-a000-000000000002'), 'https://mcp.internal.acme.example/mcp',
   'mcp.internal.acme.example', 'acme-internal-mcp',
   now64(9) - INTERVAL 8 DAY, now64(9) - INTERVAL 5 HOUR, now64(9)),
  (toUUID('dec0de00-0000-4000-a000-000000000002'), 'https://ci-agents.acme.example/mcp',
   'ci-agents.acme.example', 'ci-agents',
   now64(9) - INTERVAL 4 DAY, now64(9) - INTERVAL 3 HOUR, now64(9));

INSERT INTO telemetry_logs
  (time_unix_nano, observed_time_unix_nano, severity_text, body, trace_id,
   attributes, resource_attributes, gram_project_id, gram_urn, service_name, gram_chat_id)
SELECT
  nano,
  nano,
  'INFO',
  'Shadow MCP tool call',
  lower(hex(MD5(concat('gram-demo-shadowtrace-', toString(number))))),
  concat(
    '{"gram.event.source":"hook"',
    ',"gram.hook.source":"claude-code"',
    ',"gram.hook.event":"PostToolUse"',
    ',"gram.tool.name":"', arrayElement(['lookup_ticket', 'run_ci_job', 'search_wiki'], 1 + (number % 3)), '"',
    ',"gram.mcp.server_url":"', arrayElement(
        ['https://mcp.internal.acme.example/mcp', 'https://tools.vendor-x.example/sse',
         'https://ci-agents.acme.example/mcp'], 1 + (number % 3)), '"',
    ',"gen_ai.tool.call.result":"ok"',
    ',"gram.project.id":"', toString(proj), '"',
    ',"user.email":"', arrayElement(
        ['amara@demo.getgram.ai', 'jonas@demo.getgram.ai', 'priya@demo.getgram.ai',
         'mateo@demo.getgram.ai', 'hana@demo.getgram.ai', 'lucas@demo.getgram.ai'], 1 + (number % 6)), '"}'
  ),
  '{"gram.deployment.id":"demo-seed"}',
  proj,
  concat('hooks:', arrayElement(['lookup_ticket', 'run_ci_job', 'search_wiki'], 1 + (number % 3))),
  'gram-hooks',
  ''
FROM (
  SELECT
    number,
    if(number % 3 = 2,
       toUUID('dec0de00-0000-4000-a000-000000000002'),
       toUUID('dec0de00-0000-4000-a000-000000000001')) AS proj,
    toUnixTimestamp64Nano(subtractHours(now64(9), 3 + toInt64(number) * 9)) AS nano
  FROM numbers(24)
);

-- Authz challenges (org home "Recent challenges" panel + /access/challenges).
-- user_id values are demo members (organization_user_relationships rows exist,
-- so the member-suppression filter keeps them visible); one api_key bucket
-- stays visible regardless of membership.
INSERT INTO authz_challenges
  (timestamp, organization_id, project_id, trace_id, span_id, request_id,
   principal_urn, principal_type, user_id, user_email, api_key_id,
   role_slugs, operation, outcome, reason, scope, resource_kind, resource_id,
   selector, expanded_scopes,
   `requested_checks.scope`, `requested_checks.resource_kind`,
   `requested_checks.resource_id`, `requested_checks.selector`,
   `matched_grants.principal_urn`, `matched_grants.scope`,
   `matched_grants.selector`, `matched_grants.matched_via_check_scope`,
   evaluated_grant_count)
SELECT
  now64(9) - toIntervalHour(2 + number * 5),
  'org_gram_demo_workspace',
  if(number % 4 = 3, 'dec0de00-0000-4000-a000-000000000002', 'dec0de00-0000-4000-a000-000000000001'),
  lower(hex(MD5(concat('gram-demo-chal-', toString(number))))),
  substring(lower(hex(MD5(concat('gram-demo-chalspan-', toString(number))))), 1, 16),
  concat('req_', substring(lower(hex(MD5(concat('gram-demo-chalreq-', toString(number))))), 1, 16)),
  if(number % 5 = 4, 'api_key:akey_demo0000000001',
     concat('user:', arrayElement(['user_demo_jonas', 'user_demo_priya', 'user_demo_hana'], 1 + (number % 3)))),
  if(number % 5 = 4, 'api_key', 'user'),
  if(number % 5 = 4, NULL,
     arrayElement(['user_demo_jonas', 'user_demo_priya', 'user_demo_hana'], 1 + (number % 3))),
  if(number % 5 = 4, NULL,
     arrayElement(['jonas@demo.getgram.ai', 'priya@demo.getgram.ai', 'hana@demo.getgram.ai'], 1 + (number % 3))),
  if(number % 5 = 4, 'akey_demo0000000001', NULL),
  [],
  'require',
  'deny',
  if(number % 2 = 0, 'scope_unsatisfied', 'no_grants'),
  arrayElement(['toolset:admin', 'project:admin', 'environment:read'], 1 + (number % 3)),
  arrayElement(['toolset', 'project', 'environment'], 1 + (number % 3)),
  arrayElement(['dec0de00-0000-4000-a000-000000005e01',
                'dec0de00-0000-4000-a000-000000000002',
                'dec0de00-0000-4000-a000-00000000ee01'], 1 + (number % 3)),
  '{"project":"dec0de00-0000-4000-a000-000000000001"}',
  [arrayElement(['toolset:admin', 'project:admin', 'environment:read'], 1 + (number % 3))],
  [arrayElement(['toolset:admin', 'project:admin', 'environment:read'], 1 + (number % 3))],
  [arrayElement(['toolset', 'project', 'environment'], 1 + (number % 3))],
  [arrayElement(['dec0de00-0000-4000-a000-000000005e01',
                 'dec0de00-0000-4000-a000-000000000002',
                 'dec0de00-0000-4000-a000-00000000ee01'], 1 + (number % 3))],
  ['{"project":"dec0de00-0000-4000-a000-000000000001"}'],
  [], [], [], [],
  toUInt32(3 + number % 5)
FROM numbers(13);

-- Risk findings mirror (ClickHouse read path for the risk overview): one row
-- per Postgres finding, same md5-derived ids so the two stores agree.
INSERT INTO risk_findings
  (id, created_at, organization_id, project_id, chat_message_id, chat_id,
   user_id, external_user_id, risk_policy_id, risk_policy_version, rule_id,
   description, source, confidence, category, tags, start_pos, end_pos,
   match_len, match_redacted, message_created_at)
SELECT
  toUUID(concat(substring(hm, 1, 8), '-', substring(hm, 9, 4), '-5', substring(hm, 14, 3), '-8',
                substring(hm, 18, 3), '-', substring(hm, 21, 12))),
  ts,
  'org_gram_demo_workspace',
  if(i % 3 = 0, 'dec0de00-0000-4000-a000-000000000002', 'dec0de00-0000-4000-a000-000000000001'),
  concat(substring(hmsg, 1, 8), '-', substring(hmsg, 9, 4), '-5', substring(hmsg, 14, 3), '-8',
         substring(hmsg, 18, 3), '-', substring(hmsg, 21, 12)),
  concat(substring(hchat, 1, 8), '-', substring(hchat, 9, 4), '-5', substring(hchat, 14, 3), '-8',
         substring(hchat, 18, 3), '-', substring(hchat, 21, 12)),
  arrayElement(['user_demo_amara', 'user_demo_jonas', 'user_demo_priya',
                'user_demo_mateo', 'user_demo_hana', 'user_demo_lucas'], 1 + (i % 6)),
  arrayElement(['amara@demo.getgram.ai', 'jonas@demo.getgram.ai', 'priya@demo.getgram.ai',
                'mateo@demo.getgram.ai', 'hana@demo.getgram.ai', 'lucas@demo.getgram.ai'], 1 + (i % 6)),
  if(i % 3 = 0, 'dec0de00-0000-4000-a000-00000000f002', 'dec0de00-0000-4000-a000-00000000f001'),
  1,
  'stripe-access-token',
  'Stripe live secret key found in tool output',
  'gitleaks',
  0.97,
  'secrets',
  ['secret', 'stripe'],
  55,
  87,
  32,
  concat('sk_l', repeat('*', 26), 'u0'),
  ts
FROM (
  SELECT
    (number + 1) * 7 AS i,
    lower(hex(MD5(concat('gram-demo-risk-', toString((number + 1) * 7))))) AS hm,
    lower(hex(MD5(concat('gram-demo-msg-', toString((number + 1) * 7), '-3')))) AS hmsg,
    lower(hex(MD5(concat('gram-demo-chat-', toString((number + 1) * 7))))) AS hchat,
    subtractMinutes(subtractHours(now64(9), 5 * toInt64((number + 1) * 7)),
                    13 * toInt64(((number + 1) * 7) % 7) - 2) AS ts
  FROM numbers(8)
);

-- Postflight asserts: rows landed, the cost/session MVs actually fired, and
-- nothing leaked outside the demo scope. throwIf aborts the script (non-zero
-- exit for the runner) when violated.
SELECT throwIf(
  (SELECT count() FROM telemetry_logs WHERE gram_project_id IN
     (toUUID('dec0de00-0000-4000-a000-000000000001'), toUUID('dec0de00-0000-4000-a000-000000000002'))
   ) < 400,
  'demo seed postflight: expected >= 400 demo telemetry rows');

SELECT throwIf(
  (SELECT uniqExact(chat_id) FROM chat_session_summaries WHERE gram_project_id IN
     (toUUID('dec0de00-0000-4000-a000-000000000001'), toUUID('dec0de00-0000-4000-a000-000000000002'))
   ) < 60,
  'demo seed postflight: chat_session_summaries_mv missing sessions');

SELECT throwIf(
  (SELECT count() FROM attribute_metrics_summaries WHERE gram_project_id IN
     (toUUID('dec0de00-0000-4000-a000-000000000001'), toUUID('dec0de00-0000-4000-a000-000000000002'))
     AND department_name != ''
   ) = 0,
  'demo seed postflight: attribute_metrics_summaries has no identity dimensions');

SELECT throwIf(
  (SELECT count() FROM authz_challenges WHERE organization_id = 'org_gram_demo_workspace') = 0,
  'demo seed postflight: authz_challenges empty');

SELECT throwIf(
  (SELECT count() FROM risk_findings WHERE organization_id = 'org_gram_demo_workspace') < 8,
  'demo seed postflight: risk_findings mirror missing rows');

SELECT throwIf(
  (SELECT count() FROM telemetry_logs
   WHERE toString(resource_attributes.gram.deployment.id) = 'demo-seed'
     AND gram_project_id NOT IN
       (toUUID('dec0de00-0000-4000-a000-000000000001'), toUUID('dec0de00-0000-4000-a000-000000000002'))
   ) > 0,
  'demo seed postflight: demo-seed rows leaked outside demo projects');
