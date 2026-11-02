-- Team memory (search_team_memory, remember). Every read and write carries
-- organization and project in its row filter, so a caller can never reach
-- another project's memory through a guessed id.

-- name: GetTeamMemoryChat :one
-- Confirms a chat a memo cites lives in this project and is not deleted.
SELECT c.id
FROM chats c
JOIN projects p ON p.id = c.project_id AND p.organization_id = @organization_id AND p.deleted IS FALSE
WHERE c.id = @chat_id AND c.project_id = @project_id AND c.organization_id = @organization_id
  AND c.deleted IS FALSE;

-- name: InsertMemoryMemo :one
INSERT INTO memory_memos (organization_id, project_id, chat_id, author_user_id, content)
VALUES (@organization_id, @project_id, sqlc.narg('chat_id'), @author_user_id, @content)
RETURNING id, created_at;

-- name: SearchTeamMemory :many
-- Ranked full-text search over one project's team memory inside a time
-- window. Chat summaries count only when the background job wrote them from
-- the masked transcript, the chat is live, not excluded from memory and not a
-- personal-account session (an unclassified account is treated as personal).
-- Memos saved from a chat that fails those checks are hidden with it. Text
-- over 64 KiB is never returned. Digest periods are UTC dates, so they are
-- compared as UTC midnights whatever the session time zone is. Ranks are
-- normalized by document length so long digests do not crowd out short
-- memos, and memos, which a person saved on purpose, count double.
WITH q AS (SELECT websearch_to_tsquery('english', @query::text) AS query)
SELECT hits.kind::text AS kind, hits.id, hits.chat_id, hits.body::text AS body,
       hits.author_user_id::text AS author_user_id, hits.level::text AS level, hits.period_start,
       hits.at::timestamptz AS at, hits.rank::real AS rank
FROM (
  SELECT 'chat' AS kind, c.id, NULL::uuid AS chat_id,
         c.summary AS body, '' AS author_user_id, '' AS level, NULL::date AS period_start,
         COALESCE(c.summary_generated_at, c.updated_at) AS at,
         ts_rank(to_tsvector('english', c.summary), q.query, 1) AS rank
  FROM chats c
  CROSS JOIN q
  JOIN projects p ON p.id = c.project_id AND p.organization_id = @organization_id AND p.deleted IS FALSE
  LEFT JOIN user_accounts ua ON ua.id = c.user_account_id
  WHERE @include_chats::boolean
    AND c.project_id = @project_id AND c.organization_id = @organization_id
    AND c.deleted IS FALSE AND c.summary_redacted AND c.memory_excluded_at IS NULL
    AND c.summary IS NOT NULL AND octet_length(c.summary) <= 65536
    AND (ua.id IS NULL OR ua.account_type <> 'personal')
    AND COALESCE(c.summary_generated_at, c.updated_at) >= @from_time AND COALESCE(c.summary_generated_at, c.updated_at) < @to_time
    AND to_tsvector('english', c.summary) @@ q.query
  UNION ALL
  SELECT 'memo', m.id, m.chat_id, m.content, COALESCE(m.author_user_id, ''), '', NULL::date,
         m.created_at, 2 * ts_rank(m.search_vector, q.query, 1)
  FROM memory_memos m
  CROSS JOIN q
  JOIN projects p ON p.id = m.project_id AND p.organization_id = @organization_id AND p.deleted IS FALSE
  LEFT JOIN chats mc ON mc.id = m.chat_id
  LEFT JOIN user_accounts mua ON mua.id = mc.user_account_id
  WHERE @include_memos::boolean
    AND m.project_id = @project_id AND m.organization_id = @organization_id AND m.deleted IS FALSE
    AND m.created_at >= @from_time AND m.created_at < @to_time
    AND m.search_vector @@ q.query
    AND (m.chat_id IS NULL OR (mc.project_id = m.project_id AND mc.deleted IS FALSE AND mc.memory_excluded_at IS NULL
         AND (mua.id IS NULL OR mua.account_type <> 'personal')))
  UNION ALL
  SELECT 'digest', d.id, NULL::uuid, d.content, '', d.level, d.period_start,
         d.period_start::timestamp AT TIME ZONE 'UTC', ts_rank(d.search_vector, q.query, 1)
  FROM memory_digests d
  CROSS JOIN q
  JOIN projects p ON p.id = d.project_id AND p.organization_id = @organization_id AND p.deleted IS FALSE
  WHERE @include_digests::boolean
    AND d.project_id = @project_id AND d.organization_id = @organization_id
    AND octet_length(d.content) <= 65536
    AND d.period_end::timestamp AT TIME ZONE 'UTC' > @from_time AND d.period_start::timestamp AT TIME ZONE 'UTC' < @to_time
    AND d.search_vector @@ q.query
) hits
ORDER BY hits.rank DESC, hits.at DESC, hits.id
LIMIT @row_limit::integer;
