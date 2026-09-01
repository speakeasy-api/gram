-- Demo org seed — ClickHouse side.
--
-- Regenerates all demo-org telemetry. Every statement is scoped to the fixed
-- demo project UUIDs / demo org id (telemetry_logs has no org column; the
-- demo project ids ARE the isolation boundary — they must match
-- seed/demo/postgres.sql).
--
-- The MVs (trace/metrics/attribute_metrics/chat_token/chat_session summaries,
-- attribute_keys, spend_rule_usage) fire on INSERT, and a DELETE on
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
--   Prod:  run daily by the infra cron AFTER demo.ensure_demo_org() on
--          Postgres (ClickHouse has no procedural functions, hence a script).
--   Demo:  `mise run seed:demo` applies the same statements locally.
--   Local: `mise run seed` rewrites the demo constants to the dev-idp org
--          first (demoseed.Spec) and seeds that tenant instead.

SET lightweight_deletes_sync = 1;

-- Scoped deletes: telemetry source + every MV target + the org-keyed tables.
-- Lightweight DELETEs, not ALTER TABLE ... DELETE: a heavy mutation on
-- telemetry_logs rewrites every column of every part that contains demo rows
-- (~12 days of shared, all-tenant partitions — the partition key is time-only,
-- so project id cannot prune), which previously starved merges in prod. A
-- lightweight delete only writes the _row_exists mask for matching parts and
-- hides the rows as soon as the statement returns; physical cleanup happens in
-- background merges.
DELETE FROM telemetry_logs WHERE gram_project_id IN
  (toUUID('dec0de00-0000-4000-a000-000000000001'));
DELETE FROM trace_summaries WHERE gram_project_id IN
  (toUUID('dec0de00-0000-4000-a000-000000000001'));
DELETE FROM metrics_summaries WHERE gram_project_id IN
  (toUUID('dec0de00-0000-4000-a000-000000000001'));
DELETE FROM attribute_metrics_summaries WHERE gram_project_id IN
  (toUUID('dec0de00-0000-4000-a000-000000000001'));
DELETE FROM chat_token_summaries WHERE gram_project_id IN
  (toUUID('dec0de00-0000-4000-a000-000000000001'));
DELETE FROM chat_session_summaries WHERE gram_project_id IN
  (toUUID('dec0de00-0000-4000-a000-000000000001'));
DELETE FROM spend_rule_usage_summaries WHERE gram_project_id IN
  (toUUID('dec0de00-0000-4000-a000-000000000001'));
DELETE FROM attribute_keys WHERE gram_project_id IN
  (toUUID('dec0de00-0000-4000-a000-000000000001'));
DELETE FROM shadow_mcp_inventory_urls WHERE gram_project_id IN
  (toUUID('dec0de00-0000-4000-a000-000000000001'));
DELETE FROM authz_challenges WHERE organization_id = 'org_gram_demo_workspace';
DELETE FROM risk_findings WHERE organization_id = 'org_gram_demo_workspace';
DELETE FROM skill_session_versions WHERE organization_id = 'org_gram_demo_workspace';
DELETE FROM skill_efficacy_scores WHERE organization_id = 'org_gram_demo_workspace';

-- Tool-execution rows: 3-12 per chat (hash-picked, so busy chats and quick
-- ones both exist). gram.toolset.slug makes the Insights CTE's direct branch
-- classify each trace as hosted MCP traffic; unique per-call trace ids keep
-- trace_summaries from merging them. Failures cluster into a process_refund
-- incident over the last ~3 days plus a low background rate (~5% overall)
-- instead of an evenly spread modulus.
INSERT INTO telemetry_logs
  (time_unix_nano, observed_time_unix_nano, severity_text, body, trace_id,
   attributes, resource_attributes, gram_project_id, gram_urn, service_name, gram_chat_id)
SELECT
  nano + toInt64(k) * 75000000000 + toInt64(cityHash64('gap', i, k) % 45000000000),
  nano + toInt64(k) * 75000000000 + toInt64(cityHash64('gap', i, k) % 45000000000),
  'INFO',
  concat('Tool call: ', tool_name),
  lower(hex(MD5(concat('gram-demo-tooltrace-', toString(i), '-', toString(k))))),
  concat(
    '{"gram.tool.urn":"tools:http:acme:', tool_name, '"',
    ',"gram.tool.name":"', tool_name, '"',
    ',"gram.toolset.slug":"', if(i % 5 = 0, 'acme-ops', 'acme-support-tools'), '"',
    ',"http.response.status_code":', toString(if(
        (tool_name = 'process_refund' AND day_off <= 2 AND cityHash64('err', i, k) % 2 = 0)
        OR cityHash64('errbg', i, k) % 30 = 0, 500, 200)),
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
    arrayJoin(range(1, toUInt64(4 + reinterpretAsUInt8(unhex(substring(h, 9, 2))) % 10))) AS k,
    arrayElement([3, 3, 3, 3, 3, 1, 1, 1, 1, 4, 4, 4, 2, 2, 5, 6],
                 1 + reinterpretAsUInt8(unhex(substring(h, 13, 2))) % 16) AS uidx,
    lower(hex(MD5(concat('gram-demo-chat-', toString(number + 1))))) AS h,
    concat(substring(h, 1, 8), '-', substring(h, 9, 4), '-5', substring(h, 14, 3), '-8',
           substring(h, 18, 3), '-', substring(h, 21, 12)) AS chat_id,
    toUUID('dec0de00-0000-4000-a000-000000000001') AS proj,
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
                 1 + (cityHash64('tool', number, k) % 8)) AS tool_name,
    arrayElement([0, 0, 1, 1, 1, 2, 3, 3, 3, 3, 4, 5, 5, 7, 8, 11],
                 1 + reinterpretAsUInt8(unhex(substring(h, 1, 2))) % 16) AS day_off,
    arrayElement([8, 9, 9, 10, 10, 11, 11, 13, 14, 14, 15, 16, 16, 17, 18, 20],
                 1 + reinterpretAsUInt8(unhex(substring(h, 3, 2))) % 16) AS hour_off,
    toDateTime64(toStartOfDay(now()), 9)
      - toIntervalDay(day_off) + toIntervalHour(hour_off)
      + toIntervalMinute(reinterpretAsUInt8(unhex(substring(h, 5, 2))) % 60) AS ts0,
    if(ts0 > now64(9) - toIntervalMinute(30), ts0 - toIntervalDay(1), ts0) AS chat_dt,
    toUnixTimestamp64Nano(chat_dt) AS nano
  FROM numbers(180)
);

-- An agent that reports only an id for itself, and no address at all. The
-- Identities roster classifies a subject by what it can be keyed on, so
-- without this the demo org has no Agent row to show. Surfaced from the raw
-- logs rather than the agent-metrics view, which is keyed by email. Cleaned up
-- by the same project-scoped telemetry_logs delete as every other row here.
INSERT INTO telemetry_logs
  (time_unix_nano, observed_time_unix_nano, severity_text, body, trace_id,
   attributes, resource_attributes, gram_project_id, gram_urn, service_name)
SELECT
  nano,
  nano,
  'INFO',
  concat('Tool call: ', tool_name),
  lower(hex(MD5(concat('gram-demo-unattributed-', toString(i))))),
  concat(
    '{"gram.tool.urn":"tools:http:acme:', tool_name, '"',
    ',"gram.tool.name":"', tool_name, '"',
    ',"gram.toolset.slug":"acme-support-tools"',
    ',"http.response.status_code":200',
    ',"http.server.request.duration":0.42',
    ',"gram.project.id":"', toString(proj), '"',
    concat(',"user.id":"', actor, '"'),
    -- Also the external user id: that is the key every identity read path
    -- filters a non-directory actor by (external:<id> resolves to exactly
    -- this), so reporting only user.id leaves the agent's own page empty
    -- while the roster still lists it.
    concat(',"gram.external_user.id":"', actor, '"'),
    ',"gram.hook.source":"codex"}'
  ),
  '{"gram.deployment.id":"demo-seed"}',
  proj,
  concat('tools:http:acme:', tool_name),
  'gram-mcp-gateway'
FROM (
  SELECT
    number + 1 AS i,
    toUUID('dec0de00-0000-4000-a000-000000000001') AS proj,
    'svc-nightly-triage' AS actor,
    arrayElement(['search_logs', 'get_metrics', 'fetch_traces', 'check_health'],
                 1 + (cityHash64('unattr', number) % 4)) AS tool_name,
    toDateTime64(toStartOfDay(now()), 9)
      - toIntervalDay(1 + (number % 6)) + toIntervalHour(9 + (number % 8)) AS ts0,
    toUnixTimestamp64Nano(ts0) AS nano
  FROM numbers(12)
);

-- A person the directory has never heard of: an address that matches no
-- member, so the roster can show an Unattributed row beside the members and
-- the agent. It has to be an api_request row rather than a tool call because
-- the agent-metrics view only admits the agent surfaces, and that view is what
-- the roster reads for email-keyed identities.
INSERT INTO telemetry_logs
  (time_unix_nano, observed_time_unix_nano, severity_text, body, trace_id,
   attributes, resource_attributes, gram_project_id, gram_urn, service_name,
   gram_chat_id)
SELECT
  nano,
  nano,
  'INFO',
  'claude_code.api_request',
  lower(hex(MD5(concat('gram-demo-unattributed-api-', toString(i))))),
  concat(
    '{"prompt.id":"demo-unattributed-prompt-', toString(i), '"',
    ',"event.name":"api_request"',
    ',"gen_ai.response.id":"', resp_id, '"',
    ',"input_tokens":', toString(2200 + (i * 37) % 900),
    ',"output_tokens":', toString(180 + (i * 11) % 220),
    ',"cache_read_tokens":', toString(9000 + (i * 53) % 4000),
    ',"cache_creation_tokens":600',
    ',"cost_usd":0.1841',
    ',"model":"claude-sonnet-4-6"',
    ',"query_source":"user"',
    ',"gen_ai.conversation.id":"', chat_id, '"',
    ',"gram.project.id":"', toString(proj), '"',
    ',"user.email":"', actor, '"',
    ',"gram.external_user.id":"', actor, '"',
    ',"gram.hook.source":"claude-code"}'
  ),
  '{"gram.deployment.id":"demo-seed"}',
  proj,
  'claude-code:otel:logs',
  'claude-code',
  chat_id
FROM (
  SELECT
    number + 1 AS i,
    toUUID('dec0de00-0000-4000-a000-000000000001') AS proj,
    -- In the demo domain so the tenant rewrite reaches it, but deliberately
    -- not one of the seeded members: that mismatch is the whole point.
    'ana.vidal@demo.getgram.ai' AS actor,
    concat('msg_', substring(lower(hex(MD5(concat('gram-demo-unattributed-resp-', toString(number + 1))))), 1, 24)) AS resp_id,
    lower(hex(MD5(concat('gram-demo-unattributed-chat-', toString(number + 1))))) AS ch,
    concat(substring(ch, 1, 8), '-', substring(ch, 9, 4), '-5', substring(ch, 14, 3), '-8',
           substring(ch, 18, 3), '-', substring(ch, 21, 12)) AS chat_id,
    toDateTime64(toStartOfDay(now()), 9)
      - toIntervalDay(1 + (number % 5)) + toIntervalHour(10 + (number % 6)) AS ts0,
    toUnixTimestamp64Nano(ts0) AS nano
  FROM numbers(12)
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
    ',"gen_ai.response.id":"msg_', substring(lower(hex(MD5(concat('gram-demo-respid-', toString(i), '-', toString(turn))))), 1, 24), '"',
    ',"input_tokens":', toString(in_tok),
    ',"output_tokens":', toString(out_tok),
    ',"cache_read_tokens":', toString(in_tok * 6),
    ',"cache_creation_tokens":', toString(intDiv(in_tok, 4)),
    ',"cost_usd":', toString(round((in_tok * 57375 + out_tok * 150000) / 10000000000, 4)),
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
    arrayElement([3, 3, 3, 3, 3, 1, 1, 1, 1, 4, 4, 4, 2, 2, 5, 6],
                 1 + reinterpretAsUInt8(unhex(substring(h, 13, 2))) % 16) AS uidx,
    lower(hex(MD5(concat('gram-demo-chat-', toString(number + 1))))) AS h,
    concat(substring(h, 1, 8), '-', substring(h, 9, 4), '-5', substring(h, 14, 3), '-8',
           substring(h, 18, 3), '-', substring(h, 21, 12)) AS chat_id,
    toUUID('dec0de00-0000-4000-a000-000000000001') AS proj,
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
    toUInt64(500000 + cityHash64('in', number, turn) % 3000000) AS in_tok,
    toUInt64(2000 + cityHash64('out', number, turn) % 28000) AS out_tok,
    arrayElement([0, 0, 1, 1, 1, 2, 3, 3, 3, 3, 4, 5, 5, 7, 8, 11],
                 1 + reinterpretAsUInt8(unhex(substring(h, 1, 2))) % 16) AS day_off,
    arrayElement([8, 9, 9, 10, 10, 11, 11, 13, 14, 14, 15, 16, 16, 17, 18, 20],
                 1 + reinterpretAsUInt8(unhex(substring(h, 3, 2))) % 16) AS hour_off,
    toDateTime64(toStartOfDay(now()), 9)
      - toIntervalDay(day_off) + toIntervalHour(hour_off)
      + toIntervalMinute(reinterpretAsUInt8(unhex(substring(h, 5, 2))) % 60) AS ts0,
    if(ts0 > now64(9) - toIntervalMinute(30), ts0 - toIntervalDay(1), ts0) AS chat_dt,
    toUnixTimestamp64Nano(chat_dt) AS nano
  FROM numbers(180)
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
    ',"success":', if(cityHash64('cres', i, k) % 15 = 0, 'false', 'true'),
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
    arrayElement([3, 3, 3, 3, 3, 1, 1, 1, 1, 4, 4, 4, 2, 2, 5, 6],
                 1 + reinterpretAsUInt8(unhex(substring(h, 13, 2))) % 16) AS uidx,
    lower(hex(MD5(concat('gram-demo-chat-', toString(number + 1))))) AS h,
    concat(substring(h, 1, 8), '-', substring(h, 9, 4), '-5', substring(h, 14, 3), '-8',
           substring(h, 18, 3), '-', substring(h, 21, 12)) AS chat_id,
    toUUID('dec0de00-0000-4000-a000-000000000001') AS proj,
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
                 1 + (cityHash64('tool', number, k) % 8)) AS tool_name,
    arrayElement([0, 0, 1, 1, 1, 2, 3, 3, 3, 3, 4, 5, 5, 7, 8, 11],
                 1 + reinterpretAsUInt8(unhex(substring(h, 1, 2))) % 16) AS day_off,
    arrayElement([8, 9, 9, 10, 10, 11, 11, 13, 14, 14, 15, 16, 16, 17, 18, 20],
                 1 + reinterpretAsUInt8(unhex(substring(h, 3, 2))) % 16) AS hour_off,
    toDateTime64(toStartOfDay(now()), 9)
      - toIntervalDay(day_off) + toIntervalHour(hour_off)
      + toIntervalMinute(reinterpretAsUInt8(unhex(substring(h, 5, 2))) % 60) AS ts0,
    if(ts0 > now64(9) - toIntervalMinute(30), ts0 - toIntervalDay(1), ts0) AS chat_dt,
    toUnixTimestamp64Nano(chat_dt) AS nano
  FROM numbers(180)
  WHERE (number + 1) % 2 = 1
);

-- Odd chats: 1-2 Skill hook rows each with a hash-weighted skill pick —
-- feeds the Insights "Skill Usage" / "Users per Skill" panels (skill_name
-- materializes only when gram.tool.name = 'Skill'). Deliberately looser than
-- the efficacy tables' fixed skill-per-chat formula: these rows only drive
-- the usage charts, which should not look like a perfect rotation.
INSERT INTO telemetry_logs
  (time_unix_nano, observed_time_unix_nano, severity_text, body, trace_id,
   attributes, resource_attributes, gram_project_id, gram_urn, service_name, gram_chat_id)
SELECT
  nano + toInt64(45000000000 + (j - 1) * 420000000000 + cityHash64('sklt', i, j) % 300000000000),
  nano + toInt64(45000000000 + (j - 1) * 420000000000 + cityHash64('sklt', i, j) % 300000000000),
  'INFO',
  'Hook: Skill invoked',
  lower(hex(MD5(concat('gram-demo-skilltrace-', toString(i), '-', toString(j))))),
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
    arrayJoin(range(1, toUInt64(2 + reinterpretAsUInt8(unhex(substring(h, 11, 2))) % 2))) AS j,
    arrayElement([3, 3, 3, 3, 3, 1, 1, 1, 1, 4, 4, 4, 2, 2, 5, 6],
                 1 + reinterpretAsUInt8(unhex(substring(h, 13, 2))) % 16) AS uidx,
    lower(hex(MD5(concat('gram-demo-chat-', toString(number + 1))))) AS h,
    concat(substring(h, 1, 8), '-', substring(h, 9, 4), '-5', substring(h, 14, 3), '-8',
           substring(h, 18, 3), '-', substring(h, 21, 12)) AS chat_id,
    toUUID('dec0de00-0000-4000-a000-000000000001') AS proj,
    arrayElement(['amara@demo.getgram.ai', 'jonas@demo.getgram.ai', 'priya@demo.getgram.ai',
                  'mateo@demo.getgram.ai', 'hana@demo.getgram.ai', 'lucas@demo.getgram.ai'], uidx) AS email,
    arrayElement(['amara-mbp.local', 'jonas-mbp.local', 'priya-mbp.local', 'mateo-mbp.local', 'hana-mbp.local', 'lucas-mbp.local'], uidx) AS hostname,
    arrayElement(['triage-incident', 'support-refunds', 'triage-incident', 'runbook',
                  'support-refunds', 'triage-incident', 'support-refunds', 'runbook'],
                 1 + toUInt32(cityHash64('skl', i, j) % 8)) AS skill,
    arrayElement([0, 0, 1, 1, 1, 2, 3, 3, 3, 3, 4, 5, 5, 7, 8, 11],
                 1 + reinterpretAsUInt8(unhex(substring(h, 1, 2))) % 16) AS day_off,
    arrayElement([8, 9, 9, 10, 10, 11, 11, 13, 14, 14, 15, 16, 16, 17, 18, 20],
                 1 + reinterpretAsUInt8(unhex(substring(h, 3, 2))) % 16) AS hour_off,
    toDateTime64(toStartOfDay(now()), 9)
      - toIntervalDay(day_off) + toIntervalHour(hour_off)
      + toIntervalMinute(reinterpretAsUInt8(unhex(substring(h, 5, 2))) % 60) AS ts0,
    if(ts0 > now64(9) - toIntervalMinute(30), ts0 - toIntervalDay(1), ts0) AS chat_dt,
    toUnixTimestamp64Nano(chat_dt) AS nano
  FROM numbers(180)
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
    ',"gen_ai.response.id":"msg_', substring(lower(hex(MD5(concat('gram-demo-cursor-respid-', toString(i))))), 1, 24), '"',
    ',"gen_ai.usage.input_tokens":', toString(in_tok),
    ',"gen_ai.usage.output_tokens":', toString(out_tok),
    ',"gen_ai.usage.cache_read.input_tokens":', toString(in_tok * 4),
    ',"gen_ai.usage.cost":', toString(round((in_tok * 42 + out_tok * 150) / 10000000, 6)),
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
    arrayElement([3, 3, 3, 3, 3, 1, 1, 1, 1, 4, 4, 4, 2, 2, 5, 6],
                 1 + reinterpretAsUInt8(unhex(substring(h, 13, 2))) % 16) AS uidx,
    lower(hex(MD5(concat('gram-demo-chat-', toString(number + 1))))) AS h,
    concat(substring(h, 1, 8), '-', substring(h, 9, 4), '-5', substring(h, 14, 3), '-8',
           substring(h, 18, 3), '-', substring(h, 21, 12)) AS chat_id,
    toUUID('dec0de00-0000-4000-a000-000000000001') AS proj,
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
    toUInt64(800000 + cityHash64('in', number) % 7000000) AS in_tok,
    toUInt64(3000 + cityHash64('out', number) % 40000) AS out_tok,
    arrayElement([0, 0, 1, 1, 1, 2, 3, 3, 3, 3, 4, 5, 5, 7, 8, 11],
                 1 + reinterpretAsUInt8(unhex(substring(h, 1, 2))) % 16) AS day_off,
    arrayElement([8, 9, 9, 10, 10, 11, 11, 13, 14, 14, 15, 16, 16, 17, 18, 20],
                 1 + reinterpretAsUInt8(unhex(substring(h, 3, 2))) % 16) AS hour_off,
    toDateTime64(toStartOfDay(now()), 9)
      - toIntervalDay(day_off) + toIntervalHour(hour_off)
      + toIntervalMinute(reinterpretAsUInt8(unhex(substring(h, 5, 2))) % 60) AS ts0,
    if(ts0 > now64(9) - toIntervalMinute(30), ts0 - toIntervalDay(1), ts0) AS chat_dt,
    toUnixTimestamp64Nano(chat_dt) AS nano
  FROM numbers(180)
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
    ',"gram.hook.event":"', if(cityHash64('hfail', i, k) % 16 = 0, 'PostToolUseFailure', 'PostToolUse'), '"',
    ',"gram.tool.name":"', tool_name, '"',
    ',"gram.tool_call.source":"acme-internal-mcp"',
    if(cityHash64('hfail', i, k) % 16 = 0,
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
    arrayElement([3, 3, 3, 3, 3, 1, 1, 1, 1, 4, 4, 4, 2, 2, 5, 6],
                 1 + reinterpretAsUInt8(unhex(substring(h, 13, 2))) % 16) AS uidx,
    lower(hex(MD5(concat('gram-demo-chat-', toString(number + 1))))) AS h,
    concat(substring(h, 1, 8), '-', substring(h, 9, 4), '-5', substring(h, 14, 3), '-8',
           substring(h, 18, 3), '-', substring(h, 21, 12)) AS chat_id,
    toUUID('dec0de00-0000-4000-a000-000000000001') AS proj,
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
                 1 + (cityHash64('tool', number, k) % 8)) AS tool_name,
    arrayElement([0, 0, 1, 1, 1, 2, 3, 3, 3, 3, 4, 5, 5, 7, 8, 11],
                 1 + reinterpretAsUInt8(unhex(substring(h, 1, 2))) % 16) AS day_off,
    arrayElement([8, 9, 9, 10, 10, 11, 11, 13, 14, 14, 15, 16, 16, 17, 18, 20],
                 1 + reinterpretAsUInt8(unhex(substring(h, 3, 2))) % 16) AS hour_off,
    toDateTime64(toStartOfDay(now()), 9)
      - toIntervalDay(day_off) + toIntervalHour(hour_off)
      + toIntervalMinute(reinterpretAsUInt8(unhex(substring(h, 5, 2))) % 60) AS ts0,
    if(ts0 > now64(9) - toIntervalMinute(30), ts0 - toIntervalDay(1), ts0) AS chat_dt,
    toUnixTimestamp64Nano(chat_dt) AS nano
  FROM numbers(180)
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
    ',"gram.chat_analysis.scored_cost":', toString(round((5000000 + cityHash64('sc', i) % 40000000) / 1000000, 4)),
    ',"gram.chat_analysis.scored_tokens":', toString(2000000 + cityHash64('st', i) % 12000000),
    ',"gen_ai.conversation.id":"', chat_id, '"',
    ',"gen_ai.response.model":"', if(i % 2 = 1, 'claude-sonnet-4-6', 'gpt-5.6'), '"',
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
    ',"gram.provider":"', if(i % 2 = 1, 'anthropic', if(cityHash64('model', i - 1) % 2 = 1, 'openai', 'anthropic')), '"',
    ',"gram.account_type":"', if(email = 'mateo@demo.getgram.ai', 'personal', 'team'), '"',
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
    arrayElement([3, 3, 3, 3, 3, 1, 1, 1, 1, 4, 4, 4, 2, 2, 5, 6],
                 1 + reinterpretAsUInt8(unhex(substring(h, 13, 2))) % 16) AS uidx,
    lower(hex(MD5(concat('gram-demo-chat-', toString(number + 1))))) AS h,
    concat(substring(h, 1, 8), '-', substring(h, 9, 4), '-5', substring(h, 14, 3), '-8',
           substring(h, 18, 3), '-', substring(h, 21, 12)) AS chat_id,
    toUUID('dec0de00-0000-4000-a000-000000000001') AS proj,
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
    arrayElement([0, 0, 1, 1, 1, 2, 3, 3, 3, 3, 4, 5, 5, 7, 8, 11],
                 1 + reinterpretAsUInt8(unhex(substring(h, 1, 2))) % 16) AS day_off,
    arrayElement([8, 9, 9, 10, 10, 11, 11, 13, 14, 14, 15, 16, 16, 17, 18, 20],
                 1 + reinterpretAsUInt8(unhex(substring(h, 3, 2))) % 16) AS hour_off,
    toDateTime64(toStartOfDay(now()), 9)
      - toIntervalDay(day_off) + toIntervalHour(hour_off)
      + toIntervalMinute(reinterpretAsUInt8(unhex(substring(h, 5, 2))) % 60) AS ts0,
    if(ts0 > now64(9) - toIntervalMinute(30), ts0 - toIntervalDay(1), ts0) AS chat_dt,
    toUnixTimestamp64Nano(chat_dt) AS nano
  FROM numbers(180)
);

-- Shadow MCP inventory + companion hook telemetry (Shadow MCP page list,
-- call/user counts).
INSERT INTO shadow_mcp_inventory_urls
  (gram_project_id, canonical_server_url, url_host, server_name, first_seen, last_seen, updated_at)
VALUES
  (toUUID('dec0de00-0000-4000-a000-000000000001'), 'https://api.githubcopilot.com/mcp',
   'api.githubcopilot.com', 'GitHub', now64(9) - INTERVAL 30 DAY, now64(9) - INTERVAL 2 HOUR, now64(9)),
  (toUUID('dec0de00-0000-4000-a000-000000000001'), 'https://mcp.notion.com/mcp',
   'mcp.notion.com', 'Notion', now64(9) - INTERVAL 29 DAY, now64(9) - INTERVAL 4 HOUR, now64(9)),
  (toUUID('dec0de00-0000-4000-a000-000000000001'), 'https://mcp.linear.app/mcp',
   'mcp.linear.app', 'Linear', now64(9) - INTERVAL 28 DAY, now64(9) - INTERVAL 6 HOUR, now64(9)),
  (toUUID('dec0de00-0000-4000-a000-000000000001'), 'https://mcp.slack.com/mcp',
   'mcp.slack.com', 'Slack', now64(9) - INTERVAL 26 DAY, now64(9) - INTERVAL 8 HOUR, now64(9)),
  (toUUID('dec0de00-0000-4000-a000-000000000001'), 'https://mcp.sentry.dev/mcp',
   'mcp.sentry.dev', 'Sentry', now64(9) - INTERVAL 25 DAY, now64(9) - INTERVAL 10 HOUR, now64(9)),
  (toUUID('dec0de00-0000-4000-a000-000000000001'), 'https://mcp.datadoghq.com/api/mcp',
   'mcp.datadoghq.com', 'Datadog', now64(9) - INTERVAL 23 DAY, now64(9) - INTERVAL 12 HOUR, now64(9)),
  (toUUID('dec0de00-0000-4000-a000-000000000001'), 'https://mcp.cloudflare.com/mcp',
   'mcp.cloudflare.com', 'Cloudflare', now64(9) - INTERVAL 22 DAY, now64(9) - INTERVAL 14 HOUR, now64(9)),
  (toUUID('dec0de00-0000-4000-a000-000000000001'), 'https://mcp.stripe.com/mcp',
   'mcp.stripe.com', 'Stripe', now64(9) - INTERVAL 20 DAY, now64(9) - INTERVAL 16 HOUR, now64(9)),
  (toUUID('dec0de00-0000-4000-a000-000000000001'), 'https://mcp.figma.com/mcp',
   'mcp.figma.com', 'Figma', now64(9) - INTERVAL 19 DAY, now64(9) - INTERVAL 18 HOUR, now64(9)),
  (toUUID('dec0de00-0000-4000-a000-000000000001'), 'https://postgres.internal.example.com/mcp',
   'postgres.internal.example.com', 'Postgres Explorer', now64(9) - INTERVAL 17 DAY, now64(9) - INTERVAL 20 HOUR, now64(9)),
  (toUUID('dec0de00-0000-4000-a000-000000000001'), 'https://support-tools.example.com/mcp',
   'support-tools.example.com', 'Customer Support', now64(9) - INTERVAL 16 DAY, now64(9) - INTERVAL 22 HOUR, now64(9)),
  (toUUID('dec0de00-0000-4000-a000-000000000001'), 'https://prod-admin.example.com/mcp',
   'prod-admin.example.com', 'Production Admin', now64(9) - INTERVAL 14 DAY, now64(9) - INTERVAL 24 HOUR, now64(9)),
  (toUUID('dec0de00-0000-4000-a000-000000000001'), 'https://warehouse.example.com/mcp',
   'warehouse.example.com', 'Data Warehouse', now64(9) - INTERVAL 13 DAY, now64(9) - INTERVAL 26 HOUR, now64(9)),
  (toUUID('dec0de00-0000-4000-a000-000000000001'), 'https://incidents.example.com/mcp',
   'incidents.example.com', 'Incident Commander', now64(9) - INTERVAL 11 DAY, now64(9) - INTERVAL 28 HOUR, now64(9)),
  (toUUID('dec0de00-0000-4000-a000-000000000001'), 'https://payroll.example.com/mcp',
   'payroll.example.com', 'Payroll Assistant', now64(9) - INTERVAL 10 DAY, now64(9) - INTERVAL 30 HOUR, now64(9));

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
    ',"gram.tool.name":"', arrayElement(['search_issues', 'search_pages', 'list_issues', 'post_message', 'list_errors', 'query_metrics', 'list_zones', 'create_charge', 'get_file', 'run_query', 'lookup_ticket', 'restart_service', 'run_report', 'page_oncall', 'run_payroll'], sidx), '"',
    ',"gram.mcp.server_url":"', arrayElement(
        ['https://api.githubcopilot.com/mcp', 'https://mcp.notion.com/mcp', 'https://mcp.linear.app/mcp',
         'https://mcp.slack.com/mcp', 'https://mcp.sentry.dev/mcp', 'https://mcp.datadoghq.com/api/mcp',
         'https://mcp.cloudflare.com/mcp', 'https://mcp.stripe.com/mcp', 'https://mcp.figma.com/mcp',
         'https://postgres.internal.example.com/mcp', 'https://support-tools.example.com/mcp',
         'https://prod-admin.example.com/mcp', 'https://warehouse.example.com/mcp',
         'https://incidents.example.com/mcp', 'https://payroll.example.com/mcp'], sidx), '"',
    ',"gen_ai.tool.call.result":"ok"',
    ',"gram.project.id":"', toString(proj), '"',
    ',"user.email":"', arrayElement(
        ['amara@demo.getgram.ai', 'jonas@demo.getgram.ai', 'priya@demo.getgram.ai',
         'mateo@demo.getgram.ai', 'hana@demo.getgram.ai', 'lucas@demo.getgram.ai'],
        arrayElement([3, 3, 3, 1, 1, 4, 2, 5], 1 + toUInt32(cityHash64('shu', number) % 8))), '"}'
  ),
  '{"gram.deployment.id":"demo-seed"}',
  proj,
  concat('hooks:', arrayElement(['search_issues', 'search_pages', 'list_issues', 'post_message', 'list_errors', 'query_metrics', 'list_zones', 'create_charge', 'get_file', 'run_query', 'lookup_ticket', 'restart_service', 'run_report', 'page_oncall', 'run_payroll'], sidx)),
  'gram-hooks',
  ''
FROM (
  SELECT
    number,
    1 + toUInt32(cityHash64('sht', number) % 15) AS sidx,
    toUUID('dec0de00-0000-4000-a000-000000000001') AS proj,
    toUnixTimestamp64Nano(subtractMinutes(subtractHours(now64(9), 3 + toInt64(number) * 9),
                                          toInt64(cityHash64('shj', number) % 300))) AS nano
  FROM numbers(180)
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
  'dec0de00-0000-4000-a000-000000000001',
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
                'dec0de00-0000-4000-a000-000000000001',
                'dec0de00-0000-4000-a000-00000000ee01'], 1 + (number % 3)),
  '{"project":"dec0de00-0000-4000-a000-000000000001"}',
  [arrayElement(['toolset:admin', 'project:admin', 'environment:read'], 1 + (number % 3))],
  [arrayElement(['toolset:admin', 'project:admin', 'environment:read'], 1 + (number % 3))],
  [arrayElement(['toolset', 'project', 'environment'], 1 + (number % 3))],
  [arrayElement(['dec0de00-0000-4000-a000-000000005e01',
                 'dec0de00-0000-4000-a000-000000000001',
                 'dec0de00-0000-4000-a000-00000000ee01'], 1 + (number % 3))],
  ['{"project":"dec0de00-0000-4000-a000-000000000001"}'],
  [], [], [], [],
  toUInt32(3 + number % 5)
FROM numbers(13);

-- Risk findings mirror (the ClickHouse read path behind the risk overview,
-- the Risk Events listing and the Watchdog): one row per Postgres finding,
-- with the same md5-derived ids and the SAME weighted type draw as the
-- Postgres loop. Every array below is indexed by the finding type k and must
-- stay aligned with the type table in postgres.sql; the start/len values are
-- the fixed content prefixes there, so an edit to a prefix string is an edit
-- here too.
--
-- Beyond mirroring, this insert stamps the attribution columns the ingest
-- pipeline denormalizes and the Watchdog reads without touching Postgres:
-- chat_source (its App grouping), team (its Team grouping) and user_email
-- (its top-user display). Leaving them empty renders those groupings blank.
INSERT INTO risk_findings
  (id, created_at, organization_id, project_id, chat_message_id, chat_id,
   user_id, external_user_id, user_email, team, chat_source,
   risk_policy_id, risk_policy_version, rule_id,
   description, source, confidence, category, tags, start_pos, end_pos,
   match_len, match_redacted, surface, field, message_created_at,
   excluded_at, exclusion_id, false_positive_at, excluded_reason,
   excluded_detail)
SELECT
  toUUID(concat(substring(hm, 1, 8), '-', substring(hm, 9, 4), '-5', substring(hm, 14, 3), '-8',
                substring(hm, 18, 3), '-', substring(hm, 21, 12))),
  ts,
  'org_gram_demo_workspace',
  'dec0de00-0000-4000-a000-000000000001',
  concat(substring(hmsg, 1, 8), '-', substring(hmsg, 9, 4), '-5', substring(hmsg, 14, 3), '-8',
         substring(hmsg, 18, 3), '-', substring(hmsg, 21, 12)),
  concat(substring(hchat, 1, 8), '-', substring(hchat, 9, 4), '-5', substring(hchat, 14, 3), '-8',
         substring(hchat, 18, 3), '-', substring(hchat, 21, 12)),
  arrayElement(['user_demo_amara', 'user_demo_jonas', 'user_demo_priya',
                'user_demo_mateo', 'user_demo_hana', 'user_demo_lucas'], uidx),
  arrayElement(['amara@demo.getgram.ai', 'jonas@demo.getgram.ai', 'priya@demo.getgram.ai',
                'mateo@demo.getgram.ai', 'hana@demo.getgram.ai', 'lucas@demo.getgram.ai'], uidx),
  arrayElement(['amara@demo.getgram.ai', 'jonas@demo.getgram.ai', 'priya@demo.getgram.ai',
                'mateo@demo.getgram.ai', 'hana@demo.getgram.ai', 'lucas@demo.getgram.ai'], uidx),
  -- WorkOS directory department_name, matching demo_departments in postgres.sql.
  arrayElement(['Support Engineering', 'Support Engineering', 'Platform Engineering',
                'Platform Engineering', 'Billing Operations', 'Engineering Leadership'], uidx),
  surface_slug,
  arrayElement(['dec0de00-0000-4000-a000-00000000f001',
                'dec0de00-0000-4000-a000-00000000f001',
                'dec0de00-0000-4000-a000-00000000f001',
                'dec0de00-0000-4000-a000-00000000f007',
                'dec0de00-0000-4000-a000-00000000f002',
                'dec0de00-0000-4000-a000-00000000f001',
                'dec0de00-0000-4000-a000-00000000f006',
                'dec0de00-0000-4000-a000-00000000f006',
                'dec0de00-0000-4000-a000-00000000f006',
                'dec0de00-0000-4000-a000-00000000f002',
                'dec0de00-0000-4000-a000-00000000f003',
                'dec0de00-0000-4000-a000-00000000f008',
                'dec0de00-0000-4000-a000-00000000f007'], 1 + k),
  1,
  arrayElement(['stripe-access-token', 'aws-access-token', 'pii.credit_card',
                'pii.email_address', 'llm_judge', 'pii.us_ssn',
                'custom.sensitive_file_read', 'custom.env_secret_dump',
                'custom.ssrf_metadata_endpoint', 'prompt-injection.indirect',
                'cli.destructive_command', 'pii.topic_boundary_violation',
                'pii.phone_number'], 1 + k),
  arrayElement(['Stripe live secret key found in tool output',
                'AWS secret access key in tool output',
                'Credit card number in user message',
                'Customer email address in tool output',
                'Prompt injection attempt: instruction override + credential exfiltration',
                'US Social Security number in user message',
                'Agent reads of SSH keys, cloud credentials, or dotenv files outside the project (OWASP LLM02).',
                'Agent dumping the process environment, where CI/CD tokens and API keys live (OWASP LLM02).',
                'Agent-controlled requests to cloud metadata or loopback addresses (MCP security best practices).',
                'Injected instruction in retrieved content redirecting the agent to exfiltrate data',
                'Destructive database command issued through a tool call',
                'Conversation strayed outside the approved support topics',
                'Customer phone number in tool output'], 1 + k),
  arrayElement(['gitleaks', 'gitleaks', 'presidio', 'presidio', 'llm_judge',
                'presidio', 'custom', 'custom', 'custom', 'prompt_injection',
                'cli_destructive', 'presidio', 'presidio'], 1 + k),
  arrayElement([0.97, 0.95, 0.92, 0.88, 0.72, 0.94, 1.0, 1.0, 1.0, 0.81,
                0.99, 0.64, 0.83], 1 + k),
  -- categories.Classify(source, rule_id) computed at ingest; hardcoded here
  -- because ClickHouse cannot call it. Source-based categories win over the
  -- rule prefix, which is why k=4 is prompt_policy and k=9 prompt_injection.
  arrayElement(['secrets', 'secrets', 'financial', 'pii', 'prompt_policy',
                'government_ids', 'custom', 'custom', 'custom',
                'prompt_injection', 'cli_destructive', 'off_policy',
                'pii'], 1 + k),
  arrayElement([['secret', 'stripe'], ['secret', 'aws'], ['pii', 'pci'], ['pii'],
                ['prompt-injection'], ['pii', 'govid'],
                emptyArrayString(), emptyArrayString(), emptyArrayString(),
                ['prompt-injection', 'indirect'], ['destructive'], ['off-policy'],
                ['pii']], 1 + k),
  start_pos,
  start_pos + match_len,
  match_len,
  arrayElement([concat('sk_l', repeat('*', 26), 'u0'),
                concat('wJal', repeat('*', 32), 'MO'),
                concat(repeat('*', 15), if(i % 4 = 0, '1111', '6467')),
                '***@example.com',
                '',
                concat('412-', repeat('*', 5), '91'),
                concat('cat ', repeat('*', 26), 'ls'),
                concat('prin', repeat('*', 18), 'en'),
                concat('http', repeat('*', 59), 's/'),
                '',
                '',
                concat('walk', repeat('*', 34), 'an'),
                concat('+1-4', repeat('*', 9), '42')], 1 + k),
  'content',
  'content',
  ts,
  if(supp > 0, chat_dt + toIntervalHour(3), NULL),
  if(supp = 1, toUUID(if(k = 2, 'dec0de00-0000-4000-a000-00000000ec02',
                                'dec0de00-0000-4000-a000-00000000ec01')), NULL),
  if(supp IN (2, 3), chat_dt + toIntervalHour(3), NULL),
  arrayElement(['', 'rule', 'manual', 'automated'], 1 + supp),
  arrayElement(['', '', 'Known internal test fixture, not customer data',
                'placeholder_value'], 1 + supp)
FROM (
  SELECT
    number + 1 AS i,
    lower(hex(MD5(concat('gram-demo-chat-', toString(number + 1))))) AS hchat,
    lower(hex(MD5(concat('gram-demo-riskpick-', toString(number + 1))))) AS hp,
    arrayElement([0, 0, 1, 1, 1, 2, 3, 3, 3, 3, 4, 5, 5, 7, 8, 11],
                 1 + reinterpretAsUInt8(unhex(substring(hchat, 1, 2))) % 16) AS day_off,
    -- demo.risk_ftype(i): the weighted draw, with the trailing-3-days
    -- override that makes types 7 and 8 read as newly-emerged signals.
    if(day_off <= 2 AND reinterpretAsUInt8(unhex(substring(hp, 3, 2))) % 8 = 0,
       toInt16(7 + reinterpretAsUInt8(unhex(substring(hp, 5, 2))) % 2),
       toInt16(arrayElement([-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,
                     -1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,
                      3, 3, 3, 3, 3, 3, 3, 3,
                     12,12,12,12,12,
                      0, 0, 0, 0,
                      1, 1, 1,
                      6, 6, 6, 6, 6,
                     10,10,10,10,
                      2, 2, 2,
                      9, 9, 9,
                      4, 4,
                     11,11,
                      5, 5],
                    1 + reinterpretAsUInt8(unhex(substring(hp, 1, 2))) % 64))) AS k,
    -- Types 2, 4, 5 and 11 flag the opening user message (position 1); the
    -- rest a tool output at position 3 — must match the Postgres f_on_user.
    lower(hex(MD5(concat('gram-demo-risk-', toString(number + 1))))) AS hm,
    lower(hex(MD5(concat('gram-demo-msg-', toString(number + 1), '-',
                         if(k IN (2, 4, 5, 11), '1', '3'))))) AS hmsg,
    -- demo.chat_surface(i).
    if(i % 2 = 1, 'claude-code', if(i % 6 = 2, 'codex', 'cursor')) AS surface_slug,
    -- Byte offset of the match inside the message content: the length of the
    -- fixed prefix each type's content is built from in postgres.sql.
    arrayElement([58, 48, 41, 41, 0, 59, 16, 16, 17, 13, 16, 28, 42],
                 1 + k) AS start_pos,
    arrayElement([32, 38, 19, 22, 32, 11, 32, 24, 65, 56, 34, 40, 15],
                 1 + k) AS match_len,
    -- demo.risk_suppression(i), restricted to the same types, with the
    -- match-driven rule suppression taking precedence.
    if((k = 2 AND i % 4 = 0) OR (k = 3 AND i % 5 = 0), 1,
       if(k IN (3, 11, 12),
          arrayElement([0,0,0,0,0,0,0,0,0,0,0,2,2,2,3,3],
                       1 + reinterpretAsUInt8(unhex(substring(hp, 7, 2))) % 16),
          0)) AS supp,
    arrayElement([3, 3, 3, 3, 3, 1, 1, 1, 1, 4, 4, 4, 2, 2, 5, 6],
                 1 + reinterpretAsUInt8(unhex(substring(hchat, 13, 2))) % 16) AS uidx,
    arrayElement([8, 9, 9, 10, 10, 11, 11, 13, 14, 14, 15, 16, 16, 17, 18, 20],
                 1 + reinterpretAsUInt8(unhex(substring(hchat, 3, 2))) % 16) AS hour_off,
    toDateTime64(toStartOfDay(now()), 9)
      - toIntervalDay(day_off) + toIntervalHour(hour_off)
      + toIntervalMinute(reinterpretAsUInt8(unhex(substring(hchat, 5, 2))) % 60) AS ts0,
    if(ts0 > now64(9) - toIntervalMinute(30), ts0 - toIntervalDay(1), ts0) AS chat_dt,
    chat_dt + toIntervalMinute(2) AS ts
  FROM numbers(180)
)
WHERE k >= 0;

-- Skill efficacy mappings: one skill_session_versions row per Postgres
-- skill_observation (same det-uuid ids, same skill-per-chat formula
-- 1 + (((i-1)/2) % 3)). surface='dev' — the insights query joins scores to
-- mappings on (project, session, surface, skill, version).
INSERT INTO skill_session_versions
  (id, created_at, seen_at, organization_id, project_id, session_id,
   skill_id, skill_version_id, canonical_sha256, surface)
SELECT
  toUUID(concat(substring(ho, 1, 8), '-', substring(ho, 9, 4), '-5', substring(ho, 14, 3), '-8',
                substring(ho, 18, 3), '-', substring(ho, 21, 12))),
  ts, ts,
  'org_gram_demo_workspace',
  toUUID('dec0de00-0000-4000-a000-000000000001'),
  chat_id,
  arrayElement([toUUID('dec0de00-0000-4000-a000-0000000051a1'),
                toUUID('dec0de00-0000-4000-a000-0000000051a2'),
                toUUID('dec0de00-0000-4000-a000-0000000051a3')], sidx),
  if(sidx = 3 AND day_off <= 4, toUUID('dec0de00-0000-4000-a000-0000000052b3'),
     arrayElement([toUUID('dec0de00-0000-4000-a000-0000000052a1'),
                   toUUID('dec0de00-0000-4000-a000-0000000052a2'),
                   toUUID('dec0de00-0000-4000-a000-0000000052a3')], sidx)),
  '',
  'dev'
FROM (
  SELECT
    number + 1 AS i,
    arrayJoin(range(1, toUInt64(2 + ((number + 1) % 3)))) AS k,
    1 + (intDiv(number, 2) % 3) AS sidx,
    lower(hex(MD5(concat('gram-demo-skillobs-', toString(number + 1), '-', toString(k))))) AS ho,
    lower(hex(MD5(concat('gram-demo-chat-', toString(number + 1))))) AS h,
    concat(substring(h, 1, 8), '-', substring(h, 9, 4), '-5', substring(h, 14, 3), '-8',
           substring(h, 18, 3), '-', substring(h, 21, 12)) AS chat_id,
    arrayElement([0, 0, 1, 1, 1, 2, 3, 3, 3, 3, 4, 5, 5, 7, 8, 11],
                 1 + reinterpretAsUInt8(unhex(substring(h, 1, 2))) % 16) AS day_off,
    arrayElement([8, 9, 9, 10, 10, 11, 11, 13, 14, 14, 15, 16, 16, 17, 18, 20],
                 1 + reinterpretAsUInt8(unhex(substring(h, 3, 2))) % 16) AS hour_off,
    toDateTime64(toStartOfDay(now()), 9)
      - toIntervalDay(day_off) + toIntervalHour(hour_off)
      + toIntervalMinute(reinterpretAsUInt8(unhex(substring(h, 5, 2))) % 60) AS ts0,
    if(ts0 > now64(9) - toIntervalMinute(30), ts0 - toIntervalDay(1), ts0) AS chat_dt,
    chat_dt + toIntervalSecond(45) + toIntervalMinute(2 * (k - 1)) AS ts
  FROM numbers(180)
  WHERE (number + 1) % 2 = 1
);

-- Skill efficacy scores: one judged session per odd chat, joined to the k=1
-- mapping above. Per-skill quality profiles: support-refunds strong,
-- triage-incident middling, runbook weak with ignored/misapplied flags.
INSERT INTO skill_efficacy_scores
  (id, created_at, organization_id, project_id, session_id,
   skill_id, skill_version_id, canonical_sha256, surface, trace_id,
   gram_chat_id, score, rationale, est_turns_saved, est_minutes_saved,
   roi_confidence, flags, judge_model, judge_prompt_version)
SELECT
  toUUID(concat(substring(hs, 1, 8), '-', substring(hs, 9, 4), '-5', substring(hs, 14, 3), '-8',
                substring(hs, 18, 3), '-', substring(hs, 21, 12))),
  ts + toIntervalHour(2),
  'org_gram_demo_workspace',
  'dec0de00-0000-4000-a000-000000000001',
  chat_id,
  arrayElement([toUUID('dec0de00-0000-4000-a000-0000000051a1'),
                toUUID('dec0de00-0000-4000-a000-0000000051a2'),
                toUUID('dec0de00-0000-4000-a000-0000000051a3')], sidx),
  if(sidx = 3 AND day_off <= 4, toUUID('dec0de00-0000-4000-a000-0000000052b3'),
     arrayElement([toUUID('dec0de00-0000-4000-a000-0000000052a1'),
                   toUUID('dec0de00-0000-4000-a000-0000000052a2'),
                   toUUID('dec0de00-0000-4000-a000-0000000052a3')], sidx)),
  '',
  'dev',
  NULL,
  chat_id,
  -- Runbook v2 scores markedly lower than v1: surfaces the regression signal.
  least(0.99, greatest(0.05,
    arrayElement([0.87, 0.66, 0.42], sidx)
    - if(sidx = 3 AND day_off <= 4, 0.14, 0)
    + (toInt64(cityHash64('js', i) % 21) - 10) / 100)),
  arrayElement(['Agent followed the refund checklist and refused the pasted card number.',
                'Triage steps mostly followed but escalation criteria applied loosely.',
                'Runbook steps were skipped or applied out of order in this session.'], sidx),
  arrayElement([3, 2, 1], sidx) + (cityHash64('jt', i) % 3),
  arrayElement([13, 7, 2], sidx) + (cityHash64('jm', i) % 5),
  arrayElement(['high', 'med', 'low'], sidx),
  multiIf(
    sidx = 3 AND cityHash64('jf', i) % 3 = 0, ['ignored'],
    sidx = 3 AND cityHash64('jf', i) % 3 = 1, ['misapplied'],
    sidx = 2 AND cityHash64('jf', i) % 5 = 0, ['partially_followed'],
    sidx = 1 AND cityHash64('jf', i) % 7 = 0, ['partially_followed'],
    CAST([] AS Array(String))),
  'claude-sonnet-4-6',
  'v1'
FROM (
  SELECT
    number + 1 AS i,
    1 + (intDiv(number, 2) % 3) AS sidx,
    lower(hex(MD5(concat('gram-demo-skillscore-', toString(number + 1))))) AS hs,
    lower(hex(MD5(concat('gram-demo-chat-', toString(number + 1))))) AS h,
    concat(substring(h, 1, 8), '-', substring(h, 9, 4), '-5', substring(h, 14, 3), '-8',
           substring(h, 18, 3), '-', substring(h, 21, 12)) AS chat_id,
    arrayElement([0, 0, 1, 1, 1, 2, 3, 3, 3, 3, 4, 5, 5, 7, 8, 11],
                 1 + reinterpretAsUInt8(unhex(substring(h, 1, 2))) % 16) AS day_off,
    arrayElement([8, 9, 9, 10, 10, 11, 11, 13, 14, 14, 15, 16, 16, 17, 18, 20],
                 1 + reinterpretAsUInt8(unhex(substring(h, 3, 2))) % 16) AS hour_off,
    toDateTime64(toStartOfDay(now()), 9)
      - toIntervalDay(day_off) + toIntervalHour(hour_off)
      + toIntervalMinute(reinterpretAsUInt8(unhex(substring(h, 5, 2))) % 60) AS ts0,
    if(ts0 > now64(9) - toIntervalMinute(30), ts0 - toIntervalDay(1), ts0) AS chat_dt,
    chat_dt + toIntervalSecond(45) AS ts
  FROM numbers(180)
  WHERE (number + 1) % 2 = 1
);

-- Postflight asserts: rows landed, the cost/session MVs actually fired, and
-- nothing leaked outside the demo scope. throwIf aborts the script (non-zero
-- exit for the runner) when violated.
SELECT throwIf(
  (SELECT count() FROM telemetry_logs WHERE gram_project_id IN
     (toUUID('dec0de00-0000-4000-a000-000000000001'))
   ) < 1500,
  'demo seed postflight: expected >= 1500 demo telemetry rows');

SELECT throwIf(
  (SELECT uniqExact(chat_id) FROM chat_session_summaries WHERE gram_project_id IN
     (toUUID('dec0de00-0000-4000-a000-000000000001'))
   ) < 180,
  'demo seed postflight: chat_session_summaries_mv missing sessions');

-- Every request-shaped row carries gen_ai.response.id, the field the per-user
-- summary counts chat requests by. Without it "Chat requests" reads 0 on every
-- identity while the chat count beside it reads correctly, which looks like a
-- broken panel rather than absent data.
SELECT throwIf(
  (SELECT uniqExact(toString(attributes.gen_ai.response.id)) FROM telemetry_logs
   WHERE gram_project_id IN (toUUID('dec0de00-0000-4000-a000-000000000001'))
     AND toString(attributes.gen_ai.response.id) != ''
   ) < 180,
  'demo seed postflight: request rows missing gen_ai.response.id');

SELECT throwIf(
  (SELECT count() FROM attribute_metrics_summaries WHERE gram_project_id IN
     (toUUID('dec0de00-0000-4000-a000-000000000001'))
     AND department_name != ''
   ) = 0,
  'demo seed postflight: attribute_metrics_summaries has no identity dimensions');

SELECT throwIf(
  (SELECT count() FROM authz_challenges WHERE organization_id = 'org_gram_demo_workspace') = 0,
  'demo seed postflight: authz_challenges empty');

SELECT throwIf(
  (SELECT count() FROM skill_session_versions WHERE organization_id = 'org_gram_demo_workspace') < 90,
  'demo seed postflight: skill_session_versions missing rows');

SELECT throwIf(
  (SELECT count() FROM skill_efficacy_scores WHERE organization_id = 'org_gram_demo_workspace') < 90,
  'demo seed postflight: skill_efficacy_scores missing rows');

SELECT throwIf(
  (SELECT count() FROM risk_findings WHERE organization_id = 'org_gram_demo_workspace') < 90,
  'demo seed postflight: risk_findings mirror missing rows');

-- The Watchdog groups by these three denormalized columns. A mirror that
-- forgot them still lists signals, but every App/Team grouping and every
-- top-user row renders empty, which is the failure this catches.
SELECT throwIf(
  (SELECT count() FROM risk_findings
   WHERE organization_id = 'org_gram_demo_workspace'
     AND (chat_source = '' OR team = '' OR user_email = '')) > 0,
  'demo seed postflight: risk_findings missing chat_source/team/user_email attribution');

-- Fewer than four distinct rule clusters means the weighted type draw
-- collapsed and the Watchdog list is a flat rotation again.
SELECT throwIf(
  (SELECT uniqExact(rule_id) FROM risk_findings
   WHERE organization_id = 'org_gram_demo_workspace') < 8,
  'demo seed postflight: risk_findings cover too few rules to form signals');

SELECT throwIf(
  (SELECT count() FROM risk_findings
   WHERE organization_id = 'org_gram_demo_workspace'
     AND excluded_reason NOT IN ('', 'rule', 'manual', 'automated')) > 0,
  'demo seed postflight: risk_findings carry an unrecognized excluded_reason');

SELECT throwIf(
  (SELECT count() FROM telemetry_logs
   WHERE toString(resource_attributes.gram.deployment.id) = 'demo-seed'
     AND gram_project_id NOT IN
       (toUUID('dec0de00-0000-4000-a000-000000000001'))
   ) > 0,
  'demo seed postflight: demo-seed rows leaked outside demo projects');

-- The inverse closes the loop: every row under the demo projects must carry
-- the demo-seed marker, so the leak check above provably covers all seeded
-- telemetry (an insert that forgot the marker would slip past it).
SELECT throwIf(
  (SELECT count() FROM telemetry_logs
   WHERE gram_project_id IN
       (toUUID('dec0de00-0000-4000-a000-000000000001'))
     AND toString(resource_attributes.gram.deployment.id) != 'demo-seed'
   ) > 0,
  'demo seed postflight: demo-project telemetry rows missing the demo-seed marker');
