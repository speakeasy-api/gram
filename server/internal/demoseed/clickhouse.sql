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
DELETE FROM ai_detections WHERE organization_id = 'org_gram_demo_workspace';
DELETE FROM ai_scan_receipts WHERE organization_id = 'org_gram_demo_workspace';
DELETE FROM authz_challenges WHERE organization_id = 'org_gram_demo_workspace';
DELETE FROM risk_findings WHERE organization_id = 'org_gram_demo_workspace';
DELETE FROM skill_session_versions WHERE organization_id = 'org_gram_demo_workspace';
DELETE FROM skill_efficacy_scores WHERE organization_id = 'org_gram_demo_workspace';
DELETE FROM billing_meter_daily_summaries WHERE organization_id = 'org_gram_demo_workspace';
DELETE FROM billing_meter_readings_by_time WHERE organization_id = 'org_gram_demo_workspace';
DELETE FROM agent_events WHERE organization_id = 'org_gram_demo_workspace';

-- Inserts must never race rows from the previous seed generation. The Go
-- runner polls this same condition before advancing past the delete phase, and
-- this preflight keeps the invariant explicit for every script executor.
SELECT throwIf(
  (SELECT count() FROM telemetry_logs WHERE gram_project_id IN
     (toUUID('dec0de00-0000-4000-a000-000000000001'))
   )
  + (SELECT count() FROM billing_meter_readings_by_time
     WHERE organization_id = 'org_gram_demo_workspace')
  + (SELECT count() FROM billing_meter_daily_summaries
     WHERE organization_id = 'org_gram_demo_workspace') != 0,
  'demo seed preflight: source or summary rows remain after scoped deletes');

SELECT throwIf(
  (SELECT count() FROM agent_events
   WHERE organization_id = 'org_gram_demo_workspace') != 0,
  'demo seed preflight: agent_events rows remain after the scoped delete');

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
    -- Managed-agent calls retain the approving human email too: the actor must win.
    if(i % 4 = 0, concat(',"gram.event.source":"tool_call","gram.authorization.actor.type":"agent","gram.authorization.actor.id":"', managed_agent_id, '"'), ''),
    ',"gram.toolset.slug":"', if(i % 5 = 0, 'acme-ops', 'acme-support-tools'), '"',
    ',"http.response.status_code":', toString(if(failed, 500, 200)),
    ',"http.server.request.duration":', toString(round(0.05 + (cityHash64(i, k) % 200) / 100, 3)),
    -- The arguments and the result, as a real tool call records them. Without
    -- these the log detail sheet has nothing to show but the tool's name, and
    -- reports the payload as withheld by tool_io_logs.
    ',"gen_ai.tool.call.arguments":"', tool_args, '"',
    ',"gen_ai.tool.call.result":"', tool_result, '"',
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
    -- The account the call was made through, matching the chat rows below so
    -- an account-scoped read of someone's usage keeps its tool calls; without
    -- it every tool count drops to zero the moment a filter is applied. Only
    -- the Claude half can be personal: the two seeded personal accounts are
    -- Anthropic ones, so labelling a Cursor call personal would attribute it
    -- to an account that does not exist.
    ',"gram.account_type":"', if(hook = 'claude-code'
      AND (email = 'mateo@demo.getgram.ai'
        OR (email = 'lucas@demo.getgram.ai' AND cityHash64('acct', i) % 3 = 0)),
      'personal', 'team'), '"',
    ',"gram.hook.source":"', hook, '"',
    -- What the caller said it was at the MCP initialize handshake. Sessions
    -- that predate the handshake being recorded report nothing, which is why
    -- a slice here carries no client at all: the dashboard folds those into
    -- "unattributed", and a demo where every call is attributed would hide
    -- that bucket entirely.
    if(client_seen,
       concat(',"gram.mcp.client.name":"', client_name, '"',
              ',"gram.mcp.client.version":"', client_version, '"'), ''),
    '}'
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
    lower(hex(MD5(concat('gram-demo-managed-agent-', toString(1 + i % 3))))) AS agent_h,
    concat(substring(agent_h, 1, 8), '-', substring(agent_h, 9, 4), '-5', substring(agent_h, 14, 3), '-8',
           substring(agent_h, 18, 3), '-', substring(agent_h, 21, 12)) AS managed_agent_id,
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
    -- The MCP client follows the harness the chat ran in: a Claude Code chat
    -- calls from Claude Code or the Claude web app, a Cursor chat from Cursor
    -- or the VS Code extension. A client drawn independently of the harness
    -- would make "top clients" and "top agents" disagree for no reason.
    if(hook = 'claude-code',
       arrayElement([1, 1, 1, 1, 1, 1, 1, 2, 1, 1, 2, 1, 1, 2, 1, 1],
                    1 + reinterpretAsUInt8(unhex(substring(h, 15, 2))) % 16),
       arrayElement([3, 3, 3, 3, 3, 4, 3, 3, 4, 3, 3, 3, 4, 3, 3, 4],
                    1 + reinterpretAsUInt8(unhex(substring(h, 15, 2))) % 16)) AS cidx,
    arrayElement(['Claude Code', 'claude-ai', 'Cursor', 'Visual Studio Code'], cidx) AS client_name,
    -- A real fleet is spread over a few releases, weighted toward the newest.
    arrayElement(multiIf(
      cidx = 1, ['2.4.1', '2.4.1', '2.4.1', '2.3.8', '2.2.0'],
      cidx = 2, ['1.0.0', '1.0.0', '1.0.0', '1.0.0', '1.0.0'],
      cidx = 3, ['1.7.42', '1.7.42', '1.7.39', '1.6.14', '1.6.14'],
      ['1.104.2', '1.104.2', '1.103.1', '1.103.1', '1.102.0']),
      1 + reinterpretAsUInt8(unhex(substring(h, 17, 2))) % 5) AS client_version,
    cityHash64('cliseen', number) % 8 > 0 AS client_seen,
    -- Each client reaches for a different part of the catalog, so "most used
    -- tools by client" is a real breakdown rather than the same eight tools
    -- in the same proportions under every client.
    arrayElement(
      ['search_logs', 'get_metrics', 'query_db', 'get_customer',
       'list_deploys', 'process_refund', 'fetch_traces', 'check_health'],
      arrayElement(multiIf(
        cidx = 1, [1, 1, 2, 3, 3, 4, 5, 6, 7, 8, 1, 2, 3, 4, 7, 8],
        cidx = 2, [4, 4, 4, 6, 6, 6, 2, 2, 4, 6, 4, 6, 4, 6, 2, 4],
        cidx = 3, [1, 1, 1, 2, 2, 3, 3, 3, 7, 7, 8, 1, 2, 3, 7, 8],
        [2, 2, 4, 4, 5, 5, 5, 8, 8, 2, 4, 5, 8, 2, 5, 8]),
        1 + toUInt32(cityHash64('toolslot', number, k) % 16))) AS tool_name,
    arrayElement([0, 0, 1, 1, 1, 2, 3, 3, 3, 3, 4, 5, 5, 7, 8, 11],
                 1 + reinterpretAsUInt8(unhex(substring(h, 1, 2))) % 16) AS day_off,
    -- One predicate for the status code and the result payload: a call that
    -- returns 500 must not also carry a success body.
    (tool_name = 'process_refund' AND day_off <= 2 AND cityHash64('err', i, k) % 2 = 0)
      OR cityHash64('errbg', i, k) % 30 = 0 AS failed,
    -- Short per-call ids so two calls to the same tool do not read as one
    -- request replayed.
    substring(lower(hex(MD5(concat('gram-demo-toolio-', toString(i), '-', toString(k))))), 1, 8) AS io_id,
    toUInt32(cityHash64('iomag', i, k) % 900) AS io_mag,
    -- Arguments and result are written per tool. Their JSON quotes are escaped
    -- because these land INSIDE the attributes JSON document, as string values.
    multiIf(
      tool_name = 'search_logs',
        '{\\"query\\":\\"level:error service:payments-api\\",\\"limit\\":100}',
      tool_name = 'get_metrics',
        '{\\"metric\\":\\"http.server.request.duration\\",\\"service\\":\\"payments-api\\",\\"window\\":\\"15m\\"}',
      tool_name = 'query_db',
        '{\\"sql\\":\\"SELECT id, status FROM refunds WHERE created_at > now() - interval 1 day LIMIT 50\\"}',
      tool_name = 'get_customer',
        concat('{\\"customer_id\\":\\"cus_', io_id, '\\"}'),
      tool_name = 'list_deploys',
        '{\\"service\\":\\"payments-api\\",\\"limit\\":10}',
      tool_name = 'process_refund',
        concat('{\\"order_id\\":\\"ord_', io_id, '\\",\\"amount_cents\\":', toString(1200 + io_mag * 7), ',\\"reason\\":\\"duplicate_charge\\"}'),
      tool_name = 'fetch_traces',
        concat('{\\"trace_id\\":\\"', io_id, io_id, '\\",\\"service\\":\\"payments-api\\"}'),
      '{\\"service\\":\\"payments-api\\"}') AS tool_args,
    multiIf(
      failed AND tool_name = 'process_refund',
        concat('{\\"error\\":\\"refund declined by the payment processor\\",\\"code\\":\\"processor_declined\\",\\"order_id\\":\\"ord_', io_id, '\\"}'),
      failed,
        concat('{\\"error\\":\\"upstream returned 500 after ', toString(1 + io_mag % 3), ' retries\\",\\"code\\":\\"upstream_error\\"}'),
      tool_name = 'search_logs',
        concat('{\\"matches\\":', toString(io_mag), ',\\"truncated\\":', if(io_mag > 500, 'true', 'false'), '}'),
      tool_name = 'get_metrics',
        concat('{\\"p50_ms\\":', toString(20 + io_mag % 80), ',\\"p95_ms\\":', toString(200 + io_mag), ',\\"error_rate\\":0.0', toString(io_mag % 9), '}'),
      tool_name = 'query_db',
        concat('{\\"rows\\":', toString(io_mag % 50), ',\\"elapsed_ms\\":', toString(8 + io_mag % 120), '}'),
      tool_name = 'get_customer',
        concat('{\\"customer_id\\":\\"cus_', io_id, '\\",\\"plan\\":\\"enterprise\\",\\"status\\":\\"active\\"}'),
      tool_name = 'list_deploys',
        concat('{\\"deploys\\":', toString(1 + io_mag % 9), ',\\"latest\\":\\"v2026.9.', toString(io_mag % 40), '\\"}'),
      tool_name = 'process_refund',
        concat('{\\"refund_id\\":\\"ref_', io_id, '\\",\\"status\\":\\"succeeded\\",\\"amount_cents\\":', toString(1200 + io_mag * 7), '}'),
      tool_name = 'fetch_traces',
        concat('{\\"spans\\":', toString(3 + io_mag % 40), ',\\"root_service\\":\\"payments-api\\"}'),
      concat('{\\"status\\":\\"healthy\\",\\"checks_passed\\":', toString(4 + io_mag % 5), '}')) AS tool_result,
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
    -- The contractor works entirely on his own subscription; the manager
    -- splits, roughly a third of his chats going through the personal Claude
    -- login user_accounts already lists for him. A person who is wholly one or
    -- wholly the other never exercises the account filter on the identity
    -- Usage tab, which exists precisely for the split case.
    ',"gram.account_type":"', if(email = 'mateo@demo.getgram.ai'
      OR (email = 'lucas@demo.getgram.ai' AND cityHash64('acct', i) % 3 = 0),
      'personal', 'team'), '"',
    ',"gram.billing_mode":"', if(email = 'mateo@demo.getgram.ai'
      OR (email = 'lucas@demo.getgram.ai' AND cityHash64('acct', i) % 3 = 0),
      'flat_rate', 'metered'), '"}'
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
    -- Odd chats ran in Claude Code, so the client is the Claude pair.
    arrayElement([1, 1, 1, 1, 1, 1, 1, 2, 1, 1, 2, 1, 1, 2, 1, 1],
                 1 + reinterpretAsUInt8(unhex(substring(h, 15, 2))) % 16) AS cidx,
    -- Same client-aware draw as the hosted tool call this row is correlated
    -- with by call_demo_<i>_<k>. Drawing independently would give one call two
    -- different tool names depending on which row you read it from.
    arrayElement(
      ['search_logs', 'get_metrics', 'query_db', 'get_customer',
       'list_deploys', 'process_refund', 'fetch_traces', 'check_health'],
      arrayElement(multiIf(
        cidx = 1, [1, 1, 2, 3, 3, 4, 5, 6, 7, 8, 1, 2, 3, 4, 7, 8],
        cidx = 2, [4, 4, 4, 6, 6, 6, 2, 2, 4, 6, 4, 6, 4, 6, 2, 4],
        cidx = 3, [1, 1, 1, 2, 2, 3, 3, 3, 7, 7, 8, 1, 2, 3, 7, 8],
        [2, 2, 4, 4, 5, 5, 5, 8, 8, 2, 4, 5, 8, 2, 5, 8]),
        1 + toUInt32(cityHash64('toolslot', number, k) % 16))) AS tool_name,
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
    -- Skills fail like anything else that shells out. Without this the skills
    -- half of the erroring-targets list is permanently empty.
    if(cityHash64('sklerr', i, j) % 12 = 0,
       ',"gram.hook.error":"skill step exited non-zero"',
       ',"gen_ai.tool.call.result":"ok"'),
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

-- Calls the Gram hook denied before they ran (PreToolUse). These are the only
-- rows that carry gram.hook.block_reason, so without them the blocked status
-- filter on Tool Logs and the blocked counters on Insights are dead controls in
-- the demo org. Denials cluster on the destructive tools, and on the two users
-- whose roles do not carry production access.
INSERT INTO telemetry_logs
  (time_unix_nano, observed_time_unix_nano, severity_text, body, trace_id,
   attributes, resource_attributes, gram_project_id, gram_urn, service_name, gram_chat_id)
SELECT
  nano,
  nano,
  'WARN',
  concat('Hook: blocked ', tool_name),
  lower(hex(MD5(concat('gram-demo-blocktrace-', toString(i))))),
  concat(
    '{"gram.event.source":"hook"',
    ',"gram.hook.source":"', hook, '"',
    ',"gram.hook.event":"PreToolUse"',
    ',"gram.tool.name":"', tool_name, '"',
    ',"gram.tool_call.source":"acme-internal-mcp"',
    ',"gram.hook.block_reason":"', block_reason, '"',
    ',"gen_ai.conversation.id":"', chat_id, '"',
    ',"gram.project.id":"', toString(proj), '"',
    ',"user.email":"', email, '"',
    ',"gram.hook.hostname":"', hostname, '"}'
  ),
  '{"gram.deployment.id":"demo-seed"}',
  proj,
  concat('hooks:', tool_name),
  'gram-hooks',
  chat_id
FROM (
  SELECT
    number + 1 AS i,
    lower(hex(MD5(concat('gram-demo-chat-', toString(number + 1))))) AS h,
    concat(substring(h, 1, 8), '-', substring(h, 9, 4), '-5', substring(h, 14, 3), '-8',
           substring(h, 18, 3), '-', substring(h, 21, 12)) AS chat_id,
    toUUID('dec0de00-0000-4000-a000-000000000001') AS proj,
    -- The chat's own owner, by the same formula every other block uses. Drawing
    -- the person independently would file the block under a conversation
    -- somebody else was having.
    arrayElement([3, 3, 3, 3, 3, 1, 1, 1, 1, 4, 4, 4, 2, 2, 5, 6],
                 1 + reinterpretAsUInt8(unhex(substring(h, 13, 2))) % 16) AS uidx,
    arrayElement(['amara@demo.getgram.ai', 'jonas@demo.getgram.ai', 'priya@demo.getgram.ai',
                  'mateo@demo.getgram.ai', 'hana@demo.getgram.ai', 'lucas@demo.getgram.ai'], uidx) AS email,
    arrayElement(['amara-mbp.local', 'jonas-mbp.local', 'priya-mbp.local',
                  'mateo-mbp.local', 'hana-mbp.local', 'lucas-mbp.local'], uidx) AS hostname,
    if((number + 1) % 2 = 1, 'claude-code', 'cursor') AS hook,
    arrayElement(['process_refund', 'process_refund', 'query_db', 'restart_service', 'run_payroll'],
                 1 + toUInt32(cityHash64('blkt', number) % 5)) AS tool_name,
    multiIf(
      tool_name = 'process_refund', 'refunds above the approval threshold require a human',
      tool_name = 'query_db', 'query touches a table holding customer PII',
      tool_name = 'restart_service', 'production restart outside the change window',
      'payroll tools are not callable by agents') AS block_reason,
    arrayElement([0, 0, 1, 1, 2, 2, 3, 4, 5, 7, 8, 11],
                 1 + toUInt32(cityHash64('blkd', number) % 12)) AS day_off,
    arrayElement([9, 10, 11, 11, 13, 14, 15, 16, 16, 17],
                 1 + toUInt32(cityHash64('blkh', number) % 10)) AS hour_off,
    toDateTime64(toStartOfDay(now()), 9)
      - toIntervalDay(day_off) + toIntervalHour(hour_off)
      + toIntervalMinute(cityHash64('blkm', number) % 60) AS ts0,
    toUnixTimestamp64Nano(if(ts0 > now64(9) - toIntervalMinute(30), ts0 - toIntervalDay(1), ts0)) AS nano
  FROM numbers(150)
  -- Billing and part-time roles, i.e. the people a policy is written for:
  -- mateo, hana, lucas. Selecting the chats they own keeps the denial and the
  -- conversation it happened in agreeing about who was there. Roughly a third
  -- of chats qualify, so 150 candidates yield ~46 denials.
  WHERE uidx IN (4, 5, 6)
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
    -- Even chats ran in Cursor, so the client is the Cursor pair.
    arrayElement([3, 3, 3, 3, 3, 4, 3, 3, 4, 3, 3, 3, 4, 3, 3, 4],
                 1 + reinterpretAsUInt8(unhex(substring(h, 15, 2))) % 16) AS cidx,
    -- Same client-aware draw as the hosted tool call this row is correlated
    -- with by call_demo_<i>_<k>. Drawing independently would give one call two
    -- different tool names depending on which row you read it from.
    arrayElement(
      ['search_logs', 'get_metrics', 'query_db', 'get_customer',
       'list_deploys', 'process_refund', 'fetch_traces', 'check_health'],
      arrayElement(multiIf(
        cidx = 1, [1, 1, 2, 3, 3, 4, 5, 6, 7, 8, 1, 2, 3, 4, 7, 8],
        cidx = 2, [4, 4, 4, 6, 6, 6, 2, 2, 4, 6, 4, 6, 4, 6, 2, 4],
        cidx = 3, [1, 1, 1, 2, 2, 3, 3, 3, 7, 7, 8, 1, 2, 3, 7, 8],
        [2, 2, 4, 4, 5, 5, 5, 8, 8, 2, 4, 5, 8, 2, 5, 8]),
        1 + toUInt32(cityHash64('toolslot', number, k) % 16))) AS tool_name,
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
    -- Same split as the chat rows above, keyed the same way so a chat's
    -- score lands on the account that ran it. Odd rows only: those are the
    -- Anthropic ones, and both seeded personal accounts are Anthropic.
    ',"gram.account_type":"', if(i % 2 = 1
      AND (email = 'mateo@demo.getgram.ai'
        OR (email = 'lucas@demo.getgram.ai' AND cityHash64('acct', i) % 3 = 0)),
      'personal', 'team'), '"',
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
    -- Unsanctioned servers are where calls actually fail: expired personal
    -- tokens, rate limits, servers nobody owns. A shadow inventory that is
    -- 100% healthy hides the reason anyone looks at this page.
    if(failed,
       concat(',"gram.hook.error":"', arrayElement(
         ['upstream returned 401', 'upstream returned 429', 'connection reset by peer',
          'upstream timed out after 30s'], 1 + toUInt32(cityHash64('sherr', number) % 4)), '"'),
       ',"gen_ai.tool.call.result":"ok"'),
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
    -- Shadow usage is long-tailed: two or three servers carry most of the
    -- traffic and the rest are occasional. A uniform draw across all fifteen
    -- makes every ranking flat and every server look equally worth chasing.
    arrayElement([1, 1, 1, 1, 2, 2, 2, 3, 3, 4, 5, 5, 6, 7, 8, 9,
                  10, 11, 1, 2, 3, 12, 13, 2, 1, 14, 15, 1, 2, 3, 1, 5],
                 1 + toUInt32(cityHash64('sht', number) % 32)) AS sidx,
    -- The servers touching money and production access fail more than the
    -- read-only ones, which is what makes an erroring-servers list useful.
    (sidx IN (8, 12, 15) AND cityHash64('shf', number) % 4 = 0)
      OR cityHash64('shfbg', number) % 14 = 0 AS failed,
    toUUID('dec0de00-0000-4000-a000-000000000001') AS proj,
    -- Spread over the same trailing fortnight as the chat traffic and weighted
    -- to working hours, rather than a fixed nine-hour drumbeat marching back
    -- two months past every other row in the org.
    lower(hex(MD5(concat('gram-demo-shadowslot-', toString(number))))) AS sh,
    arrayElement([0, 0, 1, 1, 1, 2, 3, 3, 3, 3, 4, 5, 5, 7, 8, 11],
                 1 + reinterpretAsUInt8(unhex(substring(sh, 1, 2))) % 16) AS day_off,
    arrayElement([8, 9, 9, 10, 10, 11, 11, 13, 14, 14, 15, 16, 16, 17, 18, 20],
                 1 + reinterpretAsUInt8(unhex(substring(sh, 3, 2))) % 16) AS hour_off,
    toDateTime64(toStartOfDay(now()), 9)
      - toIntervalDay(day_off) + toIntervalHour(hour_off)
      + toIntervalMinute(reinterpretAsUInt8(unhex(substring(sh, 5, 2))) % 60) AS ts0,
    toUnixTimestamp64Nano(if(ts0 > now64(9) - toIntervalMinute(30), ts0 - toIntervalDay(1), ts0)) AS nano
  FROM numbers(180)
);

-- Shadow AI detections (employee enrollment detail): device-agent AI scan
-- results for the six demo directory users. Target
-- ids and categories come from the aitargets catalog
-- (server/internal/agent/aitargets); one row per (target, device, user,
-- signal) matching the ReplacingMergeTree key. Priya carries two devices so
-- the users/devices counts differ. Versions stamp installed rows only —
-- running detections usually cannot extract one.
-- Cline and Continue are the editor-extension case: both ship a CLI and both
-- carry a wildcard config dir for their version-stamped VS Code extension
-- directory, so they are also the rows that exercise the glob path end to
-- end. Zed and Goose are the two new targets that publish CIMD, so they are
-- the ones carrying a real decision below.
--
-- The two assistants are deliberately a pair: both speak MCP to Gram like a
-- harness does without being coding tools, but Hermes publishes a CIMD
-- document and OpenClaw does not. So a decision on Hermes is enforceable and
-- shows as one, while OpenClaw reads unreviewed however hard somebody wants
-- to block it — the same contrast the Harnesses tab draws between Codex and
-- Cursor.
-- Keep every comment above this statement: ClickHouse parses the VALUES list
-- as data and cannot skip a comment between rows.
INSERT INTO ai_detections
  (organization_id, target_id, device_serial, user_email, signal, category,
   version, first_seen, last_seen, updated_at)
VALUES
  ('org_gram_demo_workspace', 'claude-code', 'DEMO-MBP-AMARA', 'amara@demo.getgram.ai', 'installed',
   'harness', '2.0.44', now64(9) - INTERVAL 8 DAY, now64(9) - INTERVAL 2 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'claude-code', 'DEMO-MBP-AMARA', 'amara@demo.getgram.ai', 'running',
   'harness', '', now64(9) - INTERVAL 8 DAY, now64(9) - INTERVAL 2 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'claude-code', 'DEMO-MBP-JONAS', 'jonas@demo.getgram.ai', 'installed',
   'harness', '2.0.41', now64(9) - INTERVAL 7 DAY, now64(9) - INTERVAL 8 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'claude-code', 'DEMO-MBP-JONAS', 'jonas@demo.getgram.ai', 'running',
   'harness', '', now64(9) - INTERVAL 7 DAY, now64(9) - INTERVAL 8 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'claude-code', 'DEMO-MBP-PRIYA', 'priya@demo.getgram.ai', 'installed',
   'harness', '2.0.44', now64(9) - INTERVAL 8 DAY, now64(9) - INTERVAL 3 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'claude-code', 'DEMO-MBP-PRIYA', 'priya@demo.getgram.ai', 'running',
   'harness', '', now64(9) - INTERVAL 8 DAY, now64(9) - INTERVAL 3 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'claude-code', 'DEMO-MSTUDIO-PRIYA', 'priya@demo.getgram.ai', 'installed',
   'harness', '2.0.38', now64(9) - INTERVAL 5 DAY, now64(9) - INTERVAL 30 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'claude-code', 'DEMO-MBP-MATEO', 'mateo@demo.getgram.ai', 'installed',
   'harness', '2.0.44', now64(9) - INTERVAL 7 DAY, now64(9) - INTERVAL 22 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'claude-code', 'DEMO-MBP-MATEO', 'mateo@demo.getgram.ai', 'running',
   'harness', '', now64(9) - INTERVAL 7 DAY, now64(9) - INTERVAL 26 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'claude-code', 'DEMO-MBP-HANA', 'hana@demo.getgram.ai', 'installed',
   'harness', '2.0.41', now64(9) - INTERVAL 6 DAY, now64(9) - INTERVAL 18 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'claude-code', 'DEMO-MBP-HANA', 'hana@demo.getgram.ai', 'running',
   'harness', '', now64(9) - INTERVAL 6 DAY, now64(9) - INTERVAL 18 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'claude-code', 'DEMO-MBP-LUCAS', 'lucas@demo.getgram.ai', 'installed',
   'harness', '2.0.44', now64(9) - INTERVAL 5 DAY, now64(9) - INTERVAL 12 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'cursor', 'DEMO-MBP-AMARA', 'amara@demo.getgram.ai', 'installed',
   'harness', '1.7.52', now64(9) - INTERVAL 7 DAY, now64(9) - INTERVAL 5 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'cursor', 'DEMO-MBP-AMARA', 'amara@demo.getgram.ai', 'running',
   'harness', '', now64(9) - INTERVAL 7 DAY, now64(9) - INTERVAL 5 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'cursor', 'DEMO-MBP-JONAS', 'jonas@demo.getgram.ai', 'installed',
   'harness', '1.7.49', now64(9) - INTERVAL 6 DAY, now64(9) - INTERVAL 28 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'cursor', 'DEMO-MBP-PRIYA', 'priya@demo.getgram.ai', 'installed',
   'harness', '1.7.52', now64(9) - INTERVAL 7 DAY, now64(9) - INTERVAL 7 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'cursor', 'DEMO-MBP-PRIYA', 'priya@demo.getgram.ai', 'running',
   'harness', '', now64(9) - INTERVAL 7 DAY, now64(9) - INTERVAL 7 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'codex', 'DEMO-MBP-PRIYA', 'priya@demo.getgram.ai', 'installed',
   'harness', '0.52.0', now64(9) - INTERVAL 6 DAY, now64(9) - INTERVAL 26 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'codex', 'DEMO-MBP-MATEO', 'mateo@demo.getgram.ai', 'installed',
   'harness', '0.50.1', now64(9) - INTERVAL 5 DAY, now64(9) - INTERVAL 50 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'ollama', 'DEMO-MBP-PRIYA', 'priya@demo.getgram.ai', 'installed',
   'local_model', '0.6.2', now64(9) - INTERVAL 6 DAY, now64(9) - INTERVAL 4 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'ollama', 'DEMO-MBP-PRIYA', 'priya@demo.getgram.ai', 'running',
   'local_model', '', now64(9) - INTERVAL 6 DAY, now64(9) - INTERVAL 4 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'ollama', 'DEMO-MSTUDIO-PRIYA', 'priya@demo.getgram.ai', 'installed',
   'local_model', '0.6.0', now64(9) - INTERVAL 5 DAY, now64(9) - INTERVAL 72 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'ollama', 'DEMO-MBP-MATEO', 'mateo@demo.getgram.ai', 'installed',
   'local_model', '0.6.2', now64(9) - INTERVAL 5 DAY, now64(9) - INTERVAL 9 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'ollama', 'DEMO-MBP-MATEO', 'mateo@demo.getgram.ai', 'running',
   'local_model', '', now64(9) - INTERVAL 5 DAY, now64(9) - INTERVAL 9 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'lmstudio', 'DEMO-MBP-MATEO', 'mateo@demo.getgram.ai', 'installed',
   'local_model', '0.3.9', now64(9) - INTERVAL 4 DAY, now64(9) - INTERVAL 72 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'aider', 'DEMO-MBP-JONAS', 'jonas@demo.getgram.ai', 'installed',
   'harness', '0.86.1', now64(9) - INTERVAL 7 DAY, now64(9) - INTERVAL 120 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'openclaw', 'DEMO-MBP-HANA', 'hana@demo.getgram.ai', 'installed',
   'assistant', '0.4.1', now64(9) - INTERVAL 5 DAY, now64(9) - INTERVAL 11 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'openclaw', 'DEMO-MBP-HANA', 'hana@demo.getgram.ai', 'running',
   'assistant', '', now64(9) - INTERVAL 5 DAY, now64(9) - INTERVAL 11 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'openclaw', 'DEMO-MBP-LUCAS', 'lucas@demo.getgram.ai', 'installed',
   'assistant', '0.4.0', now64(9) - INTERVAL 4 DAY, now64(9) - INTERVAL 40 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'openclaw', 'DEMO-MBP-AMARA', 'amara@demo.getgram.ai', 'installed',
   'assistant', '0.4.1', now64(9) - INTERVAL 6 DAY, now64(9) - INTERVAL 6 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'openclaw', 'DEMO-MBP-AMARA', 'amara@demo.getgram.ai', 'running',
   'assistant', '', now64(9) - INTERVAL 6 DAY, now64(9) - INTERVAL 6 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'openclaw', 'DEMO-MSTUDIO-PRIYA', 'priya@demo.getgram.ai', 'installed',
   'assistant', '0.3.8', now64(9) - INTERVAL 4 DAY, now64(9) - INTERVAL 64 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'hermes-agent', 'DEMO-MBP-JONAS', 'jonas@demo.getgram.ai', 'installed',
   'assistant', '1.2.0', now64(9) - INTERVAL 5 DAY, now64(9) - INTERVAL 14 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'hermes-agent', 'DEMO-MBP-JONAS', 'jonas@demo.getgram.ai', 'running',
   'assistant', '', now64(9) - INTERVAL 5 DAY, now64(9) - INTERVAL 14 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'hermes-agent', 'DEMO-MBP-MATEO', 'mateo@demo.getgram.ai', 'installed',
   'assistant', '1.1.4', now64(9) - INTERVAL 3 DAY, now64(9) - INTERVAL 30 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'cline', 'DEMO-MBP-AMARA', 'amara@demo.getgram.ai', 'installed',
   'harness', '4.1.17', now64(9) - INTERVAL 6 DAY, now64(9) - INTERVAL 3 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'cline', 'DEMO-MBP-AMARA', 'amara@demo.getgram.ai', 'running',
   'harness', '', now64(9) - INTERVAL 6 DAY, now64(9) - INTERVAL 3 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'cline', 'DEMO-MBP-JONAS', 'jonas@demo.getgram.ai', 'installed',
   'harness', '4.1.12', now64(9) - INTERVAL 5 DAY, now64(9) - INTERVAL 20 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'cline', 'DEMO-MBP-PRIYA', 'priya@demo.getgram.ai', 'installed',
   'harness', '4.1.17', now64(9) - INTERVAL 4 DAY, now64(9) - INTERVAL 9 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'continue', 'DEMO-MBP-MATEO', 'mateo@demo.getgram.ai', 'installed',
   'harness', '2.1.0', now64(9) - INTERVAL 4 DAY, now64(9) - INTERVAL 44 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'zed', 'DEMO-MBP-HANA', 'hana@demo.getgram.ai', 'installed',
   'harness', '0.221.4', now64(9) - INTERVAL 7 DAY, now64(9) - INTERVAL 5 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'zed', 'DEMO-MBP-HANA', 'hana@demo.getgram.ai', 'running',
   'harness', '', now64(9) - INTERVAL 7 DAY, now64(9) - INTERVAL 5 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'zed', 'DEMO-MBP-LUCAS', 'lucas@demo.getgram.ai', 'installed',
   'harness', '0.221.4', now64(9) - INTERVAL 5 DAY, now64(9) - INTERVAL 16 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'goose', 'DEMO-MBP-PRIYA', 'priya@demo.getgram.ai', 'installed',
   'harness', '1.34.2', now64(9) - INTERVAL 5 DAY, now64(9) - INTERVAL 34 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'goose', 'DEMO-MSTUDIO-PRIYA', 'priya@demo.getgram.ai', 'installed',
   'harness', '1.33.0', now64(9) - INTERVAL 4 DAY, now64(9) - INTERVAL 58 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'warp', 'DEMO-MBP-AMARA', 'amara@demo.getgram.ai', 'installed',
   'harness', '0.2026.08.19', now64(9) - INTERVAL 8 DAY, now64(9) - INTERVAL 7 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'warp', 'DEMO-MBP-AMARA', 'amara@demo.getgram.ai', 'running',
   'harness', '', now64(9) - INTERVAL 8 DAY, now64(9) - INTERVAL 7 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'warp', 'DEMO-MBP-HANA', 'hana@demo.getgram.ai', 'installed',
   'harness', '0.2026.08.19', now64(9) - INTERVAL 6 DAY, now64(9) - INTERVAL 21 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'msty', 'DEMO-MBP-JONAS', 'jonas@demo.getgram.ai', 'installed',
   'assistant', '1.9.2', now64(9) - INTERVAL 4 DAY, now64(9) - INTERVAL 26 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'msty', 'DEMO-MBP-LUCAS', 'lucas@demo.getgram.ai', 'installed',
   'assistant', '1.9.0', now64(9) - INTERVAL 3 DAY, now64(9) - INTERVAL 52 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'jan', 'DEMO-MBP-HANA', 'hana@demo.getgram.ai', 'installed',
   'local_model', '0.8.4', now64(9) - INTERVAL 5 DAY, now64(9) - INTERVAL 13 HOUR, now64(9)),
  ('org_gram_demo_workspace', 'jan', 'DEMO-MBP-HANA', 'hana@demo.getgram.ai', 'running',
   'local_model', '', now64(9) - INTERVAL 5 DAY, now64(9) - INTERVAL 13 HOUR, now64(9));

-- Scan receipts: one per device per day over the trailing 5 days, proving
-- every enrolled device scanned recently (the page's freshness story and the
-- provable-coverage contract for zero-match devices). match_count is the
-- number of matches the agent reported, and every reported match becomes one
-- ai_detections row, so each device's count here must equal that device's row
-- count in the detections insert above — add a detection, update this array.
INSERT INTO ai_scan_receipts
  (organization_id, device_serial, user_email, scan_started_at,
   scan_completed_at, target_list_version, match_count, received_at)
SELECT
  'org_gram_demo_workspace',
  arrayElement(['DEMO-MBP-AMARA', 'DEMO-MBP-JONAS', 'DEMO-MBP-PRIYA',
                'DEMO-MSTUDIO-PRIYA', 'DEMO-MBP-MATEO', 'DEMO-MBP-HANA',
                'DEMO-MBP-LUCAS'], didx),
  arrayElement(['amara@demo.getgram.ai', 'jonas@demo.getgram.ai', 'priya@demo.getgram.ai',
                'priya@demo.getgram.ai', 'mateo@demo.getgram.ai', 'hana@demo.getgram.ai',
                'lucas@demo.getgram.ai'], didx),
  ts - toIntervalSecond(6),
  ts - toIntervalSecond(1),
  3,
  arrayElement([10, 8, 9, 4, 8, 9, 4], didx),
  ts
FROM (
  SELECT
    1 + toUInt32(number % 7) AS didx,
    toUInt32(intDiv(number, 7)) AS day_off,
    now64(9) - toIntervalDay(day_off)
      - toIntervalHour(2 + cityHash64('aiscan', number) % 4)
      - toIntervalMinute(cityHash64('aiscanm', number) % 55) AS ts
  FROM numbers(35)
);

-- Authz challenges (org home "Recent challenges" panel, /access/challenges, and
-- the per-identity Access and Security tabs).
--
-- user_id values are demo members (organization_user_relationships rows exist,
-- so the member-suppression filter keeps them visible); one api_key bucket
-- stays visible regardless of membership.
--
-- The subject and the scope are drawn from DIFFERENT hashes on purpose. Keying
-- both off `number % 3` locked each person to a single scope forever, so every
-- identity's Access tab listed one action repeated N times and the whole panel
-- read as a stuck query. Outcomes are mixed for the same reason: a log that is
-- 100% denials says nothing about which denials matter, and the donut on the
-- Access tab has only one segment to draw.
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
  if(number % 7 = 6, 'api_key:akey_demo0000000001',
     concat('user:', arrayElement(['user_demo_amara', 'user_demo_jonas', 'user_demo_priya',
                                   'user_demo_mateo', 'user_demo_hana', 'user_demo_lucas'],
                                  1 + toUInt32(cityHash64('chal-user', number) % 6)))),
  if(number % 7 = 6, 'api_key', 'user'),
  if(number % 7 = 6, NULL,
     arrayElement(['user_demo_amara', 'user_demo_jonas', 'user_demo_priya',
                   'user_demo_mateo', 'user_demo_hana', 'user_demo_lucas'],
                  1 + toUInt32(cityHash64('chal-user', number) % 6))),
  if(number % 7 = 6, NULL,
     arrayElement(['amara@demo.getgram.ai', 'jonas@demo.getgram.ai', 'priya@demo.getgram.ai',
                   'mateo@demo.getgram.ai', 'hana@demo.getgram.ai', 'lucas@demo.getgram.ai'],
                  1 + toUInt32(cityHash64('chal-user', number) % 6))),
  if(number % 7 = 6, 'akey_demo0000000001', NULL),
  [],
  'require',
  -- Roughly one in three is refused: enough denials to be worth reading,
  -- not so many that the org looks locked out of its own tools.
  if(cityHash64('chal-outcome', number) % 3 = 0, 'deny', 'allow'),
  -- reason is a closed enum on the wire: an allow that carries an empty
  -- reason fails response validation and the client discards the whole page,
  -- which reads in the UI as "no authorization checks recorded".
  if(cityHash64('chal-outcome', number) % 3 <> 0, 'grant_matched',
     if(number % 2 = 0, 'scope_unsatisfied', 'no_grants')),
  arrayElement(['toolset:admin', 'project:admin', 'environment:read',
                'environment:write', 'mcp:connect', 'skill:write', 'chat:read'],
               1 + toUInt32(cityHash64('chal-scope', number) % 7)),
  arrayElement(['toolset', 'project', 'environment',
                'environment', 'mcp', 'skill', 'chat'],
               1 + toUInt32(cityHash64('chal-scope', number) % 7)),
  arrayElement(['dec0de00-0000-4000-a000-000000005e01',
                'dec0de00-0000-4000-a000-000000000001',
                'dec0de00-0000-4000-a000-00000000ee01'], 1 + (number % 3)),
  '{"project":"dec0de00-0000-4000-a000-000000000001"}',
  [arrayElement(['toolset:admin', 'project:admin', 'environment:read',
                 'environment:write', 'mcp:connect', 'skill:write', 'chat:read'],
                1 + toUInt32(cityHash64('chal-scope', number) % 7))],
  [arrayElement(['toolset:admin', 'project:admin', 'environment:read',
                 'environment:write', 'mcp:connect', 'skill:write', 'chat:read'],
                1 + toUInt32(cityHash64('chal-scope', number) % 7))],
  [arrayElement(['toolset', 'project', 'environment',
                 'environment', 'mcp', 'skill', 'chat'],
                1 + toUInt32(cityHash64('chal-scope', number) % 7))],
  [arrayElement(['dec0de00-0000-4000-a000-000000005e01',
                 'dec0de00-0000-4000-a000-000000000001',
                 'dec0de00-0000-4000-a000-00000000ee01'], 1 + (number % 3))],
  ['{"project":"dec0de00-0000-4000-a000-000000000001"}'],
  [], [], [], [],
  toUInt32(3 + number % 5)
FROM numbers(64);

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
-- (its top-user display). Tool-output findings additionally model the mediated
-- response that produced that content, so the Risk Events MCP-server filter
-- has representative data without misattributing user-message findings.
INSERT INTO risk_findings
  (id, created_at, organization_id, project_id, chat_message_id, chat_id,
   user_id, external_user_id, user_email, team, chat_source,
   risk_policy_id, risk_policy_version, rule_id,
   description, source, confidence, category, tags, start_pos, end_pos,
   match_len, match_redacted, surface, field, message_created_at,
   excluded_at, exclusion_id, false_positive_at, excluded_reason,
   excluded_detail, execution_id, mcp_server_id, toolset_id, tool_name,
   phase, mediation_surface, mcp_method, principal_kind, identity_stamped,
   enforcement_outcome)
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
                'placeholder_value'], 1 + supp),
  if(k IN (2, 4, 5, 11), '',
     concat(substring(hexec, 1, 8), '-', substring(hexec, 9, 4), '-5',
            substring(hexec, 14, 3), '-8', substring(hexec, 18, 3), '-',
            substring(hexec, 21, 12))),
  if(k IN (2, 4, 5, 11), '',
     concat(substring(hmcp, 1, 8), '-', substring(hmcp, 9, 4), '-5',
            substring(hmcp, 14, 3), '-8', substring(hmcp, 18, 3), '-',
            substring(hmcp, 21, 12))),
  if(k IN (2, 4, 5, 11), '',
     if(i % 5 = 0, 'dec0de00-0000-4000-a000-000000005e02',
                     'dec0de00-0000-4000-a000-000000005e01')),
  if(k IN (2, 4, 5, 11), '',
     if(i % 5 = 0, 'check_health', 'get_customer')),
  if(k IN (2, 4, 5, 11), '', 'response'),
  if(k IN (2, 4, 5, 11), '', 'hosted_mcp'),
  if(k IN (2, 4, 5, 11), '', 'tools/call'),
  if(k IN (2, 4, 5, 11), '', 'user'),
  k NOT IN (2, 4, 5, 11),
  if(k IN (2, 4, 5, 11), '', 'logged')
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
    lower(hex(MD5(concat('gram-demo-mcp-execution-', toString(number + 1))))) AS hexec,
    -- Same chat-to-server rule as telemetry_logs (i % 5 = 0 is Acme Ops),
    -- so the Risk Events MCP filter agrees with the call details.
    lower(hex(MD5(if(i % 5 = 0, 'gram-demo-mcpserver-ops',
                                   'gram-demo-mcpserver-support')))) AS hmcp,
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

-- Gateway (meta MCP) traffic for the Acme Agent Gateway, which fronts the
-- four mcp_servers seeded in postgres.sql. Ids reproduce
-- demo.det_uuid('gram-demo-metamcp-1') / ('gram-demo-mcpserver-<member>') /
-- ('gram-demo-remotemcp-<member>') so ClickHouse rows join the Postgres rows.
--
-- Discovery calls (list_servers / describe_server / describe_tools) land under
-- gram.event.source = meta_discovery with a "metamcp:" urn: the runtime never
-- gives them a "tools:" urn, so they cannot count as tool calls (the query
-- layer's classifier), and the seed mirrors that. execute_tool writes no row
-- of its own: the member call below IS the row, stamped with the gateway id.
INSERT INTO telemetry_logs
  (time_unix_nano, observed_time_unix_nano, severity_text, body, trace_id,
   attributes, resource_attributes, gram_project_id, gram_urn, service_name)
SELECT
  nano,
  nano,
  'INFO',
  concat('Gateway discovery: ', tool_name),
  lower(hex(MD5(concat('gram-demo-gwdisc-', toString(i))))),
  concat(
    '{"gram.event.source":"meta_discovery"',
    ',"gram.meta_mcp_server.id":"', gateway, '"',
    ',"gram.tool.name":"', tool_name, '"',
    ',"gram.tool.urn":"metamcp:', gateway, ':', tool_name, '"',
    ',"http.response.status_code":200',
    ',"http.server.request.duration":', toString(round(0.02 + (cityHash64('gwd', i) % 40) / 100, 3)),
    ',"gram.project.id":"', toString(proj), '"',
    ',"user.email":"', email, '"',
    ',"gram.external_user.id":"', email, '"',
    -- A gateway handshakes once per session and every member dispatch in that
    -- session inherits the client, which is why these proxy hops carry one.
    if(client_seen,
       concat(',"gram.mcp.client.name":"', client_name, '"',
              ',"gram.mcp.client.version":"', client_version, '"'), ''),
    ',"gram.hook.source":"claude-code"}'
  ),
  '{"gram.deployment.id":"demo-seed"}',
  proj,
  concat('metamcp:', gateway, ':', tool_name),
  'gram-mcp-gateway'
FROM (
  SELECT
    number + 1 AS i,
    toUUID('dec0de00-0000-4000-a000-000000000001') AS proj,
    lower(hex(MD5('gram-demo-metamcp-1'))) AS gh,
    concat(substring(gh, 1, 8), '-', substring(gh, 9, 4), '-5', substring(gh, 14, 3), '-8',
           substring(gh, 18, 3), '-', substring(gh, 21, 12)) AS gateway,
    arrayElement(['amara@demo.getgram.ai', 'jonas@demo.getgram.ai', 'priya@demo.getgram.ai',
                  'mateo@demo.getgram.ai', 'hana@demo.getgram.ai', 'lucas@demo.getgram.ai'],
                 1 + (cityHash64('gwu', number) % 6)) AS email,
    -- Keyed on the person, not the row: one gateway session walks the catalog
    -- and then calls, so discovery and dispatch have to agree on who did it.
    -- An independent per-row draw would have the same session change client
    -- between listing a server and calling it.
    arrayElement([1, 1, 1, 1, 2, 2, 2, 3, 3, 5, 1, 2, 1, 3, 2, 4],
                 1 + (cityHash64('gwcli', email) % 16)) AS cidx,
    arrayElement(['Claude Code', 'Cursor', 'Visual Studio Code', 'mcp-inspector', 'claude-ai'], cidx) AS client_name,
    arrayElement(multiIf(
      cidx = 1, ['2.4.1', '2.4.1', '2.3.8'],
      cidx = 2, ['1.7.42', '1.7.42', '1.6.14'],
      cidx = 3, ['1.104.2', '1.103.1', '1.103.1'],
      cidx = 4, ['0.16.2', '0.16.2', '0.15.0'],
      ['1.0.0', '1.0.0', '1.0.0']),
      1 + (cityHash64('gwcliv', email) % 3)) AS client_version,
    cityHash64('gwcliseen', email) % 9 > 0 AS client_seen,
    -- Funnel shape: every walk lists, most describe a server, fewer pull schemas.
    multiIf(number % 10 < 5, 'list_servers', number % 10 < 8, 'describe_server', 'describe_tools') AS tool_name,
    toDateTime64(toStartOfDay(now()), 9)
      - toIntervalDay(number % 12) + toIntervalHour(8 + (cityHash64('gwh', number) % 11))
      + toIntervalMinute(cityHash64('gwm', number) % 60) AS ts0,
    toUnixTimestamp64Nano(if(ts0 > now64(9) - toIntervalMinute(30), ts0 - toIntervalDay(1), ts0)) AS nano
  FROM numbers(150)
);

-- Member calls dispatched through the gateway: ordinary tool_call rows for the
-- member (hosted members keep the "tools:http:acme:" urn + toolset slug the
-- direct path emits; remote members the "tools:externalmcp:<remote id>:" urn
-- the proxy emits), plus gram.meta_mcp_server.id. One row serves the member's
-- own usage, the gateway Activity section, and the MCP listing marker alike,
-- so call totals never double. Ops carries most of the failures.
INSERT INTO telemetry_logs
  (time_unix_nano, observed_time_unix_nano, severity_text, body, trace_id,
   attributes, resource_attributes, gram_project_id, gram_urn, service_name)
SELECT
  nano,
  nano,
  'INFO',
  concat('Tool call: ', tool_name),
  lower(hex(MD5(concat('gram-demo-gwcall-', toString(i))))),
  concat(
    '{"gram.tool.urn":"', tool_urn, '"',
    ',"gram.tool.name":"', tool_name, '"',
    if(toolset_slug != '', concat(',"gram.toolset.slug":"', toolset_slug, '"'), ''),
    ',"gram.event.source":"tool_call"',
    ',"gram.mcp_server.id":"', member, '"',
    if(toolset_slug = '', concat(',"gram.remote_mcp_server.id":"', remote, '"'), ''),
    -- The proxy stamps the urn's source segment as the tool call source; the
    -- tool-usage classifier reads it to attribute the call to the member.
    if(toolset_slug = '', concat(',"gram.tool_call.source":"', remote, '"'), ''),
    ',"gram.meta_mcp_server.id":"', gateway, '"',
    ',"http.response.status_code":', toString(status),
    ',"http.server.request.duration":', toString(round(0.05 + (cityHash64('gwc', i) % 300) / 100, 3)),
    ',"gram.project.id":"', toString(proj), '"',
    ',"user.email":"', email, '"',
    ',"gram.external_user.id":"', email, '"',
    -- A gateway handshakes once per session and every member dispatch in that
    -- session inherits the client, which is why these proxy hops carry one.
    if(client_seen,
       concat(',"gram.mcp.client.name":"', client_name, '"',
              ',"gram.mcp.client.version":"', client_version, '"'), ''),
    ',"gram.hook.source":"claude-code"}'
  ),
  '{"gram.deployment.id":"demo-seed"}',
  proj,
  tool_urn,
  'gram-mcp-gateway'
FROM (
  SELECT
    number + 1 AS i,
    toUUID('dec0de00-0000-4000-a000-000000000001') AS proj,
    lower(hex(MD5('gram-demo-metamcp-1'))) AS gh,
    concat(substring(gh, 1, 8), '-', substring(gh, 9, 4), '-5', substring(gh, 14, 3), '-8',
           substring(gh, 18, 3), '-', substring(gh, 21, 12)) AS gateway,
    -- Weighted member pick: support 5, ops 3, linear 1, slack 1 of every 10.
    arrayElement(['support', 'support', 'support', 'support', 'support', 'ops', 'ops', 'ops', 'linear', 'slack'],
                 1 + (cityHash64('gwmem', number) % 10)) AS mkey,
    lower(hex(MD5(concat('gram-demo-mcpserver-', mkey)))) AS mh,
    concat(substring(mh, 1, 8), '-', substring(mh, 9, 4), '-5', substring(mh, 14, 3), '-8',
           substring(mh, 18, 3), '-', substring(mh, 21, 12)) AS member,
    lower(hex(MD5(concat('gram-demo-remotemcp-', mkey)))) AS rh,
    concat(substring(rh, 1, 8), '-', substring(rh, 9, 4), '-5', substring(rh, 14, 3), '-8',
           substring(rh, 18, 3), '-', substring(rh, 21, 12)) AS remote,
    multiIf(mkey = 'support', 'acme-support-tools', mkey = 'ops', 'acme-ops', '') AS toolset_slug,
    multiIf(
      mkey = 'support', arrayElement(['search_logs', 'get_customer', 'query_db', 'fetch_traces'], 1 + (cityHash64('gwt', number) % 4)),
      mkey = 'ops', arrayElement(['list_deploys', 'check_health', 'get_metrics', 'process_refund'], 1 + (cityHash64('gwt', number) % 4)),
      mkey = 'linear', arrayElement(['list_issues', 'create_issue', 'get_issue'], 1 + (cityHash64('gwt', number) % 3)),
      arrayElement(['send_message', 'list_channels'], 1 + (cityHash64('gwt', number) % 2))) AS tool_name,
    if(toolset_slug != '', concat('tools:http:acme:', tool_name), concat('tools:externalmcp:', remote, ':', tool_name)) AS tool_urn,
    if((mkey = 'ops' AND cityHash64('gwerr', number) % 5 = 0) OR cityHash64('gwbg', number) % 40 = 0, 500, 200) AS status,
    arrayElement(['amara@demo.getgram.ai', 'jonas@demo.getgram.ai', 'priya@demo.getgram.ai',
                  'mateo@demo.getgram.ai', 'hana@demo.getgram.ai', 'lucas@demo.getgram.ai'],
                 1 + (cityHash64('gwu2', number) % 6)) AS email,
    -- Gateway endpoints see more kinds of client than a hosted server does:
    -- agent harnesses plus the MCP Inspector someone left open. Keyed on the
    -- person so a caller presents the same client here as in discovery.
    arrayElement([1, 1, 1, 1, 2, 2, 2, 3, 3, 5, 1, 2, 1, 3, 2, 4],
                 1 + (cityHash64('gwcli', email) % 16)) AS cidx,
    arrayElement(['Claude Code', 'Cursor', 'Visual Studio Code', 'mcp-inspector', 'claude-ai'], cidx) AS client_name,
    arrayElement(multiIf(
      cidx = 1, ['2.4.1', '2.4.1', '2.3.8'],
      cidx = 2, ['1.7.42', '1.7.42', '1.6.14'],
      cidx = 3, ['1.104.2', '1.103.1', '1.103.1'],
      cidx = 4, ['0.16.2', '0.16.2', '0.15.0'],
      ['1.0.0', '1.0.0', '1.0.0']),
      1 + (cityHash64('gwcliv', email) % 3)) AS client_version,
    cityHash64('gwcliseen', email) % 9 > 0 AS client_seen,
    toDateTime64(toStartOfDay(now()), 9)
      - toIntervalDay(number % 12) + toIntervalHour(8 + (cityHash64('gwh2', number) % 11))
      + toIntervalMinute(cityHash64('gwm2', number) % 60) AS ts0,
    toUnixTimestamp64Nano(if(ts0 > now64(9) - toIntervalMinute(30), ts0 - toIntervalDay(1), ts0)) AS nano
  FROM numbers(240)
);

-- Dispatches to GitHub from before it left the gateway (its membership is
-- soft-deleted in Postgres three days ago). They count toward the gateway's
-- dispatched-call totals but must not appear in Calls by member.
INSERT INTO telemetry_logs
  (time_unix_nano, observed_time_unix_nano, severity_text, body, trace_id,
   attributes, resource_attributes, gram_project_id, gram_urn, service_name)
SELECT
  nano,
  nano,
  'INFO',
  concat('Tool call: ', tool_name),
  lower(hex(MD5(concat('gram-demo-gwgone-', toString(i))))),
  concat(
    '{"gram.tool.urn":"', tool_urn, '"',
    ',"gram.tool.name":"', tool_name, '"',
    ',"gram.event.source":"tool_call"',
    ',"gram.mcp_server.id":"', member, '"',
    ',"gram.remote_mcp_server.id":"', remote, '"',
    ',"gram.tool_call.source":"', remote, '"',
    ',"gram.meta_mcp_server.id":"', gateway, '"',
    ',"http.response.status_code":', toString(status),
    ',"http.server.request.duration":', toString(round(0.08 + (cityHash64('gwgc', i) % 200) / 100, 3)),
    ',"gram.project.id":"', toString(proj), '"',
    ',"user.email":"', email, '"',
    ',"gram.external_user.id":"', email, '"',
    -- A gateway handshakes once per session and every member dispatch in that
    -- session inherits the client, which is why these proxy hops carry one.
    if(client_seen,
       concat(',"gram.mcp.client.name":"', client_name, '"',
              ',"gram.mcp.client.version":"', client_version, '"'), ''),
    ',"gram.hook.source":"claude-code"}'
  ),
  '{"gram.deployment.id":"demo-seed"}',
  proj,
  tool_urn,
  'gram-mcp-gateway'
FROM (
  SELECT
    number + 1 AS i,
    toUUID('dec0de00-0000-4000-a000-000000000001') AS proj,
    lower(hex(MD5('gram-demo-metamcp-1'))) AS gh,
    concat(substring(gh, 1, 8), '-', substring(gh, 9, 4), '-5', substring(gh, 14, 3), '-8',
           substring(gh, 18, 3), '-', substring(gh, 21, 12)) AS gateway,
    lower(hex(MD5('gram-demo-mcpserver-github'))) AS mh,
    concat(substring(mh, 1, 8), '-', substring(mh, 9, 4), '-5', substring(mh, 14, 3), '-8',
           substring(mh, 18, 3), '-', substring(mh, 21, 12)) AS member,
    lower(hex(MD5('gram-demo-remotemcp-github'))) AS rh,
    concat(substring(rh, 1, 8), '-', substring(rh, 9, 4), '-5', substring(rh, 14, 3), '-8',
           substring(rh, 18, 3), '-', substring(rh, 21, 12)) AS remote,
    arrayElement(['list_pull_requests', 'get_issue', 'search_code'], 1 + (cityHash64('gwgt', number) % 3)) AS tool_name,
    concat('tools:externalmcp:', remote, ':', tool_name) AS tool_urn,
    if(cityHash64('gwge', number) % 8 = 0, 502, 200) AS status,
    arrayElement([1, 1, 1, 1, 2, 2, 2, 3, 3, 5, 1, 2, 1, 3, 2, 4],
                 1 + (cityHash64('gwgcli', number) % 16)) AS cidx,
    arrayElement(['Claude Code', 'Cursor', 'Visual Studio Code', 'mcp-inspector', 'claude-ai'], cidx) AS client_name,
    arrayElement(multiIf(
      cidx = 1, ['2.4.1', '2.4.1', '2.3.8'],
      cidx = 2, ['1.7.42', '1.7.42', '1.6.14'],
      cidx = 3, ['1.104.2', '1.103.1', '1.103.1'],
      cidx = 4, ['0.16.2', '0.16.2', '0.15.0'],
      ['1.0.0', '1.0.0', '1.0.0']),
      1 + (cityHash64('gwgcliv', number) % 3)) AS client_version,
    cityHash64('gwgcliseen', number) % 9 > 0 AS client_seen,
    arrayElement(['amara@demo.getgram.ai', 'priya@demo.getgram.ai', 'mateo@demo.getgram.ai'],
                 1 + (cityHash64('gwgu', number) % 3)) AS email,
    -- Only while GitHub was still a member: four to eleven days back.
    toDateTime64(toStartOfDay(now()), 9)
      - toIntervalDay(4 + (number % 8)) + toIntervalHour(9 + (cityHash64('gwgh', number) % 9))
      + toIntervalMinute(cityHash64('gwgm', number) % 60) AS ts0,
    toUnixTimestamp64Nano(ts0) AS nano
  FROM numbers(24)
);

-- What the Claude Code hooks record when an agent calls the gateway itself:
-- hook rows whose gram.mcp.server_url is the gateway endpoint URL. The tool
-- usage classifier matches that URL to the gateway, so Tool Logs and Insights
-- show them as the gateway rather than as an unknown (shadow) MCP server.
INSERT INTO telemetry_logs
  (time_unix_nano, observed_time_unix_nano, severity_text, body, trace_id,
   attributes, resource_attributes, gram_project_id, gram_urn, service_name, gram_chat_id)
SELECT
  nano,
  nano,
  'INFO',
  concat('Hook: PostToolUse ', tool_name),
  lower(hex(MD5(concat('gram-demo-gwhook-', toString(i))))),
  concat(
    '{"gram.event.source":"hook"',
    ',"gram.hook.source":"claude-code"',
    ',"gram.hook.event":"PostToolUse"',
    ',"gram.tool.name":"', tool_name, '"',
    ',"gram.tool_call.source":"acme-demo-gateway"',
    ',"gram.mcp.match":"https://app.getgram.ai/mcp/acme-demo-gateway"',
    ',"gram.mcp.server_url":"https://app.getgram.ai/mcp/acme-demo-gateway"',
    ',"gen_ai.tool.call.result":"ok"',
    ',"gen_ai.tool.call.id":"call_demo_gw_', toString(i), '"',
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
    toUUID('dec0de00-0000-4000-a000-000000000001') AS proj,
    -- Odd chats carry Claude provenance, matching the rest of the hook rows.
    lower(hex(MD5(concat('gram-demo-chat-', toString(2 * number + 1))))) AS h,
    concat(substring(h, 1, 8), '-', substring(h, 9, 4), '-5', substring(h, 14, 3), '-8',
           substring(h, 18, 3), '-', substring(h, 21, 12)) AS chat_id,
    -- Same funnel shape as the discovery rows, ending in an execute_tool.
    multiIf(number % 10 < 4, 'list_servers', number % 10 < 6, 'describe_server',
            number % 10 < 8, 'describe_tools', 'execute_tool') AS tool_name,
    arrayElement(['amara@demo.getgram.ai', 'jonas@demo.getgram.ai', 'priya@demo.getgram.ai',
                  'mateo@demo.getgram.ai', 'hana@demo.getgram.ai', 'lucas@demo.getgram.ai'],
                 1 + (cityHash64('gwhu', number) % 6)) AS email,
    toDateTime64(toStartOfDay(now()), 9)
      - toIntervalDay(number % 12) + toIntervalHour(8 + (cityHash64('gwhh', number) % 11))
      + toIntervalMinute(cityHash64('gwhm', number) % 60) AS ts0,
    toUnixTimestamp64Nano(if(ts0 > now64(9) - toIntervalMinute(30), ts0 - toIntervalDay(1), ts0)) AS nano
  FROM numbers(40)
);

-- Meter usage is independent of telemetry-derived invoice estimates. Every
-- meter has 96 immutable facts across 12 days, with enough facets for a remainder.
-- Product-specific volumes keep all three estimated-spend series visible at
-- PAYG list prices without changing the number or identity of the readings.
INSERT INTO billing_meter_readings_by_time
  (id, organization_id, project_id, meter_id, operation_id, unit,
   measurement_method, value, occurred_at, produced_at, corrects_reading_id, attributes)
SELECT
  toUUID(concat(substring(h, 1, 8), '-', substring(h, 9, 4), '-5', substring(h, 14, 3),
                '-8', substring(h, 18, 3), '-', substring(h, 21, 12))),
  'org_gram_demo_workspace',
  toUUID('dec0de00-0000-4000-a000-000000000001'),
  meter,
  concat('gram-demo-meter-operation-', toString(number)),
  if(meter_index IN (2, 3), 'bytes', 'stokens'),
  if(meter_index IN (2, 3), 'http_body_bytes', 'tiktoken_o200k_base'),
  toInt64((1000 + cityHash64('meter-volume', number) % 9000)
    * multiIf(meter_index IN (2, 3), 2048, meter_index = 1, 100, 10)),
  least(toDateTime64(toStartOfDay(now('UTC')), 9, 'UTC') - toIntervalDay(intDiv(sample, 8))
    + toIntervalHour(8 + sample % 8), now64(9, 'UTC') - toIntervalMinute(30)),
  now64(9, 'UTC'),
  NULL,
  multiIf(
    meter_index = 1,
    map(
      'codec', 'o200k_base',
      'model', arrayElement(['claude-sonnet-4', 'gpt-4.1'], 1 + sample % 2),
      'provider', arrayElement(['anthropic', 'openai'], 1 + sample % 2),
      'hook_source', arrayElement(['claude-code', 'cursor'], 1 + sample % 2),
      'hook_hostname', concat('gram-demo-device-', toString(1 + sample % 8)),
      'account_type', if(sample % 3 = 0, 'personal', 'enterprise'),
      'billing_mode', if(sample % 3 = 0, 'flat_rate', 'usage_based'),
      'workload_source', arrayElement(['hook', 'import', 'assistant', 'native'], 1 + sample % 4),
      'assistant_id', if(sample % 4 = 2, 'gram-demo-managed-agent-1', ''),
      'billing_user_id', concat('user_demo_', person),
      'billing_user_account_email', concat(person, '@demo.getgram.ai'),
      'billing_user_division_name', if(sample % 2 = 0, 'Product', 'Operations'),
      'billing_user_department_name', arrayElement(
        ['Engineering', 'Support', 'Security', 'Design', 'Finance', 'Sales', 'Research', ''], 1 + sample % 8),
      'billing_user_job_title', if(sample % 2 = 0, 'Engineer', 'Specialist'),
      'billing_user_employee_type', if(sample % 3 = 0, 'Contractor', 'Full-time'),
      'billing_user_cost_center_name', concat('DEMO-', toString(1 + sample % 8)),
      'billing_user_rbac_roles', if(sample % 2 = 0, '["member"]', '["admin","member"]'),
      'billing_user_directory_groups', if(sample % 2 = 0, '["Engineering"]', '["Operations","Security"]')),
    meter_index IN (2, 3),
    map(
      'mcp_server_type', if(sample % 2 = 0, 'externalmcp', 'toolset'),
      'mcp_server_id', concat('gram-demo-meter-server-', toString(1 + sample % 8)),
      'mcp_server_slug', concat('acme-demo-server-', toString(1 + sample % 4)),
      'custom_domain', if(sample % 3 = 0, 'mcp.demo.getgram.ai', ''),
      'request_path', concat('/mcp/acme-demo-server-', toString(1 + sample % 4))),
    map(
      'codec', 'o200k_base',
      'risk_policy_id', if(sample % 4 = 0, '', 'dec0de00-0000-4000-a000-00000000f001'),
      'risk_policy_version', if(sample % 4 = 0, '', toString(1 + sample % 2)),
      'risk_policy_link_status', if(sample % 4 = 0, 'unlinked', 'linked'),
      'risk_policy_link_reason', if(sample % 4 = 0, 'direct_scan', ''),
      'scan_execution_path', if(sample % 2 = 0, 'inline', 'background'),
      'message_type', arrayElement(['user', 'assistant', 'tool_result'], 1 + sample % 3),
      'message_link_status', if(sample % 4 = 0, 'unlinked', 'linked'),
      'message_link_reason', if(sample % 4 = 0, 'no_message', ''),
      'hook_source', if(sample % 2 = 0, 'claude-code', 'cursor'),
      'message_user_id', concat('user_demo_', person),
      'model', if(meter_index IN (6, 7), 'gpt-4.1-mini', ''),
      'provider', if(meter_index IN (6, 7), 'openai', ''),
      'tool_name', if(sample % 3 = 2, 'search_code', '')))
FROM (
  SELECT
    number,
    1 + number % 9 AS meter_index,
    intDiv(number, 9) AS sample,
    arrayElement([
      'gram.agent_session.storage', 'gram.mcp.bandwidth.ingress', 'gram.mcp.bandwidth.egress',
      'gram.risk.scan.gitleaks', 'gram.risk.scan.presidio', 'gram.risk.scan.prompt_injection',
      'gram.risk.scan.prompt_policy', 'gram.risk.scan.custom_rules', 'gram.risk.scan.cli_destructive'
    ], meter_index) AS meter,
    arrayElement(['amara', 'jonas', 'priya', 'mateo', 'hana', 'lucas'], 1 + sample % 6) AS person,
    lower(hex(MD5(concat('gram-demo-meter-reading-', toString(number))))) AS h
  FROM numbers(864)
);

-- One identical physical redelivery per meter. Raw FINAL still collapses the
-- ledger read, while the incremental summary intentionally counts each delivery.
INSERT INTO billing_meter_readings_by_time
  (id, organization_id, project_id, meter_id, operation_id, unit,
   measurement_method, value, occurred_at, produced_at, corrects_reading_id, attributes)
SELECT id, organization_id, project_id, meter_id, operation_id, unit,
       measurement_method, value, occurred_at, produced_at, corrects_reading_id, attributes
FROM billing_meter_readings_by_time FINAL
WHERE organization_id = 'org_gram_demo_workspace' AND reading_kind = 'usage'
ORDER BY meter_id, operation_id
LIMIT 1 BY meter_id
SETTINGS do_not_merge_across_partitions_select_final = 1;

SELECT throwIf(
  (SELECT count() FROM billing_meter_readings_by_time FINAL
   WHERE organization_id = 'org_gram_demo_workspace' AND reading_kind = 'usage') != 864,
  'demo seed postflight: meter usage missing or duplicated')
SETTINGS do_not_merge_across_partitions_select_final = 1;

SELECT throwIf(
  (SELECT uniqExact(meter_id) FROM billing_meter_readings_by_time FINAL
   WHERE organization_id = 'org_gram_demo_workspace' AND reading_kind = 'usage') != 9
  OR (SELECT count() FROM billing_meter_readings_by_time FINAL
      WHERE organization_id = 'org_gram_demo_workspace' AND reading_kind != 'usage') != 0,
  'demo seed postflight: meter families missing or unexpected non-usage readings')
SETTINGS do_not_merge_across_partitions_select_final = 1;

SELECT throwIf(
  (SELECT sum(reading_count) FROM billing_meter_daily_summaries
   WHERE organization_id = 'org_gram_demo_workspace'
     AND facet = 'total') != 873,
  'demo seed postflight: ordinary meter deliveries missing or duplicated');

-- Postflight asserts: rows landed, the cost/session MVs actually fired, and
-- nothing leaked outside the demo scope. throwIf aborts the script (non-zero
-- exit for the runner) when violated.
SELECT throwIf(
  (SELECT count() FROM telemetry_logs WHERE gram_project_id IN
     (toUUID('dec0de00-0000-4000-a000-000000000001'))
   ) < 1500,
  'demo seed postflight: expected >= 1500 demo telemetry rows');

SELECT throwIf(
  (SELECT count() FROM telemetry_logs
   WHERE gram_project_id IN (toUUID('dec0de00-0000-4000-a000-000000000001'))
     AND meta_mcp_server_id != ''
     AND event_source = 'meta_discovery') < 150,
  'demo seed postflight: gateway discovery rows missing');

SELECT throwIf(
  (SELECT uniqExact(mcp_server_id) FROM telemetry_logs
   WHERE gram_project_id IN (toUUID('dec0de00-0000-4000-a000-000000000001'))
     AND meta_mcp_server_id != ''
     AND event_source = 'tool_call') < 4,
  'demo seed postflight: gateway member calls do not cover every member');

SELECT throwIf(
  (SELECT count() FROM telemetry_logs
   WHERE gram_project_id IN (toUUID('dec0de00-0000-4000-a000-000000000001'))
     AND meta_mcp_server_id != ''
     AND event_source = 'tool_call'
     AND mcp_server_id = concat(substring(lower(hex(MD5('gram-demo-mcpserver-github'))), 1, 8), '-',
                                substring(lower(hex(MD5('gram-demo-mcpserver-github'))), 9, 4), '-5',
                                substring(lower(hex(MD5('gram-demo-mcpserver-github'))), 14, 3), '-8',
                                substring(lower(hex(MD5('gram-demo-mcpserver-github'))), 18, 3), '-',
                                substring(lower(hex(MD5('gram-demo-mcpserver-github'))), 21, 12))) < 24,
  'demo seed postflight: removed gateway member dispatches missing');

SELECT throwIf(
  (SELECT count() FROM telemetry_logs
   WHERE gram_project_id IN (toUUID('dec0de00-0000-4000-a000-000000000001'))
     AND event_source = 'hook'
     AND endsWith(toString(attributes.gram.mcp.server_url), '/mcp/acme-demo-gateway')) < 40,
  'demo seed postflight: hook-observed gateway calls missing');

SELECT throwIf(
  (SELECT uniqExact(chat_id) FROM chat_session_summaries WHERE gram_project_id IN
     (toUUID('dec0de00-0000-4000-a000-000000000001'))
   ) < 180,
  'demo seed postflight: chat_session_summaries_mv missing sessions');

-- Every request-shaped row carries gen_ai.response.id, the field the per-user
-- summary counts chat requests by. Without it "Chat requests" reads 0 on every
-- identity while the chat count beside it reads correctly, which looks like a
-- broken panel rather than absent data.
--
-- Request-shaped means the row reports token usage: that is what separates an
-- LLM request from the tool-call and hook rows seeded alongside it, and it
-- stays true of request rows added later regardless of their body text. A
-- global floor would not catch a new insert that forgot the field, so the
-- assert is that no such row is missing it, with a floor beside it so an
-- empty table cannot satisfy the first check vacuously.
SELECT throwIf(
  (SELECT countIf(toString(attributes.gen_ai.response.id) = '') FROM telemetry_logs
   WHERE gram_project_id IN (toUUID('dec0de00-0000-4000-a000-000000000001'))
     AND (toString(attributes.input_tokens) != ''
          OR toString(attributes.gen_ai.usage.input_tokens) != '')
   ) > 0,
  'demo seed postflight: request rows missing gen_ai.response.id');

SELECT throwIf(
  (SELECT count() FROM telemetry_logs
   WHERE gram_project_id IN (toUUID('dec0de00-0000-4000-a000-000000000001'))
     AND (toString(attributes.input_tokens) != ''
          OR toString(attributes.gen_ai.usage.input_tokens) != '')
   ) < 180,
  'demo seed postflight: too few request rows');

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

SELECT throwIf(
  (SELECT uniqExact(target_id) FROM ai_detections
   WHERE organization_id = 'org_gram_demo_workspace') < 15,
  'demo seed postflight: ai_detections missing targets');

-- Each AI Discovery tab reads one category, so a category with no rows is an
-- empty page. Assert all three survived the reseed rather than only the
-- target count, which cannot tell a missing tab from a missing tool.
SELECT throwIf(
  (SELECT uniqExact(category) FROM ai_detections
   WHERE organization_id = 'org_gram_demo_workspace'
     AND category IN ('harness', 'assistant', 'local_model')) < 3,
  'demo seed postflight: ai_detections must cover harness, assistant and local_model');

SELECT throwIf(
  (SELECT count() FROM ai_scan_receipts
   WHERE organization_id = 'org_gram_demo_workspace') < 25,
  'demo seed postflight: ai_scan_receipts missing rows');

-- The Watchdog groups by these three denormalized columns. A mirror that
-- forgot them still lists signals, but every App/Team grouping and every
-- top-user row renders empty, which is the failure this catches.
SELECT throwIf(
  (SELECT count() FROM risk_findings
   WHERE organization_id = 'org_gram_demo_workspace'
     AND (chat_source = '' OR team = '' OR user_email = '')) > 0,
  'demo seed postflight: risk_findings missing chat_source/team/user_email attribution');

-- Platform MCP summarizes rule-level impact, not individual finding rows.
-- Keep at least one live cluster with multiple attributed users and clients
-- plus a stored display sample so that its privacy-safe evidence path is exercised.
SELECT throwIf(
  (SELECT count() FROM (
    SELECT rule_id
    FROM risk_findings
    WHERE organization_id = 'org_gram_demo_workspace'
      AND project_id = 'dec0de00-0000-4000-a000-000000000001'
      AND excluded_at IS NULL AND false_positive_at IS NULL
      AND dead_letter_reason = ''
    GROUP BY rule_id
    HAVING uniqExactIf(if(external_user_id != '', external_user_id, user_id),
                      external_user_id != '' OR user_id != '') > 1
       AND uniqExactIf(chat_source, chat_source != '') > 1
       AND countIf(match_redacted != '') > 0
  )) = 0,
  'demo seed postflight: watchdog needs multi-user multi-client evidence');

SELECT throwIf(
  (SELECT count() FROM risk_findings
   WHERE organization_id = 'org_gram_demo_workspace'
     AND mcp_server_id != ''
     AND (execution_id = '' OR toolset_id = '' OR tool_name = ''
          OR phase != 'response' OR mediation_surface != 'hosted_mcp'
          OR mcp_method != 'tools/call' OR principal_kind != 'user'
          OR identity_stamped = false OR enforcement_outcome != 'logged')) > 0
  OR
  (SELECT count() FROM risk_findings
   WHERE organization_id = 'org_gram_demo_workspace'
     AND mcp_server_id != '') = 0,
  'demo seed postflight: mediated risk findings missing complete MCP attribution');

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

-- The Shadow AI inventory is pinned exactly, not bounded. Its size is what
-- seed/demo/PAGES.md documents and what the per-device receipt counts above
-- must add up to, and both drifted once already when detections were added
-- without either being updated. An exact count fails the seed the moment the
-- three stop agreeing, and the category check guards that every kind of tool
-- the catalog distinguishes is represented on the page, since a category with
-- no rows renders as an empty filter.
SELECT throwIf(
  (SELECT count() FROM ai_detections
   WHERE organization_id = 'org_gram_demo_workspace') <> 52,
  'demo seed postflight: expected exactly 52 demo AI detection rows, so update PAGES.md and the receipt match counts alongside any change');

SELECT throwIf(
  (SELECT uniqExact(category) FROM ai_detections
   WHERE organization_id = 'org_gram_demo_workspace') <> 3,
  'demo seed postflight: demo AI detections must cover harness, assistant and local_model');

-- Agent events: what Explore (the semantic catalog over agent_events) reads.
-- 144 sessions over the trailing 12 days, two per user per day, dealt across
-- the two harnesses the dialects know: users 1, 3 and 5 on Claude Code
-- (anthropic, usage and cost stated on api_request rows), users 2, 4 and 6 on
-- Codex (openai, tokens stated but no cost, which is what the provider
-- emits). Each session has 2-5 turns, each turn a prompt, an api_request
-- and an api_response (an api_error now and then), and 0-3 tool calls. A
-- Claude tool call is a tool_decision followed by a tool_call_result unless
-- the decision rejected it, so a blocked call is a decision alone; a Codex
-- call is its result. Every row family has its own gram-demo-agent- prefix
-- and occurred_at is now()-relative, so a reseed lands inside the window.
--
-- Turn rows: a prompt, its API request and the response, per turn.
INSERT INTO agent_events
  (organization_id, project_id, occurred_at_unix_nano, observed_at_unix_nano,
   record_id, session_id, turn_id, event_id, event_type, raw_event_name,
   source, provider, surface, user_id, user_email, external_user_id,
   account_type, billing_mode, external_org_id, device_id,
   department_name, division_name, job_title, employee_type, cost_center_name,
   roles, groups, model, query_source, skill_name, agent_name,
   mcp_server_name, mcp_tool_name, tool_name, text, outcome, outcome_message,
   duration_nano, input_content, output_content,
   input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, cost_usd,
   attributes, resource_attributes, scope_attributes)
SELECT
  'org_gram_demo_workspace',
  'dec0de00-0000-4000-a000-000000000001',
  occurred,
  occurred + 800000000,
  concat('gram-demo-agent-', kind, '-', toString(i), '-', toString(t)),
  session_v,
  concat('prompt_demo_', toString(i), '_', toString(t)),
  if(kind = 'prompt', concat('gram-demo-agent-prompt-', toString(i), '-', toString(t)),
     concat('req_demo_', toString(i), '_', toString(t))),
  multiIf(kind = 'prompt', 'prompt', kind = 'request', 'api_request', failed, 'api_error', 'api_response'),
  concat(if(on_claude, 'claude_code.', 'codex.'),
         multiIf(kind = 'prompt', 'user_prompt', kind = 'request', 'api_request', failed, 'api_error', 'api_response')),
  arrayElement(['claude-code', 'codex'], 1 + toUInt32(NOT on_claude)), provider_v, surface_v, user_id_v, email_v, '',
  '', '', '', device_v,
  department_v, division_v, title_v, emp_type_v, cost_center_v,
  roles_v, groups_v,
  model_v, '', '', '',
  '', '', '',
  if(kind = 'prompt',
     arrayElement(['Why is the refund job retrying every five minutes',
                   'List the open incidents for the billing service and summarise each one',
                   'Find every place the invoice total is rounded and check the currency',
                   'Draft a runbook step for rotating the payments webhook secret',
                   'Which customers hit the rate limit on the export endpoint this week',
                   'Explain what the support-refunds skill does before I run it',
                   'Check whether the nightly reconciliation finished and how long it took',
                   'Summarise the last three deploys of the gateway and any rollbacks'],
                  1 + toUInt32((i + t) % 8)),
     ''),
  multiIf(kind != 'response', '', failed, 'error', 'ok'),
  if(kind = 'response' AND failed, 'overloaded_error: the model is busy, retry later', ''),
  if(kind = 'response', response_nano, 0),
  '', '',
  if(kind = 'request', in_tokens, 0),
  if(kind = 'request', out_tokens, 0),
  if(kind = 'request' AND on_claude, cache_read, 0),
  if(kind = 'request' AND on_claude, cache_write, 0),
  if(kind = 'request' AND on_claude,
     if(model_v = 'claude-sonnet-5',
        (in_tokens * 3 + out_tokens * 15 + cache_read * 0.3 + cache_write * 3.75) / 1000000,
        (in_tokens * 0.8 + out_tokens * 4 + cache_read * 0.08 + cache_write * 1) / 1000000),
     0),
  '{}', '{}', '{}'
FROM (
  SELECT
    intDiv(number, 15) AS i,
    intDiv(number % 15, 3) AS t,
    arrayElement(['prompt', 'request', 'response'], 1 + toUInt32(number % 3)) AS kind,

    toUInt32(i % 6) + 1 AS uidx,
    intDiv(i, 12) AS day_back,
    intDiv(i % 12, 6) AS slot,
    toUnixTimestamp64Nano(now64(9))
      - toInt64(day_back) * 86400000000000
      - toInt64(slot * 5 + 2 + (i * 7) % 3) * 3600000000000
      - toInt64((i * 13) % 50) * 60000000000 AS session_start,
    2 + toUInt32(i % 4) AS turns,
    uidx IN (1, 3, 5) AS on_claude,
    if(on_claude, 'claude-code', 'codex') AS surface_v,
    if(on_claude, 'anthropic', 'openai') AS provider_v,
    multiIf(NOT on_claude, 'gpt-5-codex', i % 3 = 0, 'claude-haiku-4-5-20251001', 'claude-sonnet-5') AS model_v,
    lower(hex(MD5(concat('gram-demo-agent-session-', toString(i))))) AS hs,
    concat(substring(hs, 1, 8), '-', substring(hs, 9, 4), '-5', substring(hs, 14, 3), '-8',
           substring(hs, 18, 3), '-', substring(hs, 21, 12)) AS session_v,
    arrayElement(['user_demo_amara', 'user_demo_jonas', 'user_demo_priya',
                  'user_demo_mateo', 'user_demo_hana', 'user_demo_lucas'], uidx) AS user_id_v,
    arrayElement(['amara@demo.getgram.ai', 'jonas@demo.getgram.ai', 'priya@demo.getgram.ai',
                  'mateo@demo.getgram.ai', 'hana@demo.getgram.ai', 'lucas@demo.getgram.ai'], uidx) AS email_v,
    arrayElement(['amara-mbp.local', 'jonas-mbp.local', 'priya-mbp.local',
                  'mateo-mbp.local', 'hana-mbp.local', 'lucas-mbp.local'], uidx) AS device_v,
    -- Directory attributes mirror demo_* arrays in postgres.sql so dimensions agree across stores.
    arrayElement(['Support Engineering', 'Support Engineering', 'Platform Engineering',
                  'Platform Engineering', 'Billing Operations', 'Engineering Leadership'], uidx) AS department_v,
    arrayElement(['Customer Experience', 'Customer Experience', 'R&D',
                  'R&D', 'Customer Experience', 'R&D'], uidx) AS division_v,
    arrayElement(['Support Engineer', 'Senior Support Engineer', 'Platform Engineer',
                  'Site Reliability Engineer', 'Billing Analyst', 'Engineering Manager'], uidx) AS title_v,
    arrayElement(['full-time', 'full-time', 'full-time', 'contractor', 'part-time', 'full-time'], uidx) AS emp_type_v,
    arrayElement(['CC-SUP-4100', 'CC-SUP-4100', 'CC-ENG-2200', 'CC-ENG-2200', 'CC-OPS-3300', 'CC-ENG-2200'], uidx) AS cost_center_v,
    [arrayElement(['support', 'support', 'engineering', 'engineering', 'billing', 'engineering'], uidx)] AS roles_v,
    [arrayElement(['Frontline Support', 'Frontline Support', 'Infra', 'Reliability', 'Billing Ops', 'Leadership'], uidx)] AS groups_v,
    session_start + toInt64(t) * 95000000000 AS turn_start,
    (i * t) % 23 = 5 AS failed,
    toInt64(1200 + (i * 31 + t * 17) % 2600) AS in_tokens,
    toInt64(150 + (i * 7 + t * 29) % 900) AS out_tokens,
    toInt64(800 + (i * 5) % 3000) AS cache_read,
    toInt64(if(t = 0, 1500, 0)) AS cache_write,
    toInt64(1500 + (i * 11 + t * 3) % 9000) * 1000000 AS response_nano,
    multiIf(kind = 'prompt', turn_start,
            kind = 'request', turn_start + 2000000000,
            turn_start + 2000000000 + response_nano) AS occurred
  FROM numbers(144 * 5 * 3)
)
WHERE t < turns;

-- Tool rows: 0-3 calls per turn. A Claude call is a decision then a result
-- (or the rejecting decision alone); a Codex call is its result.
INSERT INTO agent_events
  (organization_id, project_id, occurred_at_unix_nano, observed_at_unix_nano,
   record_id, session_id, turn_id, event_id, event_type, raw_event_name,
   source, provider, surface, user_id, user_email, external_user_id,
   account_type, billing_mode, external_org_id, device_id,
   department_name, division_name, job_title, employee_type, cost_center_name,
   roles, groups, model, query_source, skill_name, agent_name,
   mcp_server_name, mcp_tool_name, tool_name, text, outcome, outcome_message,
   duration_nano, input_content, output_content,
   input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, cost_usd,
   attributes, resource_attributes, scope_attributes)
SELECT
  'org_gram_demo_workspace',
  'dec0de00-0000-4000-a000-000000000001',
  occurred,
  occurred + 800000000,
  concat('gram-demo-agent-', kind, '-', toString(i), '-', toString(t), '-', toString(k)),
  session_v,
  concat('prompt_demo_', toString(i), '_', toString(t)),
  concat('call_demo_agent_', toString(i), '_', toString(t), '_', toString(k)),
  if(kind = 'decision', 'tool_decision', 'tool_call_result'),
  concat(if(on_claude, 'claude_code.', 'codex.'), if(kind = 'decision', 'tool_decision', 'tool_result')),
  arrayElement(['claude-code', 'codex'], 1 + toUInt32(NOT on_claude)), provider_v, surface_v, user_id_v, email_v, '',
  '', '', '', device_v,
  department_v, division_v, title_v, emp_type_v, cost_center_v,
  roles_v, groups_v,
  model_v, '', '', '',
  if(tool_v = 'mcp_tool', 'acme-crm', ''),
  if(tool_v = 'mcp_tool', arrayElement(['lookup_customer', 'process_refund', 'list_invoices'], 1 + toUInt32((i + k) % 3)), ''),
  tool_v,
  '',
  multiIf(kind = 'decision', if(rejected, 'rejected', 'ok'), errored, 'error', 'ok'),
  multiIf(kind = 'decision' AND rejected, 'blocked by policy: write outside the checkout',
          kind = 'result' AND errored, 'exit status 1', ''),
  if(kind = 'result', tool_nano, 0),
  '', '',
  0, 0, 0, 0, 0,
  '{}', '{}', '{}'
FROM (
  SELECT
    intDiv(number, 30) AS i,
    intDiv(number % 30, 6) AS t,
    intDiv(number % 6, 2) AS k,
    arrayElement(['decision', 'result'], 1 + toUInt32(number % 2)) AS kind,

    toUInt32(i % 6) + 1 AS uidx,
    intDiv(i, 12) AS day_back,
    intDiv(i % 12, 6) AS slot,
    toUnixTimestamp64Nano(now64(9))
      - toInt64(day_back) * 86400000000000
      - toInt64(slot * 5 + 2 + (i * 7) % 3) * 3600000000000
      - toInt64((i * 13) % 50) * 60000000000 AS session_start,
    2 + toUInt32(i % 4) AS turns,
    uidx IN (1, 3, 5) AS on_claude,
    if(on_claude, 'claude-code', 'codex') AS surface_v,
    if(on_claude, 'anthropic', 'openai') AS provider_v,
    multiIf(NOT on_claude, 'gpt-5-codex', i % 3 = 0, 'claude-haiku-4-5-20251001', 'claude-sonnet-5') AS model_v,
    lower(hex(MD5(concat('gram-demo-agent-session-', toString(i))))) AS hs,
    concat(substring(hs, 1, 8), '-', substring(hs, 9, 4), '-5', substring(hs, 14, 3), '-8',
           substring(hs, 18, 3), '-', substring(hs, 21, 12)) AS session_v,
    arrayElement(['user_demo_amara', 'user_demo_jonas', 'user_demo_priya',
                  'user_demo_mateo', 'user_demo_hana', 'user_demo_lucas'], uidx) AS user_id_v,
    arrayElement(['amara@demo.getgram.ai', 'jonas@demo.getgram.ai', 'priya@demo.getgram.ai',
                  'mateo@demo.getgram.ai', 'hana@demo.getgram.ai', 'lucas@demo.getgram.ai'], uidx) AS email_v,
    arrayElement(['amara-mbp.local', 'jonas-mbp.local', 'priya-mbp.local',
                  'mateo-mbp.local', 'hana-mbp.local', 'lucas-mbp.local'], uidx) AS device_v,
    -- Directory attributes mirror demo_* arrays in postgres.sql so dimensions agree across stores.
    arrayElement(['Support Engineering', 'Support Engineering', 'Platform Engineering',
                  'Platform Engineering', 'Billing Operations', 'Engineering Leadership'], uidx) AS department_v,
    arrayElement(['Customer Experience', 'Customer Experience', 'R&D',
                  'R&D', 'Customer Experience', 'R&D'], uidx) AS division_v,
    arrayElement(['Support Engineer', 'Senior Support Engineer', 'Platform Engineer',
                  'Site Reliability Engineer', 'Billing Analyst', 'Engineering Manager'], uidx) AS title_v,
    arrayElement(['full-time', 'full-time', 'full-time', 'contractor', 'part-time', 'full-time'], uidx) AS emp_type_v,
    arrayElement(['CC-SUP-4100', 'CC-SUP-4100', 'CC-ENG-2200', 'CC-ENG-2200', 'CC-OPS-3300', 'CC-ENG-2200'], uidx) AS cost_center_v,
    [arrayElement(['support', 'support', 'engineering', 'engineering', 'billing', 'engineering'], uidx)] AS roles_v,
    [arrayElement(['Frontline Support', 'Frontline Support', 'Infra', 'Reliability', 'Billing Ops', 'Leadership'], uidx)] AS groups_v,
    session_start + toInt64(t) * 95000000000 AS turn_start,
    (i + t) % 4 AS tools,
    arrayElement(['Bash', 'Read', 'Grep', 'Glob', 'Edit', 'mcp_tool'], 1 + toUInt32((i * 3 + t * 5 + k * 7) % 6)) AS tool_v,
    (i * 5 + t + k) % 25 = 0 AS rejected,
    (i * 3 + t * 7 + k) % 17 = 0 AS errored,
    toInt64(200 + (i * 37 + t * 11 + k * 5) % 5800) * 1000000 AS tool_nano,
    turn_start + 15000000000 + toInt64(k) * 20000000000 + if(kind = 'result', tool_nano, 0) AS occurred
  FROM numbers(144 * 5 * 3 * 2)
)
WHERE t < turns
  AND k < tools
  -- Codex has no decision events, and a rejected Claude call has no result.
  AND NOT (kind = 'decision' AND NOT on_claude)
  AND NOT (kind = 'result' AND on_claude AND rejected);

-- Postflight: the Explore datasets have sessions and tool calls to collapse,
-- every demo user and both harnesses are represented, and cost is only ever
-- stated where the provider states it.
SELECT throwIf(
  (SELECT count() FROM agent_events
   WHERE organization_id = 'org_gram_demo_workspace') < 2000,
  'demo seed postflight: expected >= 2000 demo agent_events rows');

SELECT throwIf(
  (SELECT uniqExact(user_email) FROM agent_events
   WHERE organization_id = 'org_gram_demo_workspace') != 6
  OR (SELECT uniqExact(surface) FROM agent_events
      WHERE organization_id = 'org_gram_demo_workspace') != 2,
  'demo seed postflight: demo agent events must cover all six users and both harnesses');

SELECT throwIf(
  (SELECT countIf(cost_usd > 0) FROM agent_events
   WHERE organization_id = 'org_gram_demo_workspace' AND surface = 'codex') != 0,
  'demo seed postflight: Codex states no cost, so no Codex demo row may carry one');
