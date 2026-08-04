-- Demo org seed — Postgres side.
--
-- Defines demo.ensure_demo_org(), a fully self-contained, idempotent function
-- that (re)generates the shared demo organization's data. Every write is scoped
-- to the fixed demo constants below; the function aborts before writing if any
-- preflight isolation check fails.
--
--   Local:  applied by `mise run seed:demo` (psql -f this file, which also
--           executes the function at the bottom).
--   Prod:   the function is installed once, then the scheduled runner executes
--           it daily via `gram demo-seed`. Timestamps are relative to
--           now(), so each run regenerates a fresh trailing ~12-day window.
--
-- Constants (must match seed/demo/clickhouse.sql):
--   org id       org_gram_demo_workspace
--   project id   dec0de00-0000-4000-a000-000000000001 ('Default' — the only
--                project; ...0002 survives only as a legacy delete scope)
--   chat ids     demo.det_uuid('gram-demo-chat-' || n) — md5 with RFC nibbles
--   message ids  demo.det_uuid('gram-demo-msg-' || n || '-' || m)
--   users        user_demo_* / *@demo.getgram.ai (parallel arrays below; the
--                per-chat owner index formula 1 + (n % 6) is mirrored in the
--                ClickHouse seed's arrayElement calls)

CREATE SCHEMA IF NOT EXISTS demo;

-- Deterministic RFC-compliant UUID from a name. Plain md5(...)::uuid leaves
-- random version/variant nibbles, which fails the dashboard SDK's strict
-- uuid validation (zod requires version 1-8 + variant [89ab]) — so the
-- version nibble is forced to '5' and the variant to '8'. The ClickHouse
-- seed reproduces this formatting exactly (see clickhouse.sql).
CREATE OR REPLACE FUNCTION demo.det_uuid(name text) RETURNS uuid
LANGUAGE sql IMMUTABLE
RETURN (overlay(overlay(md5(name) placing '5' from 13) placing '8' from 17))::uuid;

CREATE OR REPLACE FUNCTION demo.ensure_demo_org() RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
  demo_org  CONSTANT text := 'org_gram_demo_workspace';
  proj_a    CONSTANT uuid := 'dec0de00-0000-4000-a000-000000000001';
  proj_b    CONSTANT uuid := 'dec0de00-0000-4000-a000-000000000002';
  policy_a  CONSTANT uuid := 'dec0de00-0000-4000-a000-00000000f001';

  asset_id     CONSTANT uuid := 'dec0de00-0000-4000-a000-00000000a001';
  deploy_id    CONSTANT uuid := 'dec0de00-0000-4000-a000-00000000d001';
  doa_id       CONSTANT uuid := 'dec0de00-0000-4000-a000-00000000da01';
  toolset_1    CONSTANT uuid := 'dec0de00-0000-4000-a000-000000005e01';
  toolset_2    CONSTANT uuid := 'dec0de00-0000-4000-a000-000000005e02';
  rule_monthly CONSTANT uuid := 'dec0de00-0000-4000-a000-00000000e001';
  rule_weekly  CONSTANT uuid := 'dec0de00-0000-4000-a000-00000000e002';

  bulk_chats CONSTANT int := 60;

  titles CONSTANT text[] := ARRAY[
    'Incident triage: payment 401s', 'Refund request for order 4823',
    'Deploy rollout check', 'Webhook delivery debugging',
    'Latency regression on /orders', 'Billing dispute follow-up',
    'Schema migration review', 'On-call escalation summary',
    'Cache hit rate investigation', 'Rate limit tuning'
  ];
  questions CONSTANT text[] := ARRAY[
    'Can you check why the checkout flow is erroring?',
    'Pull the latest metrics for the orders service.',
    'Investigate the spike in 500s since this morning.',
    'Why is the worker queue backing up?',
    'Find the slow query hitting the dashboard.',
    'Walk me through the last incident timeline.'
  ];
  answers CONSTANT text[] := ARRAY[
    'Done. Metrics look within normal range — nothing alarming.',
    'Pulled the data: error rate is 0.2%, well under the alert threshold.',
    'The spike traces back to a slow downstream dependency; it recovered on its own.',
    'Queue depth is back to baseline after the worker pool scaled up.',
    'The slow query was missing an index; noted for a follow-up migration.',
    'Reconciled — every charge in the batch has a matching ledger entry.'
  ];

  -- Demo employee roster. Parallel arrays, NOT text[][]: subscripting a 2-D
  -- array with a single index (arr[i]) yields NULL in PostgreSQL, which
  -- silently nulled every chat owner in an earlier version. The identity
  -- profiles (division/department/...) mirror the user.attributes.* values on
  -- the ClickHouse rows so directory data and telemetry dimensions agree.
  demo_user_ids CONSTANT text[] := ARRAY[
    'user_demo_amara', 'user_demo_jonas', 'user_demo_priya',
    'user_demo_mateo', 'user_demo_hana', 'user_demo_lucas'];
  demo_user_emails CONSTANT text[] := ARRAY[
    'amara@demo.getgram.ai', 'jonas@demo.getgram.ai', 'priya@demo.getgram.ai',
    'mateo@demo.getgram.ai', 'hana@demo.getgram.ai', 'lucas@demo.getgram.ai'];
  demo_user_names CONSTANT text[] := ARRAY[
    'Amara Okafor', 'Jonas Lindqvist', 'Priya Raman',
    'Mateo Alvarez', 'Hana Sato', 'Lucas Meyer'];
  demo_divisions CONSTANT text[] := ARRAY[
    'Customer Experience', 'Customer Experience', 'R&D',
    'R&D', 'Customer Experience', 'R&D'];
  demo_departments CONSTANT text[] := ARRAY[
    'Support Engineering', 'Support Engineering', 'Platform Engineering',
    'Platform Engineering', 'Billing Operations', 'Engineering Leadership'];
  demo_titles CONSTANT text[] := ARRAY[
    'Support Engineer', 'Senior Support Engineer', 'Platform Engineer',
    'Site Reliability Engineer', 'Billing Analyst', 'Engineering Manager'];
  demo_emp_types CONSTANT text[] := ARRAY[
    'full-time', 'full-time', 'full-time', 'contractor', 'part-time', 'full-time'];
  demo_cost_centers CONSTANT text[] := ARRAY[
    'CC-SUP-4100', 'CC-SUP-4100', 'CC-ENG-2200', 'CC-ENG-2200', 'CC-OPS-3300', 'CC-ENG-2200'];
  demo_teams CONSTANT text[] := ARRAY[
    'Frontline Support', 'Frontline Support', 'Infra', 'Reliability', 'Billing Ops', 'Leadership'];

  tool_names CONSTANT text[] := ARRAY[
    'search_logs', 'get_metrics', 'query_db', 'get_customer',
    'list_deploys', 'process_refund', 'fetch_traces', 'check_health'];
  tool_urns  CONSTANT text[] := ARRAY[
    'tools:http:acme:search_logs', 'tools:http:acme:get_metrics',
    'tools:http:acme:query_db', 'tools:http:acme:get_customer',
    'tools:http:acme:list_deploys', 'tools:http:acme:process_refund',
    'tools:http:acme:fetch_traces', 'tools:http:acme:check_health'];
  audit_actions CONSTANT text[] := ARRAY[
    'toolset:create', 'deployments:create', 'api_key:create',
    'toolset:update', 'toolset:update', 'plugin:publish'];

  skill_content_refunds CONSTANT text :=
E'---\nname: support-refunds\ndescription: How to process customer refunds safely.\n---\n\n# Refund handling\n\n1. Verify the order id and amount with the customer.\n2. Use the process_refund tool with the confirmed amount.\n3. Never accept full card numbers in chat.\n';
  skill_content_triage CONSTANT text :=
E'---\nname: triage-incident\ndescription: Steps for triaging a production incident.\n---\n\n# Incident triage\n\n1. Pull error logs with search_logs for the affected window.\n2. Correlate with recent deploys via list_deploys.\n3. Escalate to on-call when the error budget is at risk.\n';
  skill_content_runbook CONSTANT text :=
E'---\nname: runbook\ndescription: General operational runbook for the Acme stack.\n---\n\n# Acme runbook\n\n- check_health before and after every rollout.\n- fetch_traces for latency regressions.\n- get_metrics to confirm recovery.\n';

  i int;
  m int;
  n_msgs int;
  chat_id uuid;
  msg_id uuid;
  flagged_msg uuid;
  chat_proj uuid;
  owner_idx int;
  chat_ts timestamptz;
  chat_policy uuid;
  leak text;
  role_urn_admin text;
  role_urn_member text;
  skill_id uuid;
  version_id uuid;
  refunds_version uuid;
  chat_count int;
  finding_count int;
  member_count int;
  tool_count int;
  stray int;
BEGIN
  ------------------------------------------------------------------
  -- Preflight isolation asserts: refuse to run if the demo constants
  -- collide with anything that is not unambiguously the demo org.
  -- Identity check deliberately does NOT use gram_account_type: the auth
  -- callback's org-metadata upsert overwrites it (e.g. to 'pro') whenever
  -- someone impersonates the org, and the seed's own upsert below restores
  -- it. A WorkOS link or a foreign slug, however, means this id belongs to a
  -- real org — abort.
  ------------------------------------------------------------------
  IF EXISTS (
    SELECT 1 FROM organization_metadata
    WHERE id = demo_org AND (workos_id IS NOT NULL OR slug <> 'acme-demo')
  ) THEN
    RAISE EXCEPTION 'demo seed aborted: org % exists but is not the demo org', demo_org;
  END IF;

  IF EXISTS (
    SELECT 1 FROM projects
    WHERE id IN (proj_a, proj_b) AND organization_id <> demo_org
  ) THEN
    RAISE EXCEPTION 'demo seed aborted: demo project id owned by another org';
  END IF;

  ------------------------------------------------------------------
  -- Org, projects, features, users, memberships, directory.
  -- projects has ON DELETE RESTRICT from deployments, and toolsets only SET
  -- NULL their project_id — so the deployment stack and toolsets are deleted
  -- explicitly before the projects delete cascades everything else
  -- (chats -> chat_messages -> risk_results, risk_policies, skills, ...).
  ------------------------------------------------------------------
  -- 'enterprise' account type: the demo must showcase enterprise-gated
  -- surfaces (Logs page and other EnterpriseGate features). Demo identity is
  -- carried by the fixed org id (constants.DemoOrganizationID) — NOT by
  -- account type, which the auth callback overwrites anyway.
  INSERT INTO organization_metadata (id, name, slug, gram_account_type, whitelisted)
  VALUES (demo_org, 'Acme Demo Org', 'acme-demo', 'enterprise', TRUE)
  ON CONFLICT (id) DO UPDATE
    SET name = EXCLUDED.name, slug = EXCLUDED.slug,
        gram_account_type = EXCLUDED.gram_account_type;

  DELETE FROM toolsets WHERE organization_id = demo_org;
  DELETE FROM deployment_statuses WHERE deployment_id IN
    (SELECT id FROM deployments WHERE organization_id = demo_org);
  DELETE FROM deployment_logs WHERE project_id IN (proj_a, proj_b);
  DELETE FROM deployments WHERE organization_id = demo_org;
  DELETE FROM assets WHERE project_id IN (proj_a, proj_b);
  DELETE FROM projects WHERE organization_id = demo_org;

  -- Single project: the demo org intentionally has exactly one project so
  -- new users land somewhere obvious. proj_b remains declared ONLY as a
  -- legacy-cleanup delete scope from when the seed created two projects.
  INSERT INTO projects (id, name, slug, organization_id) VALUES
    (proj_a, 'Default', 'default', demo_org);

  -- 'rbac' is required for the agent-sessions list: without it
  -- authz.ShouldEnforce is false and chatVisibilityScope falls back to
  -- own-sessions-only, hiding every seeded chat (owned by user_demo_*).
  INSERT INTO organization_features (organization_id, feature_name)
  SELECT demo_org, f
  FROM unnest(ARRAY['logs', 'tool_io_logs', 'session_capture', 'skills', 'rbac']) AS f
  ON CONFLICT (organization_id, feature_name) WHERE deleted IS FALSE DO NOTHING;

  FOR i IN 1 .. array_length(demo_user_ids, 1) LOOP
    INSERT INTO users (id, email, display_name, workos_id)
    VALUES (demo_user_ids[i], demo_user_emails[i], demo_user_names[i],
            'workos_' || demo_user_ids[i])
    ON CONFLICT (id) DO UPDATE
      SET email = EXCLUDED.email, display_name = EXCLUDED.display_name,
          workos_id = EXCLUDED.workos_id;
  END LOOP;

  -- Memberships: fake, credential-less members so team/enrollment/facepile
  -- surfaces render. Real users still never join the demo org — access is by
  -- impersonation only. No WorkOS sync job iterates local rows, so fake
  -- workos_* ids are inert while organization_metadata.workos_id stays NULL.
  DELETE FROM organization_user_relationships WHERE organization_id = demo_org;
  FOR i IN 1 .. array_length(demo_user_ids, 1) LOOP
    INSERT INTO organization_user_relationships
      (organization_id, user_id, workos_user_id, workos_membership_id, created_at)
    VALUES (demo_org, demo_user_ids[i], 'workos_' || demo_user_ids[i],
            'demo_mem_' || demo_user_ids[i], now() - (interval '40 days' * i));
  END LOOP;

  -- Role assignments (Roles column on the team page). Global roles are synced
  -- from WorkOS in real envs; tolerate their absence locally.
  SELECT 'role:global:' || id INTO role_urn_admin
  FROM global_roles WHERE workos_slug = 'admin' AND deleted_at IS NULL LIMIT 1;
  SELECT 'role:global:' || id INTO role_urn_member
  FROM global_roles WHERE workos_slug = 'member' AND deleted_at IS NULL LIMIT 1;

  DELETE FROM organization_role_assignments WHERE organization_id = demo_org;
  IF role_urn_admin IS NOT NULL AND role_urn_member IS NOT NULL THEN
    FOR i IN 1 .. array_length(demo_user_ids, 1) LOOP
      INSERT INTO organization_role_assignments
        (organization_id, workos_user_id, user_id, role_urn, workos_updated_at)
      VALUES (demo_org, 'workos_' || demo_user_ids[i], demo_user_ids[i],
              CASE WHEN demo_user_ids[i] IN ('user_demo_lucas', 'user_demo_priya')
                   THEN role_urn_admin ELSE role_urn_member END,
              now());
    END LOOP;
  END IF;

  -- Directory profiles: feed spend-rule audiences, enrollment attributes, and
  -- mirror the user.attributes.* identity on the ClickHouse telemetry.
  DELETE FROM directory_user_group_memberships WHERE directory_group_id IN
    (SELECT id FROM directory_groups WHERE organization_id = demo_org);
  DELETE FROM directory_users WHERE organization_id = demo_org;
  DELETE FROM directory_groups WHERE organization_id = demo_org;

  INSERT INTO directory_groups
    (organization_id, workos_directory_group_id, name, workos_created_at, workos_updated_at)
  SELECT demo_org, 'demo_grp_' || replace(lower(t), ' ', '-'), t, now(), now()
  FROM (SELECT DISTINCT unnest(demo_teams) AS t) g;

  FOR i IN 1 .. array_length(demo_user_ids, 1) LOOP
    INSERT INTO directory_users
      (organization_id, user_id, workos_directory_user_id, email, attributes,
       workos_created_at, workos_updated_at)
    VALUES (demo_org, demo_user_ids[i], 'demo_dir_' || demo_user_ids[i],
            demo_user_emails[i],
            jsonb_build_object(
              'division_name', demo_divisions[i],
              'department_name', demo_departments[i],
              'job_title', demo_titles[i],
              'employee_type', demo_emp_types[i],
              'cost_center_name', demo_cost_centers[i]),
            now(), now());

    INSERT INTO directory_user_group_memberships
      (directory_user_id, directory_group_id, workos_directory_user_id,
       workos_directory_group_id, workos_created_at)
    SELECT du.id, dg.id, du.workos_directory_user_id, dg.workos_directory_group_id, now()
    FROM directory_users du, directory_groups dg
    WHERE du.workos_directory_user_id = 'demo_dir_' || demo_user_ids[i]
      AND dg.organization_id = demo_org AND dg.name = demo_teams[i];
  END LOOP;

  -- AI provider accounts (enrollment page Accounts column): everyone has a
  -- team account under one shared fake provider org; mateo also carries a
  -- personal account so the personal-account story has an example.
  DELETE FROM user_accounts WHERE organization_id = demo_org;
  DELETE FROM device_owners WHERE organization_id = demo_org;
  DELETE FROM device_agent_syncs WHERE organization_id = demo_org;
  FOR i IN 1 .. array_length(demo_user_ids, 1) LOOP
    INSERT INTO user_accounts
      (organization_id, user_id, provider, external_org_id, external_account_uuid,
       external_account_id, email, account_type, billing_mode)
    VALUES (demo_org, demo_user_ids[i], 'anthropic', 'demo-ext-org-acme',
            'demo-acct-' || demo_user_ids[i], demo_user_ids[i] || '_acct',
            demo_user_emails[i], 'team', 'metered');

    INSERT INTO device_owners (organization_id, provider, device_id, linked_user_id)
    VALUES (demo_org, 'anthropic', 'demo-device-' || demo_user_ids[i], demo_user_ids[i]);

    IF i <= 4 THEN
      INSERT INTO device_agent_syncs (organization_id, email, first_seen_at, last_seen_at)
      VALUES (demo_org, demo_user_emails[i], now() - interval '9 days', now() - interval '3 hours');
    END IF;
  END LOOP;
  INSERT INTO user_accounts
    (organization_id, user_id, provider, external_org_id, external_account_uuid,
     external_account_id, email, account_type, billing_mode)
  VALUES (demo_org, 'user_demo_mateo', 'anthropic', 'demo-ext-org-personal',
          'demo-acct-mateo-personal', 'user_demo_mateo_personal',
          'mateo.alvarez@personal.example', 'personal', 'flat_rate');

  -- Enterprise billing contract: without a contracted TUM baseline the
  -- Billing page's Platform+Overage estimate shows "Requires a contracted
  -- baseline", which reads broken for an enterprise org.
  INSERT INTO billing_metadata (organization_id, tum_monthly_token_limit, billing_cycle_anchor_day)
  VALUES (demo_org, 50000000, 1)
  ON CONFLICT (organization_id) DO UPDATE
    SET tum_monthly_token_limit = EXCLUDED.tum_monthly_token_limit;

  ------------------------------------------------------------------
  -- Deployment stack: one completed deployment with 8 HTTP tools whose URNs
  -- (tools:http:acme:*) match the ClickHouse telemetry, feeding the
  -- Deployments, Sources, MCP, Playground, and Catalog surfaces.
  ------------------------------------------------------------------
  INSERT INTO assets (id, project_id, name, url, kind, content_type, content_length, sha256)
  VALUES (asset_id, proj_a, 'Acme Internal API', 'file://demo/acme-openapi.yaml',
          'openapiv3', 'application/x-yaml', 24576,
          'dec0de0000000000000000000000000000000000000000000000000000000001');

  INSERT INTO deployments (id, user_id, project_id, organization_id, idempotency_key, created_at)
  VALUES (deploy_id, 'user_demo_lucas', proj_a, demo_org, 'demo-seed-deployment-1',
          now() - interval '11 days');

  INSERT INTO deployment_statuses (deployment_id, status)
  VALUES (deploy_id, 'completed');

  INSERT INTO deployment_logs (event, message, deployment_id, project_id) VALUES
    ('deployment:started', 'Processing 1 OpenAPI document', deploy_id, proj_a),
    ('deployment:tools', 'Extracted 8 tools from Acme Internal API', deploy_id, proj_a),
    ('deployment:completed', 'Deployment completed successfully', deploy_id, proj_a);

  INSERT INTO deployments_openapiv3_assets (id, deployment_id, asset_id, name, slug)
  VALUES (doa_id, deploy_id, asset_id, 'Acme Internal API', 'acme');

  FOR i IN 1 .. array_length(tool_names, 1) LOOP
    INSERT INTO http_tool_definitions
      (tool_urn, project_id, deployment_id, openapiv3_document_id, name, summary,
       description, server_env_var, http_method, path, schema_version, schema,
       read_only_hint)
    VALUES (tool_urns[i], proj_a, deploy_id, doa_id, tool_names[i],
            'Acme ' || replace(tool_names[i], '_', ' '),
            'Calls the Acme internal API operation ' || tool_names[i] || '.',
            'ACME_SERVER_URL',
            CASE WHEN tool_names[i] IN ('process_refund', 'set_env') THEN 'POST' ELSE 'GET' END,
            '/' || replace(tool_names[i], '_', '/'),
            '1.0.0', '{"type":"object","properties":{}}'::jsonb,
            tool_names[i] <> 'process_refund');
  END LOOP;

  INSERT INTO toolsets (id, organization_id, project_id, name, slug, description,
                        mcp_slug, mcp_enabled, mcp_is_public)
  VALUES
    (toolset_1, demo_org, proj_a, 'Acme Support Tools', 'acme-support-tools',
     'Support workflows: logs, refunds, customer lookups.',
     'acme-demo-support', TRUE, FALSE),
    (toolset_2, demo_org, proj_a, 'Acme Ops', 'acme-ops',
     'Operational checks and deploy tooling.', NULL, TRUE, FALSE);

  -- Version = epoch seconds: the server caches toolset contents in Redis
  -- keyed by (deployment, toolset, version), and a reseed reusing version 1
  -- would keep serving the stale cached payload. A fresh version per run
  -- changes the cache key, so every reseed is immediately visible.
  INSERT INTO toolset_versions (toolset_id, version, tool_urns, resource_urns) VALUES
    (toolset_1, extract(epoch FROM now())::bigint, tool_urns, '{}'),
    (toolset_2, extract(epoch FROM now())::bigint, ARRAY[tool_urns[1], tool_urns[2], tool_urns[5], tool_urns[7], tool_urns[8]], '{}');

  ------------------------------------------------------------------
  -- Prompts (the Prompts page otherwise falls back to onboarding).
  ------------------------------------------------------------------
  -- engine must be 'mustache': the dashboard SDK's Engine enum is closed and
  -- a NULL/empty engine fails client-side validation, crashing every page
  -- that embeds a prompt template (Prompts list, source detail via
  -- tools.list). URN kind is 'prompt', not the http tools' kind.
  INSERT INTO prompt_templates (id, tool_urn, project_id, history_id, name, description, prompt, engine, kind)
  VALUES
    ('dec0de00-0000-4000-a000-00000000dd01', 'tools:prompt:acme:triage-ticket', proj_a,
     'dec0de00-0000-4000-a000-00000000dd11', 'triage-ticket',
     'Structured first response for a new support ticket.',
     'Read the customer message, identify the order id, check recent errors with search_logs, and draft a first response.',
     'mustache', 'prompt'),
    ('dec0de00-0000-4000-a000-00000000dd02', 'tools:prompt:acme:latency-investigation', proj_a,
     'dec0de00-0000-4000-a000-00000000dd12', 'latency-investigation',
     'Investigate a latency regression end to end.',
     'Fetch traces for the affected service, compare p95 against the last deploy, and summarize the likely cause.',
     'mustache', 'prompt');

  ------------------------------------------------------------------
  -- Skills: three manual skills with one version each and an open edit
  -- suggestion on support-refunds (lights up the review flow). Version shas
  -- are display-only on the read path, so sha256(content) stands in for the
  -- server's canonicalized sha.
  ------------------------------------------------------------------
  FOR i IN 1 .. 3 LOOP
    skill_id := ('dec0de00-0000-4000-a000-00000000' || '51a' || i)::uuid;
    version_id := ('dec0de00-0000-4000-a000-00000000' || '52a' || i)::uuid;

    INSERT INTO skills (id, project_id, name, display_name, summary, source_kind,
                        classification, first_seen_at, last_seen_at, seen_count)
    VALUES (skill_id, proj_a,
            CASE i WHEN 1 THEN 'support-refunds' WHEN 2 THEN 'triage-incident' ELSE 'runbook' END,
            CASE i WHEN 1 THEN 'Support Refunds' WHEN 2 THEN 'Triage Incident' ELSE 'Runbook' END,
            CASE i WHEN 1 THEN 'How to process customer refunds safely.'
                   WHEN 2 THEN 'Steps for triaging a production incident.'
                   ELSE 'General operational runbook for the Acme stack.' END,
            'manual', 'custom', now() - interval '10 days', now() - interval '1 day', 14 - i * 3);

    INSERT INTO skill_versions (id, skill_id, content, canonical_sha256, raw_sha256,
                                description, spec_valid, created_by_user_id, created_at)
    SELECT version_id, skill_id, c,
           encode(sha256(convert_to(c, 'UTF8')), 'hex'),
           encode(sha256(convert_to(c, 'UTF8')), 'hex'),
           'Initial version', TRUE, 'user_demo_lucas', now() - interval '10 days'
    FROM (SELECT CASE i WHEN 1 THEN skill_content_refunds
                        WHEN 2 THEN skill_content_triage
                        ELSE skill_content_runbook END AS c) s;

    IF i = 1 THEN
      refunds_version := version_id;
    END IF;
  END LOOP;

  INSERT INTO skill_edit_suggestions (id, project_id, skill_id, base_version_id,
                                      rationale, status, scored_session_count)
  VALUES ('dec0de00-0000-4000-a000-000000005301', proj_a,
          'dec0de00-0000-4000-a000-0000000051a1', refunds_version,
          'Sessions show agents accepting card numbers in chat before refusing; the skill should forbid it up front.',
          'open', 7);

  -- The diff MUST carry ---/+++ file headers: gitdiff.Parse yields zero file
  -- fragments for a bare hunk, skilldiff.Apply then fails and the dashboard
  -- retires the suggestion as "no longer lines up with the manifest".
  INSERT INTO skill_edit_suggestion_changes (project_id, suggestion_id, proposed_diff, rationale, position)
  VALUES
    (proj_a, 'dec0de00-0000-4000-a000-000000005301',
E'--- a/SKILL.md\n+++ b/SKILL.md\n@@ -6,4 +6,5 @@\n # Refund handling\n \n 1. Verify the order id and amount with the customer.\n+1a. If the customer pastes a card number, stop and ask them to remove it.\n 2. Use the process_refund tool with the confirmed amount.',
     'Make the PCI guardrail the first checkpoint instead of a trailing note.', 1);

  ------------------------------------------------------------------
  -- Risk policies (one per project so both overview pages count an active
  -- policy) + chats + transcripts + findings. Chats spread over the trailing
  -- ~12 days; every 7th chat carries a gitleaks finding whose match string
  -- appears inside a tool_result message. Odd chats mirror the ClickHouse
  -- Claude provenance: their tool messages carry call_demo_<i>_<k> ids that
  -- join the tool_result telemetry rows, and user messages carry
  -- demo-prompt-<i>-<turn> ids that join the api_request rows.
  ------------------------------------------------------------------
  INSERT INTO risk_policies (id, project_id, organization_id, name, policy_type,
                             sources, enabled, action, audience_type, auto_name, version)
  VALUES
    (policy_a, proj_a, demo_org, 'Acme secrets & PII policy', 'standard',
     '{regex,presidio}', TRUE, 'flag', 'everyone', TRUE, 1);

  FOR i IN 1 .. bulk_chats LOOP
    chat_id := demo.det_uuid('gram-demo-chat-' || i);
    chat_proj := proj_a;
    chat_policy := policy_a;
    owner_idx := 1 + (i % array_length(demo_user_ids, 1));
    -- 5h spacing = ~12.5 days for 60 chats: inside the session/cost MV date
    -- cutoffs and the 90-day TTLs; matches the ClickHouse formula exactly.
    chat_ts := now() - (interval '5 hours' * i) - (interval '13 minutes' * (i % 7));

    INSERT INTO chats (id, project_id, organization_id, user_id, external_user_id,
                       title, created_at, updated_at)
    VALUES (chat_id, chat_proj, demo_org, demo_user_ids[owner_idx], demo_user_emails[owner_idx],
            titles[1 + (i % array_length(titles, 1))] || ' #' || (1000 + i),
            chat_ts, chat_ts);

    -- Odd (Claude) chats use a fixed 8-message layout so ALL THREE ClickHouse
    -- api_request turns (demo-prompt-<i>-1..3) and BOTH tool_result rows
    -- (call_demo_<i>_1/2) join the transcript exactly:
    --   1 user(p1), 2 assistant, 3 tool(_1), 4 user(p2), 5 assistant,
    --   6 tool(_2), 7 user(p3), 8 assistant
    -- Even (Cursor) chats keep the varied user/assistant alternation; every
    -- 7th chat's tool message at position 3 carries the leaked fake key.
    n_msgs := CASE WHEN i % 2 = 1 THEN 8 ELSE 4 + (i % 3) * 2 END;
    flagged_msg := NULL;

    FOR m IN 1 .. n_msgs LOOP
      msg_id := demo.det_uuid('gram-demo-msg-' || i || '-' || m);

      IF (i % 7 = 0 AND m = 3) OR (i % 2 = 1 AND m IN (3, 6)) THEN
        IF i % 7 = 0 AND m = 3 THEN
          leak := 'sk_live_DEMO' || lpad(i::text, 4, '0') || 'x9q2v8w1r5t3y7u0';
          INSERT INTO chat_messages (id, chat_id, project_id, role, content, model,
                                     tool_call_id, created_at, risk_analyzed_at)
          VALUES (msg_id, chat_id, chat_proj, 'tool',
                  'payments req_id=2041 status=401 error=invalid_api_key key=' || leak,
                  'claude-sonnet-4-6', 'call_demo_' || i || '_1',
                  chat_ts + (interval '40 seconds' * m), now());
          flagged_msg := msg_id;
        ELSE
          INSERT INTO chat_messages (id, chat_id, project_id, role, content, model,
                                     tool_call_id, created_at, risk_analyzed_at)
          VALUES (msg_id, chat_id, chat_proj, 'tool',
                  '{"status":"ok","rows":' || (40 + (i * 13 + m) % 400) || ',"took_ms":' || (20 + (i * 7 + m) % 300) || '}',
                  'claude-sonnet-4-6', 'call_demo_' || i || '_' || CASE WHEN m = 3 THEN 1 ELSE 2 END,
                  chat_ts + (interval '40 seconds' * m), now());
        END IF;
      ELSE
        INSERT INTO chat_messages (id, chat_id, project_id, role, content, model,
                                   message_id,
                                   prompt_tokens, completion_tokens, total_tokens,
                                   created_at, risk_analyzed_at)
        VALUES (msg_id, chat_id, chat_proj,
                CASE WHEN i % 2 = 1 AND m IN (1, 4, 7) THEN 'user'
                     WHEN i % 2 = 1 THEN 'assistant'
                     WHEN m % 2 = 1 THEN 'user'
                     ELSE 'assistant' END,
                CASE WHEN (i % 2 = 1 AND m IN (1, 4, 7)) OR (i % 2 = 0 AND m % 2 = 1)
                     THEN questions[1 + ((i + m) % array_length(questions, 1))]
                     ELSE answers[1 + ((i + m) % array_length(answers, 1))] END,
                'claude-sonnet-4-6',
                -- Ties odd-chat user turns to the ClickHouse api_request rows
                -- (exact per-turn cost matching in the detail sheet).
                CASE WHEN i % 2 = 1 AND m IN (1, 4, 7)
                     THEN 'demo-prompt-' || i || '-' || ((m + 2) / 3)
                     ELSE NULL END,
                CASE WHEN (i % 2 = 1 AND m NOT IN (1, 4, 7)) OR (i % 2 = 0 AND m % 2 = 0)
                     THEN 1200 + (i * 37 + m * 91) % 2400 ELSE 0 END,
                CASE WHEN (i % 2 = 1 AND m NOT IN (1, 4, 7)) OR (i % 2 = 0 AND m % 2 = 0)
                     THEN 180 + (i * 53 + m * 17) % 700 ELSE 0 END,
                CASE WHEN (i % 2 = 1 AND m NOT IN (1, 4, 7)) OR (i % 2 = 0 AND m % 2 = 0)
                     THEN 1380 + (i * 37 + m * 91) % 2400 + (i * 53 + m * 17) % 700 ELSE 0 END,
                chat_ts + (interval '40 seconds' * m), now());
      END IF;
    END LOOP;

    IF flagged_msg IS NOT NULL THEN
      INSERT INTO risk_results (id, project_id, organization_id, risk_policy_id,
                                risk_policy_version, chat_message_id, source, found,
                                rule_id, description, match, start_pos, end_pos,
                                confidence, tags, created_at)
      -- Offsets computed from the actual content so drill-downs reconstruct
      -- the exact span: the fixed prefix is 58 bytes, strpos keeps it honest.
      VALUES (demo.det_uuid('gram-demo-risk-' || i), chat_proj, demo_org, chat_policy, 1,
              flagged_msg, 'gitleaks', TRUE, 'stripe-access-token',
              'Stripe live secret key found in tool output', leak,
              strpos('payments req_id=2041 status=401 error=invalid_api_key key=' || leak, leak) - 1,
              strpos('payments req_id=2041 status=401 error=invalid_api_key key=' || leak, leak) - 1 + length(leak),
              0.97, '{secret,stripe}',
              chat_ts + interval '2 minutes');
    END IF;
  END LOOP;

  ------------------------------------------------------------------
  -- Audit activity feed (org home per-project latest action + right rail).
  -- Inserted in chronological order: the feed orders by seq DESC.
  ------------------------------------------------------------------
  DELETE FROM audit_logs WHERE organization_id = demo_org;
  FOR i IN REVERSE 12 .. 1 LOOP
    INSERT INTO audit_logs (id, organization_id, project_id, actor_id, actor_type,
                            actor_display_name, action, subject_id, subject_type,
                            subject_display_name, created_at)
    VALUES (demo.det_uuid('gram-demo-audit-' || i), demo_org,
            proj_a,
            demo_user_ids[1 + (i % 6)], 'user', demo_user_names[1 + (i % 6)],
            audit_actions[1 + (i % array_length(audit_actions, 1))],
            'demo-subject-' || i, 'toolset',
            CASE WHEN i % 2 = 0 THEN 'Acme Ops' ELSE 'Acme Support Tools' END,
            now() - (interval '19 hours' * i));
  END LOOP;

  ------------------------------------------------------------------
  -- Spend rules calibrated to the seeded ClickHouse usage (~$0.37 top monthly
  -- spender) so the rules page shows one breach and one warning per rule.
  -- Events are pre-inserted because the evaluator is flag-gated; the dedupe
  -- key (rule, type, email, window_start) keeps a live evaluator idempotent.
  ------------------------------------------------------------------
  DELETE FROM spend_rule_events WHERE organization_id = demo_org;
  DELETE FROM spend_rules WHERE organization_id = demo_org;

  INSERT INTO spend_rules (id, organization_id, name, slug, description, target_expr,
                           limit_usd_cents, window_kind, warn_at_pct, action, enabled,
                           version, created_at)
  VALUES
    (rule_monthly, demo_org, 'Monthly agent budget', 'monthly-agent-budget',
     'Per-person monthly cap on agent/CLI spend.',
     'email.endsWith("@demo.getgram.ai")', 30, 'monthly', 80, 'flag', TRUE, 1,
     now() - interval '20 days'),
    -- Builder-representable condition: the rule edit sheet cannot round-trip
    -- 'email in [...]' CEL (no is-one-of operator), which left the condition
    -- blank in the UI. Department equality maps cleanly.
    (rule_weekly, demo_org, 'Support weekly cap', 'support-weekly-cap',
     'Weekly guardrail for the support rotation.',
     'department_name == "Support Engineering"',
     20, 'weekly', 75, 'block', TRUE, 1, now() - interval '15 days');

  INSERT INTO spend_rule_events (organization_id, spend_rule_id, rule_urn, event_type,
                                 user_id, email, display_name, spend_usd_cents,
                                 limit_usd_cents, window_start, window_end, created_at)
  VALUES
    (demo_org, rule_monthly, 'spend_rule:monthly-agent-budget:v1', 'breach',
     'user_demo_mateo', 'mateo@demo.getgram.ai', 'Mateo Alvarez', 31, 30,
     date_trunc('month', now()), date_trunc('month', now()) + interval '1 month',
     now() - interval '1 day'),
    (demo_org, rule_monthly, 'spend_rule:monthly-agent-budget:v1', 'warning',
     'user_demo_jonas', 'jonas@demo.getgram.ai', 'Jonas Lindqvist', 25, 30,
     date_trunc('month', now()), date_trunc('month', now()) + interval '1 month',
     now() - interval '2 days'),
    (demo_org, rule_weekly, 'spend_rule:support-weekly-cap:v1', 'breach',
     'user_demo_jonas', 'jonas@demo.getgram.ai', 'Jonas Lindqvist', 25, 20,
     date_trunc('week', now()), date_trunc('week', now()) + interval '1 week',
     now() - interval '6 hours'),
    (demo_org, rule_weekly, 'spend_rule:support-weekly-cap:v1', 'warning',
     'user_demo_amara', 'amara@demo.getgram.ai', 'Amara Okafor', 15, 20,
     date_trunc('week', now()), date_trunc('week', now()) + interval '1 week',
     now() - interval '10 hours');

  ------------------------------------------------------------------
  -- Postflight asserts: demo data landed, and nothing leaked outside
  -- the demo org.
  ------------------------------------------------------------------
  SELECT count(*) INTO chat_count FROM chats WHERE organization_id = demo_org;
  SELECT count(*) INTO finding_count
  FROM risk_results WHERE organization_id = demo_org AND risk_results.found;
  SELECT count(*) INTO member_count
  FROM organization_user_relationships WHERE organization_id = demo_org AND deleted_at IS NULL;
  SELECT count(*) INTO tool_count
  FROM http_tool_definitions WHERE project_id = proj_a AND deleted IS FALSE;

  IF chat_count < bulk_chats THEN
    RAISE EXCEPTION 'demo seed postflight: expected >= % chats, found %', bulk_chats, chat_count;
  END IF;
  IF finding_count = 0 THEN
    RAISE EXCEPTION 'demo seed postflight: no risk findings were created';
  END IF;
  IF member_count <> array_length(demo_user_ids, 1) THEN
    RAISE EXCEPTION 'demo seed postflight: expected % members, found %',
      array_length(demo_user_ids, 1), member_count;
  END IF;
  IF tool_count <> array_length(tool_names, 1) THEN
    RAISE EXCEPTION 'demo seed postflight: expected % http tools, found %',
      array_length(tool_names, 1), tool_count;
  END IF;

  SELECT count(*) INTO stray
  FROM chats WHERE project_id IN (proj_a, proj_b) AND organization_id <> demo_org;
  IF stray > 0 THEN
    RAISE EXCEPTION 'demo seed postflight: % chats on demo projects belong to another org', stray;
  END IF;

  SELECT count(*) INTO stray
  FROM chats WHERE organization_id = demo_org
    AND (user_id IS NULL OR user_id = '' OR external_user_id IS NULL);
  IF stray > 0 THEN
    RAISE EXCEPTION 'demo seed postflight: % chats have no owner (sessions would render as anonymous)', stray;
  END IF;

  -- Every table that carries both the demo org id and a project id must agree
  -- with the demo constants: a mismatch means an insert escaped its scoping.
  SELECT count(*) INTO stray FROM (
    SELECT 1 FROM deployments WHERE organization_id = demo_org AND project_id <> proj_a
    UNION ALL
    SELECT 1 FROM toolsets WHERE organization_id = demo_org AND project_id <> proj_a
    UNION ALL
    SELECT 1 FROM risk_policies WHERE organization_id = demo_org AND project_id <> proj_a
    UNION ALL
    SELECT 1 FROM risk_results WHERE organization_id = demo_org AND project_id <> proj_a
    UNION ALL
    SELECT 1 FROM audit_logs WHERE organization_id = demo_org AND project_id <> proj_a
  ) x;
  IF stray > 0 THEN
    RAISE EXCEPTION 'demo seed postflight: % demo-org rows reference non-demo projects', stray;
  END IF;

  -- Global (non-org-scoped) tables must only carry rows for the demo roster:
  -- memberships/accounts/devices reference user_demo_* ids exclusively.
  SELECT count(*) INTO stray FROM (
    SELECT 1 FROM organization_user_relationships
    WHERE organization_id = demo_org AND user_id NOT LIKE 'user\_demo\_%'
    UNION ALL
    SELECT 1 FROM user_accounts
    WHERE organization_id = demo_org AND user_id NOT LIKE 'user\_demo\_%'
    UNION ALL
    SELECT 1 FROM device_owners
    WHERE organization_id = demo_org AND linked_user_id NOT LIKE 'user\_demo\_%'
    UNION ALL
    SELECT 1 FROM directory_users
    WHERE organization_id = demo_org AND user_id NOT LIKE 'user\_demo\_%'
  ) x;
  IF stray > 0 THEN
    RAISE EXCEPTION 'demo seed postflight: % demo-org rows reference non-demo users', stray;
  END IF;

  RAISE NOTICE 'demo seed ok: % chats, % findings, % members, % tools',
    chat_count, finding_count, member_count, tool_count;
END;
$$;

SELECT demo.ensure_demo_org();
