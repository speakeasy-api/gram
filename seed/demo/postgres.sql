-- Demo workspace seed — Postgres side.
--
-- Defines demo.ensure_demo_org(), a fully self-contained, idempotent function
-- that (re)generates the shared demo organization's data. Every write is scoped
-- to the fixed demo constants below; the function aborts before writing if any
-- preflight isolation check fails.
--
--   Local:  applied by `mise run seed:demo` (psql -f this file, which also
--           executes the function at the bottom).
--   Prod:   the function is installed once, then pg_cron runs
--           `SELECT demo.ensure_demo_org();` daily. Timestamps are relative to
--           now(), so each run regenerates a fresh trailing ~30-day window.
--
-- Constants (must match seed/demo/clickhouse.sql):
--   org id       org_gram_demo_workspace
--   project ids  dec0de00-0000-4000-a000-000000000001 (acme-support)
--                dec0de00-0000-4000-a000-000000000002 (acme-platform)
--   chat ids     md5('gram-demo-chat-' || n)::uuid
--   message ids  md5('gram-demo-msg-' || n || '-' || m)::uuid

CREATE SCHEMA IF NOT EXISTS demo;

CREATE OR REPLACE FUNCTION demo.ensure_demo_org() RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
  demo_org  CONSTANT text := 'org_gram_demo_workspace';
  proj_a    CONSTANT uuid := 'dec0de00-0000-4000-a000-000000000001';
  proj_b    CONSTANT uuid := 'dec0de00-0000-4000-a000-000000000002';
  policy_id CONSTANT uuid := 'dec0de00-0000-4000-a000-00000000f001';

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
  -- Demo employee roster. TEXT ids, all under the reserved demo email domain.
  demo_users CONSTANT text[][] := ARRAY[
    ARRAY['user_demo_amara',  'amara@demo.getgram.ai',  'Amara Okafor'],
    ARRAY['user_demo_jonas',  'jonas@demo.getgram.ai',  'Jonas Lindqvist'],
    ARRAY['user_demo_priya',  'priya@demo.getgram.ai',  'Priya Raman'],
    ARRAY['user_demo_mateo',  'mateo@demo.getgram.ai',  'Mateo Alvarez'],
    ARRAY['user_demo_hana',   'hana@demo.getgram.ai',   'Hana Sato'],
    ARRAY['user_demo_lucas',  'lucas@demo.getgram.ai',  'Lucas Meyer']
  ];

  i int;
  m int;
  n_msgs int;
  chat_id uuid;
  msg_id uuid;
  flagged_msg uuid;
  chat_proj uuid;
  chat_owner text[];
  chat_ts timestamptz;
  leak text;
  chat_count int;
  finding_count int;
  stray int;
BEGIN
  ------------------------------------------------------------------
  -- Preflight isolation asserts: refuse to run if the demo constants
  -- collide with anything that is not unambiguously the demo org.
  ------------------------------------------------------------------
  IF EXISTS (
    SELECT 1 FROM organization_metadata
    WHERE id = demo_org AND (gram_account_type <> 'demo' OR workos_id IS NOT NULL)
  ) THEN
    RAISE EXCEPTION 'demo seed aborted: org % exists but is not a demo org', demo_org;
  END IF;

  IF EXISTS (
    SELECT 1 FROM projects
    WHERE id IN (proj_a, proj_b) AND organization_id <> demo_org
  ) THEN
    RAISE EXCEPTION 'demo seed aborted: demo project id owned by another org';
  END IF;

  ------------------------------------------------------------------
  -- Org, projects, features, users. Deleting the demo projects
  -- cascades chats -> chat_messages -> risk_results and risk_policies,
  -- so the regeneration below can never leave stale rows behind.
  ------------------------------------------------------------------
  INSERT INTO organization_metadata (id, name, slug, gram_account_type, whitelisted)
  VALUES (demo_org, 'Acme Demo Workspace', 'acme-demo', 'demo', TRUE)
  ON CONFLICT (id) DO UPDATE
    SET name = EXCLUDED.name, slug = EXCLUDED.slug,
        gram_account_type = EXCLUDED.gram_account_type;

  DELETE FROM projects WHERE organization_id = demo_org;

  INSERT INTO projects (id, name, slug, organization_id) VALUES
    (proj_a, 'Acme Support',  'acme-support',  demo_org),
    (proj_b, 'Acme Platform', 'acme-platform', demo_org);

  INSERT INTO organization_features (organization_id, feature_name)
  SELECT demo_org, f
  FROM unnest(ARRAY['logs', 'tool_io_logs', 'session_capture', 'skills']) AS f
  ON CONFLICT (organization_id, feature_name) WHERE deleted IS FALSE DO NOTHING;

  FOR i IN 1 .. array_length(demo_users, 1) LOOP
    INSERT INTO users (id, email, display_name)
    VALUES (demo_users[i][1], demo_users[i][2], demo_users[i][3])
    ON CONFLICT (id) DO UPDATE
      SET email = EXCLUDED.email, display_name = EXCLUDED.display_name;
  END LOOP;

  ------------------------------------------------------------------
  -- Risk policy + chats + transcripts + findings.
  -- Chats spread over the trailing ~12 days; every 7th chat carries a
  -- gitleaks finding whose match string appears inside a tool_result
  -- message, so the risk pages and the transcript highlight both work.
  ------------------------------------------------------------------
  INSERT INTO risk_policies (
    id, project_id, organization_id, name, policy_type, sources,
    enabled, action, audience_type, auto_name, version
  ) VALUES (
    policy_id, proj_a, demo_org, 'Acme secrets & PII policy', 'standard',
    '{regex,presidio}', TRUE, 'flag', 'everyone', TRUE, 1
  );

  FOR i IN 1 .. bulk_chats LOOP
    chat_id := md5('gram-demo-chat-' || i)::uuid;
    chat_proj := CASE WHEN i % 3 = 0 THEN proj_b ELSE proj_a END;
    chat_owner := demo_users[1 + (i % array_length(demo_users, 1))];
    -- 5h spacing = ~12.5 days for 60 chats. Must stay well inside the ~90-day
    -- TTLs; also matches the ClickHouse formula in clickhouse.sql exactly.
    chat_ts := now() - (interval '5 hours' * i) - (interval '13 minutes' * (i % 7));

    INSERT INTO chats (id, project_id, organization_id, user_id, external_user_id,
                       title, created_at, updated_at)
    VALUES (chat_id, chat_proj, demo_org, chat_owner[1], chat_owner[2],
            titles[1 + (i % array_length(titles, 1))] || ' #' || (1000 + i),
            chat_ts, chat_ts);

    n_msgs := 4 + (i % 3) * 2;
    flagged_msg := NULL;

    FOR m IN 1 .. n_msgs LOOP
      msg_id := md5('gram-demo-msg-' || i || '-' || m)::uuid;

      IF i % 7 = 0 AND m = 3 THEN
        -- tool_result carrying a leaked (fake) key -> risk finding target.
        leak := 'sk_live_DEMO' || lpad(i::text, 4, '0') || 'x9q2v8w1r5t3y7u0';
        INSERT INTO chat_messages (id, chat_id, project_id, role, content, model,
                                   tool_call_id, created_at, risk_analyzed_at)
        VALUES (msg_id, chat_id, chat_proj, 'tool',
                'payments req_id=2041 status=401 error=invalid_api_key key=' || leak,
                'claude-sonnet-4-6', 'call_demo_' || i,
                chat_ts + (interval '40 seconds' * m), now());
        flagged_msg := msg_id;
      ELSE
        INSERT INTO chat_messages (id, chat_id, project_id, role, content, model,
                                   prompt_tokens, completion_tokens, total_tokens,
                                   created_at, risk_analyzed_at)
        VALUES (msg_id, chat_id, chat_proj,
                CASE WHEN m % 2 = 1 THEN 'user' ELSE 'assistant' END,
                CASE WHEN m % 2 = 1
                     THEN questions[1 + ((i + m) % array_length(questions, 1))]
                     ELSE answers[1 + ((i + m) % array_length(answers, 1))] END,
                'claude-sonnet-4-6',
                CASE WHEN m % 2 = 0 THEN 1200 + (i * 37 + m * 91) % 2400 ELSE 0 END,
                CASE WHEN m % 2 = 0 THEN 180 + (i * 53 + m * 17) % 700 ELSE 0 END,
                CASE WHEN m % 2 = 0 THEN 1380 + (i * 37 + m * 91) % 2400 + (i * 53 + m * 17) % 700 ELSE 0 END,
                chat_ts + (interval '40 seconds' * m), now());
      END IF;
    END LOOP;

    IF flagged_msg IS NOT NULL THEN
      INSERT INTO risk_results (id, project_id, organization_id, risk_policy_id,
                                risk_policy_version, chat_message_id, source, found,
                                rule_id, description, match, start_pos, end_pos,
                                confidence, tags, created_at)
      VALUES (md5('gram-demo-risk-' || i)::uuid, chat_proj, demo_org, policy_id, 1,
              flagged_msg, 'gitleaks', TRUE, 'stripe-access-token',
              'Stripe live secret key found in tool output', leak,
              55, 55 + length(leak), 0.97, '{secret,stripe}',
              chat_ts + interval '2 minutes');
    END IF;
  END LOOP;

  ------------------------------------------------------------------
  -- Postflight asserts: demo data landed, and nothing leaked outside
  -- the demo org.
  ------------------------------------------------------------------
  SELECT count(*) INTO chat_count FROM chats WHERE organization_id = demo_org;
  SELECT count(*) INTO finding_count
  FROM risk_results WHERE organization_id = demo_org AND risk_results.found;

  IF chat_count < bulk_chats THEN
    RAISE EXCEPTION 'demo seed postflight: expected >= % chats, found %', bulk_chats, chat_count;
  END IF;
  IF finding_count = 0 THEN
    RAISE EXCEPTION 'demo seed postflight: no risk findings were created';
  END IF;

  SELECT count(*) INTO stray
  FROM chats WHERE project_id IN (proj_a, proj_b) AND organization_id <> demo_org;
  IF stray > 0 THEN
    RAISE EXCEPTION 'demo seed postflight: % chats on demo projects belong to another org', stray;
  END IF;

  RAISE NOTICE 'demo seed ok: % chats, % findings', chat_count, finding_count;
END;
$$;

SELECT demo.ensure_demo_org();
