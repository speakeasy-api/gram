-- Team memory digest rollup. Every read filters on the same eligibility as
-- team memory search: a chat contributes only when its summary came from
-- the redacted transcript, it is not excluded from memory, not deleted, and
-- not owned through a personal account. An unclassified account
-- (account_type NULL) fails closed like session recall.

-- name: ListMemoryOrganizations :many
-- Organizations with redacted summaries or memos since @since. The job
-- evaluates the rollout flag for these, then lists candidates only for
-- enabled ones, so orgs with the flag off never crowd a batch.
SELECT DISTINCT c.organization_id
FROM chats c
WHERE c.summary_redacted IS TRUE
  AND c.summary_generated_at >= @since::timestamptz
UNION
SELECT DISTINCT m.organization_id
FROM memory_memos m
WHERE m.deleted IS FALSE
  AND m.created_at >= @since::timestamptz;

-- name: ListDayDigestCandidates :many
-- Project-days in [@since, @until) with eligible chat summaries or memos and
-- no day digest yet. Chats are bucketed by when their summary was written,
-- so a summary that lands after its day closed goes into the next open day.
-- An empty @project_ids means every project; the memory eval passes its own.
WITH sources AS (
  SELECT c.organization_id, c.project_id, (c.summary_generated_at AT TIME ZONE 'UTC')::date AS day
  FROM chats c
  JOIN projects p ON p.id = c.project_id AND p.deleted IS FALSE
  LEFT JOIN user_accounts ua ON ua.id = c.user_account_id
  WHERE c.summary_redacted IS TRUE
    AND c.summary IS NOT NULL
    AND c.memory_excluded_at IS NULL
    AND c.deleted IS FALSE
    AND (ua.id IS NULL OR ua.account_type <> 'personal')
    AND c.organization_id = ANY(@organization_ids::text[])
    AND (cardinality(@project_ids::uuid[]) = 0 OR c.project_id = ANY(@project_ids::uuid[]))
    AND c.summary_generated_at >= @since::timestamptz
    AND c.summary_generated_at < @until::timestamptz
  UNION
  SELECT m.organization_id, m.project_id, (m.created_at AT TIME ZONE 'UTC')::date AS day
  FROM memory_memos m
  JOIN projects p ON p.id = m.project_id AND p.deleted IS FALSE
  LEFT JOIN chats c ON c.id = m.chat_id
  LEFT JOIN user_accounts ua ON ua.id = c.user_account_id
  WHERE m.deleted IS FALSE
    AND (c.id IS NULL OR (c.memory_excluded_at IS NULL AND c.deleted IS FALSE AND (ua.id IS NULL OR ua.account_type <> 'personal')))
    AND m.organization_id = ANY(@organization_ids::text[])
    AND (cardinality(@project_ids::uuid[]) = 0 OR m.project_id = ANY(@project_ids::uuid[]))
    AND m.created_at >= @since::timestamptz
    AND m.created_at < @until::timestamptz
)
SELECT s.organization_id, s.project_id, s.day::date AS period_start
FROM sources s
WHERE NOT EXISTS (
  SELECT 1 FROM memory_digests d
  WHERE d.project_id = s.project_id
    AND d.level = 'day'
    AND d.period_start = s.day
)
ORDER BY s.day, s.project_id
LIMIT @row_limit;

-- name: DeleteStaleDayDigests :many
-- Day digests that folded a chat or memo which is no longer eligible:
-- excluded from memory, deleted, moved to a personal account, or gone.
-- Their text may still carry what was removed, so they are deleted and the
-- job rebuilds the day from the sources still eligible. Parents that folded
-- them go next, through DeleteOrphanedRollups. It runs for every
-- organization, flag or not: removal from memory must not depend on rollout.
DELETE FROM memory_digests d
WHERE d.level = 'day'
  AND (
    cardinality(d.chat_ids) > (
      SELECT count(*)
      FROM chats c
      LEFT JOIN user_accounts ua ON ua.id = c.user_account_id
      WHERE c.id = ANY(d.chat_ids)
        AND c.summary_redacted IS TRUE
        AND c.summary IS NOT NULL
        AND c.memory_excluded_at IS NULL
        AND c.deleted IS FALSE
        AND (ua.id IS NULL OR ua.account_type <> 'personal')
    )
    OR cardinality(d.memo_ids) > (
      SELECT count(*)
      FROM memory_memos m
      LEFT JOIN chats c ON c.id = m.chat_id
      LEFT JOIN user_accounts ua ON ua.id = c.user_account_id
      WHERE m.id = ANY(d.memo_ids)
        AND m.deleted IS FALSE
        AND (c.id IS NULL OR (c.memory_excluded_at IS NULL AND c.deleted IS FALSE AND (ua.id IS NULL OR ua.account_type <> 'personal')))
    )
  )
RETURNING d.id;

-- name: DeleteOrphanedRollups :many
-- Week or month digests that folded a child digest which no longer exists,
-- because DeleteStaleDayDigests (or an earlier pass of this query) removed
-- it. They are rebuilt from the replacement children.
DELETE FROM memory_digests d
WHERE d.level = @level::text
  AND cardinality(d.child_digest_ids) > (
    SELECT count(*) FROM memory_digests c WHERE c.id = ANY(d.child_digest_ids)
  )
RETURNING d.id;

-- name: ListRollupDigestCandidates :many
-- Parent periods (week or month) that have child digests starting in
-- [@since, @until) and no digest of their own yet. A week rolls into the
-- month its Monday falls in, which date_trunc('month', period_start) gives.
-- An empty @project_ids means every project.
SELECT d.organization_id, d.project_id, date_trunc(@parent_level::text, d.period_start::timestamp)::date AS period_start
FROM memory_digests d
WHERE d.level = @child_level::text
  AND d.organization_id = ANY(@organization_ids::text[])
  AND (cardinality(@project_ids::uuid[]) = 0 OR d.project_id = ANY(@project_ids::uuid[]))
  AND d.period_start >= @since::date
  AND d.period_start < @until::date
  AND EXISTS (SELECT 1 FROM projects p WHERE p.id = d.project_id AND p.deleted IS FALSE)
  AND NOT EXISTS (
    SELECT 1 FROM memory_digests p
    WHERE p.project_id = d.project_id
      AND p.level = @parent_level::text
      AND p.period_start = date_trunc(@parent_level::text, d.period_start::timestamp)::date
  )
GROUP BY d.organization_id, d.project_id, date_trunc(@parent_level::text, d.period_start::timestamp)::date
ORDER BY 3, d.project_id
LIMIT @row_limit;

-- name: ListDayDigestChats :many
-- Newest first so @row_limit keeps a busy day's latest chats; the caller
-- restores chronological order.
SELECT c.id, c.summary::text AS summary, c.summary_generated_at::timestamptz AS summary_generated_at
FROM chats c
LEFT JOIN user_accounts ua ON ua.id = c.user_account_id
WHERE c.project_id = @project_id
  AND c.organization_id = @organization_id
  AND c.summary_redacted IS TRUE
  AND c.summary IS NOT NULL
  AND c.memory_excluded_at IS NULL
  AND c.deleted IS FALSE
  AND (ua.id IS NULL OR ua.account_type <> 'personal')
  AND c.summary_generated_at >= @period_start::timestamptz
  AND c.summary_generated_at < @period_end::timestamptz
ORDER BY c.summary_generated_at DESC
LIMIT @row_limit;

-- name: ListDayDigestMemos :many
-- Memo authors stay out of digest prompts: digests are team-wide and only
-- need what was decided, not who saved it.
SELECT m.id, m.content, m.created_at
FROM memory_memos m
LEFT JOIN chats c ON c.id = m.chat_id
LEFT JOIN user_accounts ua ON ua.id = c.user_account_id
WHERE m.project_id = @project_id
  AND m.organization_id = @organization_id
  AND m.deleted IS FALSE
  AND (c.id IS NULL OR (c.memory_excluded_at IS NULL AND c.deleted IS FALSE AND (ua.id IS NULL OR ua.account_type <> 'personal')))
  AND m.created_at >= @period_start::timestamptz
  AND m.created_at < @period_end::timestamptz
ORDER BY m.created_at DESC
LIMIT @row_limit;

-- name: ListChildDigests :many
SELECT d.id, d.period_start, d.content
FROM memory_digests d
WHERE d.project_id = @project_id
  AND d.organization_id = @organization_id
  AND d.level = @level::text
  AND d.period_start >= @since::date
  AND d.period_start < @until::date
ORDER BY d.period_start;

-- name: InsertDigest :one
-- Digests are written once. A concurrent or repeated run for the same
-- period inserts nothing and returns no row.
INSERT INTO memory_digests (
  organization_id, project_id, level, period_start, period_end, content,
  chat_ids, memo_ids, child_digest_ids, model, input_tokens, output_tokens
) VALUES (
  @organization_id, @project_id, @level, @period_start, @period_end, @content,
  @chat_ids::uuid[], @memo_ids::uuid[], @child_digest_ids::uuid[], @model, @input_tokens, @output_tokens
)
ON CONFLICT (project_id, level, period_start) DO NOTHING
RETURNING id;

-- name: ListRecallDigests :many
-- Every digest of the project whose period starts at or after @since. The
-- caller drops digests a parent already folded.
SELECT d.id, d.level, d.period_start, d.content, d.chat_ids, d.memo_ids, d.child_digest_ids
FROM memory_digests d
WHERE d.project_id = @project_id
  AND d.organization_id = @organization_id
  AND d.period_start >= @since::date
ORDER BY d.period_start, d.level;

-- name: ListRecallChats :many
-- Eligible chats summarized since @since that no day digest has folded yet,
-- newest first so @row_limit keeps the most recent.
SELECT c.id, c.summary::text AS summary, c.summary_generated_at::timestamptz AS summary_generated_at
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

-- name: ListRecallMemos :many
-- Eligible memos saved since @since that no day digest has folded yet:
-- today's memos, and any a capped day digest left out. Newest first so
-- @row_limit keeps the most recent.
SELECT m.id, m.content, m.created_at
FROM memory_memos m
LEFT JOIN chats c ON c.id = m.chat_id
LEFT JOIN user_accounts ua ON ua.id = c.user_account_id
WHERE m.project_id = @project_id
  AND m.organization_id = @organization_id
  AND m.deleted IS FALSE
  AND (c.id IS NULL OR (c.memory_excluded_at IS NULL AND c.deleted IS FALSE AND (ua.id IS NULL OR ua.account_type <> 'personal')))
  AND m.created_at >= @since::timestamptz
  AND NOT EXISTS (
    SELECT 1 FROM memory_digests d
    WHERE d.project_id = m.project_id
      AND d.level = 'day'
      AND d.memo_ids @> ARRAY[m.id]
  )
ORDER BY m.created_at DESC
LIMIT @row_limit;

-- name: GetDigest :one
SELECT d.id, d.level, d.period_start, d.period_end, d.content, d.chat_ids, d.memo_ids, d.child_digest_ids
FROM memory_digests d
WHERE d.id = @id
  AND d.project_id = @project_id
  AND d.organization_id = @organization_id;

-- name: ListDigestsByID :many
SELECT d.id, d.level, d.period_start, d.content
FROM memory_digests d
WHERE d.id = ANY(@ids::uuid[])
  AND d.project_id = @project_id
  AND d.organization_id = @organization_id
ORDER BY d.period_start;

-- name: ListZoomChats :many
-- A day digest's chats that are still eligible: a chat excluded or deleted
-- since the digest was written is left out.
SELECT c.id, c.summary::text AS summary, c.summary_generated_at::timestamptz AS summary_generated_at
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

-- name: ListZoomMemos :many
SELECT m.id, m.chat_id, m.content, m.created_at, COALESCE(u.display_name, '')::text AS author_name
FROM memory_memos m
LEFT JOIN chats c ON c.id = m.chat_id
LEFT JOIN user_accounts ua ON ua.id = c.user_account_id
LEFT JOIN users u ON u.id = m.author_user_id
WHERE m.id = ANY(@ids::uuid[])
  AND m.project_id = @project_id
  AND m.organization_id = @organization_id
  AND m.deleted IS FALSE
  AND (c.id IS NULL OR (c.memory_excluded_at IS NULL AND c.deleted IS FALSE AND (ua.id IS NULL OR ua.account_type <> 'personal')))
ORDER BY m.created_at;

-- name: DeleteProjectDigests :execrows
-- Every digest of one organization's projects. Only the team memory eval
-- uses it: it rebuilds a synthetic corpus's digests with each model it
-- compares, so digests written by an earlier model must not survive.
DELETE FROM memory_digests
WHERE organization_id = @organization_id
  AND project_id = ANY(@project_ids::uuid[]);
