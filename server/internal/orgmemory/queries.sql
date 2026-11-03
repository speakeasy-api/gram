-- Dashboard reads of a project's Organizational Memory. Every chat and memo
-- read applies the same eligibility as team memory search and the digest
-- rollup: a chat counts only when its summary came from the redacted
-- transcript, it is not excluded, not deleted, and not owned through a
-- personal account. Chat titles are returned here because every caller holds
-- an unrestricted chat:read grant and already sees them in Agent Sessions.

-- name: CountRememberedSessions :one
SELECT count(*)::bigint
FROM chats c
LEFT JOIN user_accounts ua ON ua.id = c.user_account_id
WHERE c.project_id = @project_id
  AND c.organization_id = @organization_id
  AND c.summary_redacted IS TRUE
  AND c.summary IS NOT NULL
  AND c.memory_excluded_at IS NULL
  AND c.deleted IS FALSE
  AND (ua.id IS NULL OR ua.account_type <> 'personal')
  AND c.summary_generated_at >= @since::timestamptz;

-- name: CountLeftOutSessions :one
-- Summarized sessions memory skips on purpose: excluded by a person, or from
-- a personal or unclassified account.
SELECT count(*)::bigint
FROM chats c
LEFT JOIN user_accounts ua ON ua.id = c.user_account_id
WHERE c.project_id = @project_id
  AND c.organization_id = @organization_id
  AND c.summary_redacted IS TRUE
  AND c.summary IS NOT NULL
  AND c.deleted IS FALSE
  AND (c.memory_excluded_at IS NOT NULL
       OR (ua.id IS NOT NULL AND (ua.account_type IS NULL OR ua.account_type = 'personal')))
  AND c.summary_generated_at >= @since::timestamptz;

-- name: CountSavedMemos :one
SELECT count(*)::bigint
FROM memory_memos m
LEFT JOIN chats c ON c.id = m.chat_id
LEFT JOIN user_accounts ua ON ua.id = c.user_account_id
WHERE m.project_id = @project_id
  AND m.organization_id = @organization_id
  AND m.deleted IS FALSE
  AND (m.chat_id IS NULL OR (c.project_id = m.project_id AND c.memory_excluded_at IS NULL AND c.deleted IS FALSE AND (ua.id IS NULL OR ua.account_type <> 'personal')))
  AND m.created_at >= @since::timestamptz;

-- name: CountMemoryRecalls :one
-- Platform MCP reads only: dashboard reads are audited under the same action
-- with a tool name that starts with "dashboard.".
SELECT count(*)::bigint
FROM audit_logs a
WHERE a.organization_id = @organization_id
  AND a.subject_type = 'team_memory'
  AND a.subject_id = @project_id::text
  AND a.action = 'team_memory:read'
  AND a.created_at >= @since::timestamptz
  AND COALESCE(a.metadata->>'tool', '') NOT LIKE 'dashboard.%';

-- name: ListDigests :many
SELECT d.id, d.level, d.period_start, d.period_end, d.content,
  cardinality(d.chat_ids)::int AS session_count,
  cardinality(d.memo_ids)::int AS memo_count,
  cardinality(d.child_digest_ids)::int AS child_count,
  d.generated_at
FROM memory_digests d
WHERE d.project_id = @project_id
  AND d.organization_id = @organization_id
  AND d.level = @level::text
  AND (sqlc.narg(before)::date IS NULL OR d.period_start < sqlc.narg(before)::date)
ORDER BY d.period_start DESC
LIMIT @row_limit;

-- name: GetDigest :one
SELECT d.id, d.level, d.period_start, d.period_end, d.content,
  d.chat_ids, d.memo_ids, d.child_digest_ids, d.generated_at
FROM memory_digests d
WHERE d.id = @id
  AND d.project_id = @project_id
  AND d.organization_id = @organization_id;

-- name: ListDigestsByID :many
SELECT d.id, d.level, d.period_start, d.period_end, d.content,
  cardinality(d.chat_ids)::int AS session_count,
  cardinality(d.memo_ids)::int AS memo_count,
  cardinality(d.child_digest_ids)::int AS child_count,
  d.generated_at
FROM memory_digests d
WHERE d.id = ANY(@ids::uuid[])
  AND d.project_id = @project_id
  AND d.organization_id = @organization_id
ORDER BY d.period_start;

-- name: ListSessionsByID :many
-- A day digest's sessions that are still eligible: one excluded or deleted
-- since the digest was written is left out.
SELECT c.id, c.title, c.summary::text AS summary, c.summary_generated_at::timestamptz AS summary_generated_at
FROM chats c
LEFT JOIN user_accounts ua ON ua.id = c.user_account_id
WHERE c.id = ANY(@ids::uuid[])
  AND c.project_id = @project_id
  AND c.organization_id = @organization_id
  AND c.summary_redacted IS TRUE
  AND c.summary IS NOT NULL
  AND c.memory_excluded_at IS NULL
  AND c.deleted IS FALSE
  AND (ua.id IS NULL OR ua.account_type <> 'personal')
ORDER BY c.summary_generated_at;

-- name: ListMemosByID :many
SELECT m.id, m.chat_id, m.content, m.created_at, m.author_user_id, m.author_agent_id,
  COALESCE(u.display_name, a.name, '')::text AS author_name
FROM memory_memos m
LEFT JOIN chats c ON c.id = m.chat_id
LEFT JOIN user_accounts ua ON ua.id = c.user_account_id
LEFT JOIN users u ON u.id = m.author_user_id
LEFT JOIN agents a ON a.id = m.author_agent_id
WHERE m.id = ANY(@ids::uuid[])
  AND m.project_id = @project_id
  AND m.organization_id = @organization_id
  AND m.deleted IS FALSE
  AND (m.chat_id IS NULL OR (c.project_id = m.project_id AND c.memory_excluded_at IS NULL AND c.deleted IS FALSE AND (ua.id IS NULL OR ua.account_type <> 'personal')))
ORDER BY m.created_at;

-- name: ListRecentSessions :many
-- Eligible sessions summarized since @since that no day digest has folded
-- yet, newest first.
SELECT c.id, c.title, c.summary::text AS summary, c.summary_generated_at::timestamptz AS summary_generated_at
FROM chats c
LEFT JOIN user_accounts ua ON ua.id = c.user_account_id
WHERE c.project_id = @project_id
  AND c.organization_id = @organization_id
  AND c.summary_redacted IS TRUE
  AND c.summary IS NOT NULL
  AND c.memory_excluded_at IS NULL
  AND c.deleted IS FALSE
  AND (ua.id IS NULL OR ua.account_type <> 'personal')
  AND c.summary_generated_at >= @since::timestamptz
  AND NOT EXISTS (
    SELECT 1 FROM memory_digests d
    WHERE d.project_id = c.project_id
      AND d.level = 'day'
      AND d.chat_ids @> ARRAY[c.id]
  )
ORDER BY c.summary_generated_at DESC
LIMIT @row_limit;

-- name: ListRecentMemos :many
-- Eligible memos saved since @since that no day digest has folded yet,
-- newest first.
SELECT m.id, m.chat_id, m.content, m.created_at, m.author_user_id, m.author_agent_id,
  COALESCE(u.display_name, a.name, '')::text AS author_name
FROM memory_memos m
LEFT JOIN chats c ON c.id = m.chat_id
LEFT JOIN user_accounts ua ON ua.id = c.user_account_id
LEFT JOIN users u ON u.id = m.author_user_id
LEFT JOIN agents a ON a.id = m.author_agent_id
WHERE m.project_id = @project_id
  AND m.organization_id = @organization_id
  AND m.deleted IS FALSE
  AND (m.chat_id IS NULL OR (c.project_id = m.project_id AND c.memory_excluded_at IS NULL AND c.deleted IS FALSE AND (ua.id IS NULL OR ua.account_type <> 'personal')))
  AND m.created_at >= @since::timestamptz
  AND NOT EXISTS (
    SELECT 1 FROM memory_digests d
    WHERE d.project_id = m.project_id
      AND d.level = 'day'
      AND d.memo_ids @> ARRAY[m.id]
  )
ORDER BY m.created_at DESC
LIMIT @row_limit;
