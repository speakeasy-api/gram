-- Seed enough rows in growth tables so the planner produces a realistic
-- plan against representative statistics. Without this every table is
-- empty and Postgres prefers Seq Scan regardless of indexing.
--
-- Shape: one hot project with a small slice (1k chat_messages, ~80%
-- analyzed) plus many distractor projects holding the bulk of the
-- table (~49k chat_messages). This gives WHERE project_id = $hot a
-- selective predicate so the planner can pick the partial index over
-- a Seq Scan when the index exists.

BEGIN;

INSERT INTO organization_metadata (id, name, slug, gram_account_type)
VALUES ('plan-check-org', 'plan-check', 'plan-check', 'free')
ON CONFLICT DO NOTHING;

-- Hot project (the one queries target).
INSERT INTO projects (id, organization_id, slug, name)
VALUES (
  '00000000-0000-0000-0000-000000000001'::uuid,
  'plan-check-org',
  'plan-check-hot',
  'plan-check-hot'
)
ON CONFLICT DO NOTHING;

-- 50 distractor projects.
INSERT INTO projects (id, organization_id, slug, name)
SELECT ('00000000-0000-0000-0000-' || lpad((1000 + g)::text, 12, '0'))::uuid,
       'plan-check-org',
       'plan-check-distractor-' || g,
       'plan-check-distractor-' || g
FROM generate_series(1, 50) AS g
ON CONFLICT DO NOTHING;

-- Hot chat for the hot project.
INSERT INTO chats (id, project_id, organization_id)
VALUES (
  '00000000-0000-0000-0001-000000000001'::uuid,
  '00000000-0000-0000-0000-000000000001'::uuid,
  'plan-check-org'
)
ON CONFLICT DO NOTHING;

-- One chat per distractor project.
INSERT INTO chats (id, project_id, organization_id)
SELECT ('00000000-0000-0000-0001-' || lpad((1000 + g)::text, 12, '0'))::uuid,
       ('00000000-0000-0000-0000-' || lpad((1000 + g)::text, 12, '0'))::uuid,
       'plan-check-org'
FROM generate_series(1, 50) AS g
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

-- Hot project: 1000 messages.
INSERT INTO chat_messages (chat_id, project_id, role, content)
SELECT '00000000-0000-0000-0001-000000000001'::uuid,
       '00000000-0000-0000-0000-000000000001'::uuid,
       'user',
       'plan-check hot message ' || g
FROM generate_series(1, 1000) AS g;

-- Distractor messages: ~49k spread across 50 distractor chats (~980 each).
INSERT INTO chat_messages (chat_id, project_id, role, content)
SELECT c.id, c.project_id, 'user', 'distractor ' || g
FROM chats c,
     LATERAL generate_series(1, 980) AS g
WHERE c.project_id <> '00000000-0000-0000-0000-000000000001'::uuid;

-- risk_results: cover ~80% of the hot project's messages so the anti-join
-- has work to do but FetchUnanalyzed still returns a non-empty batch.
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
LIMIT 800;

-- Distractor risk_results: bulk up the table so a Seq Scan on risk_results
-- becomes more expensive than an index lookup for the hot project's slice.
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
WHERE cm.project_id <> '00000000-0000-0000-0000-000000000001'::uuid;

ANALYZE chat_messages;
ANALYZE risk_results;
ANALYZE risk_policies;
ANALYZE chats;
ANALYZE projects;

COMMIT;
