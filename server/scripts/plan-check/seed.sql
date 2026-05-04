-- Seed enough rows in growth tables so the planner produces a realistic
-- plan against representative statistics. Without this every table is
-- empty and Postgres prefers Seq Scan regardless of indexing.
--
-- Row counts target the smallest size where a missing index on a hot
-- column becomes visible in EXPLAIN. Bump if a query slips through.

BEGIN;

INSERT INTO organization_metadata (id, name, slug, gram_account_type)
VALUES ('plan-check-org', 'plan-check', 'plan-check', 'free')
ON CONFLICT DO NOTHING;

INSERT INTO projects (id, organization_id, slug, name)
VALUES (
  '00000000-0000-0000-0000-000000000001'::uuid,
  'plan-check-org',
  'plan-check',
  'plan-check'
)
ON CONFLICT DO NOTHING;

INSERT INTO chats (id, project_id, organization_id)
VALUES (
  '00000000-0000-0000-0000-000000000003'::uuid,
  '00000000-0000-0000-0000-000000000001'::uuid,
  'plan-check-org'
)
ON CONFLICT DO NOTHING;

INSERT INTO risk_policies (id, project_id, organization_id, name, sources, version, enabled)
VALUES (
  '00000000-0000-0000-0000-000000000002'::uuid,
  '00000000-0000-0000-0000-000000000001'::uuid,
  'plan-check-org',
  'plan-check',
  ARRAY['content']::TEXT[],
  1,
  TRUE
)
ON CONFLICT DO NOTHING;

-- chat_messages: one hot project, mix of analyzed/unanalyzed.
INSERT INTO chat_messages (chat_id, project_id, role, content)
SELECT '00000000-0000-0000-0000-000000000003'::uuid,
       '00000000-0000-0000-0000-000000000001'::uuid,
       'user',
       'plan-check seed message ' || g
FROM generate_series(1, 50000) AS g;

-- risk_results: ~80% coverage so the anti-join has work to do.
INSERT INTO risk_results (
    project_id, organization_id, risk_policy_id, risk_policy_version,
    chat_message_id, source, found
)
SELECT cm.project_id,
       'plan-check-org',
       '00000000-0000-0000-0000-000000000002'::uuid,
       1,
       cm.id,
       'content',
       FALSE
FROM chat_messages cm
WHERE cm.project_id = '00000000-0000-0000-0000-000000000001'::uuid
ORDER BY cm.seq
LIMIT 40000;

ANALYZE chat_messages;
ANALYZE risk_results;
ANALYZE risk_policies;

COMMIT;
