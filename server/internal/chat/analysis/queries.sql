-- Chat analysis pipeline queries. The pipeline mirrors skill efficacy
-- (server/internal/skills/queries.sql): a durable queue of (chat, judge)
-- scoring units is enqueued from the chats table, reserved against the
-- organization's daily budget under an advisory lock, judged by an LLM, and
-- marked scored once the verdict is in ClickHouse.

-- name: GetChatAnalysisSettingsForProject :many
-- One row per configured judge for the project's organization; the LEFT JOIN
-- keeps the organization id flowing back even when no judge is configured, so
-- a caller can tell "project gone" (no rows) from "nothing enabled" (one row
-- with null settings columns). A judge with no row is off.
SELECT
  p.organization_id,
  s.judge,
  s.enabled,
  s.daily_cap
FROM projects p
LEFT JOIN chat_analysis_settings s ON s.organization_id = p.organization_id
WHERE p.id = @project_id::uuid
  AND p.deleted IS FALSE;

-- name: UpsertChatAnalysisSettingsForJudge :one
-- Writes one judge's switch and budget for the project's organization: the
-- organization is derived from a live project, so a deleted or unknown project
-- writes nothing.
INSERT INTO chat_analysis_settings (
  organization_id,
  judge,
  enabled,
  daily_cap
)
SELECT
  p.organization_id,
  @judge::text,
  @enabled::boolean,
  @daily_cap::integer
FROM projects p
WHERE p.id = @project_id::uuid
  AND p.deleted IS FALSE
ON CONFLICT (organization_id, judge) DO UPDATE
SET enabled = excluded.enabled,
    daily_cap = excluded.daily_cap,
    updated_at = clock_timestamp()
RETURNING *;

-- name: LockOrganizationChatAnalysisBudget :exec
-- Settings updates share the reservation lock so their audit snapshots and the
-- budgets observed by reservations both describe committed state.
SELECT pg_advisory_xact_lock(hashtextextended('chat-analysis:' || @organization_id::text, 0));

-- name: GetChatAnalysisSettingForOrganizationJudge :one
SELECT *
FROM chat_analysis_settings
WHERE organization_id = @organization_id
  AND judge = @judge;

-- name: UpsertChatAnalysisSettingForOrganizationJudge :one
INSERT INTO chat_analysis_settings (
  organization_id,
  judge,
  enabled,
  daily_cap
)
VALUES (
  @organization_id,
  @judge,
  @enabled,
  @daily_cap
)
ON CONFLICT (organization_id, judge) DO UPDATE
SET enabled = excluded.enabled,
    daily_cap = excluded.daily_cap,
    updated_at = clock_timestamp()
RETURNING *;

-- name: LockProjectOrganizationChatAnalysisBudget :exec
-- First statement of the reservation transaction: serialises counting and
-- reserving per organization, entered through the project, and held to commit.
-- The key space is distinct from the skill efficacy lock so the two pipelines
-- never serialise each other.
SELECT pg_advisory_xact_lock(hashtextextended('chat-analysis:' || p.organization_id, 0))
FROM projects p
WHERE p.id = @project_id::uuid;

-- name: ListChatAnalysisCandidateChats :many
-- One enqueue page: live chats holding at least one stored message, walked
-- oldest-first on the immutable (created_at, id) keyset within the enqueue
-- lookback. last_message_at is the chat's latest transcript write, which the
-- enqueue stores as observed_at so the reservation's recent-first order and
-- quiet check both describe transcript activity rather than chat creation.
SELECT
  c.id,
  c.organization_id,
  c.created_at,
  latest.last_message_at::timestamptz AS last_message_at
FROM chats c
CROSS JOIN LATERAL (
  SELECT max(cm.created_at) AS last_message_at
  FROM chat_messages cm
  WHERE cm.chat_id = c.id
    AND (cm.project_id IS NULL OR cm.project_id = c.project_id)
) latest
WHERE c.project_id = @project_id
  AND c.deleted IS FALSE
  AND c.created_at > now() - @lookback::interval
  AND latest.last_message_at IS NOT NULL
  AND (
    sqlc.narg('cursor_created_at')::timestamptz IS NULL
    OR (c.created_at, c.id) > (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::uuid)
  )
ORDER BY c.created_at ASC, c.id ASC
LIMIT @page_size;

-- name: EnqueueChatAnalysisEvaluations :exec
-- Idempotent enqueue: one row per (chat, judge) unit, expanded from the page's
-- chats crossed with the judge roster. Scored and failed units are left alone;
-- a pending unit refreshes observed_at so a resumed session keeps its place in
-- the recent-first reservation order and its quiet clock restarts.
INSERT INTO chat_analysis_evaluations (
  organization_id,
  project_id,
  chat_id,
  session_id,
  judge,
  observed_at
)
SELECT
  unit.organization_id,
  @project_id,
  unit.chat_id,
  unit.session_id,
  j.judge,
  unit.observed_at
FROM (
  SELECT
    unnest(@organization_ids::text[]) AS organization_id,
    unnest(@chat_ids::uuid[]) AS chat_id,
    unnest(@session_ids::text[]) AS session_id,
    unnest(@observed_ats::timestamptz[]) AS observed_at
) unit
CROSS JOIN unnest(@judges::text[]) AS j(judge)
ON CONFLICT (project_id, chat_id, judge) DO UPDATE
SET observed_at = GREATEST(chat_analysis_evaluations.observed_at, excluded.observed_at),
    updated_at = clock_timestamp()
WHERE chat_analysis_evaluations.state = 'pending';

-- name: CountChatAnalysisJudgeSpendForProject :many
-- Per-judge spend for the day, organization-grained and entered through the
-- project: counts every project in the organization. Judges with no spend are
-- simply absent.
SELECT e.judge, count(*) AS spend
FROM chat_analysis_evaluations e
JOIN projects p ON p.organization_id = e.organization_id
WHERE p.id = @project_id::uuid
  AND e.reserved_on = @reserved_on::date
  AND e.state IN ('reserved', 'scored')
GROUP BY e.judge;

-- name: ListPendingChatAnalysisEvaluations :many
-- Recent-first keyset page over a project's pending evaluations, ordered on the
-- unique (observed_at, id) key. A null cursor starts at the head of the queue.
-- No row lock is taken: pending -> reserved is written only by the reservation
-- pass, and every such pass holds the same per-organization advisory lock for
-- its whole transaction.
--
-- The liveness recheck sits before paging so a unit whose project or chat was
-- deleted after enqueue is never a candidate and never spends the budget, and
-- the quiet check is live: a session that resumed after enqueue stays out of
-- the page until its transcript has gone quiet again.
SELECT
  e.id,
  e.organization_id,
  e.project_id,
  e.chat_id,
  e.session_id,
  e.judge,
  e.observed_at,
  e.state,
  e.reserved_on,
  e.attempts,
  e.last_error,
  e.scored_at,
  e.created_at,
  e.updated_at
FROM chat_analysis_evaluations e
JOIN projects p
  ON p.id = e.project_id
  AND p.deleted IS FALSE
JOIN chats c
  ON c.project_id = e.project_id
  AND c.id = e.chat_id
  AND c.deleted IS FALSE
WHERE e.project_id = @project_id
  AND e.state = 'pending'
  AND e.observed_at <= now() - @inactivity::interval
  AND NOT EXISTS (
    SELECT 1
    FROM chat_messages cm
    WHERE cm.chat_id = c.id
      AND (cm.project_id IS NULL OR cm.project_id = p.id)
      AND cm.created_at > now() - @inactivity::interval
  )
  AND (
    sqlc.narg('cursor_observed_at')::timestamptz IS NULL
    OR (e.observed_at, e.id) < (sqlc.narg('cursor_observed_at')::timestamptz, sqlc.narg('cursor_id')::uuid)
  )
ORDER BY e.observed_at DESC, e.id DESC
LIMIT @page_size;

-- name: ReserveChatAnalysisEvaluations :execrows
UPDATE chat_analysis_evaluations
SET state = 'reserved',
    reserved_on = @reserved_on::date,
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND id = ANY(@ids::uuid[])
  AND state = 'pending';

-- name: LoadReservedChatAnalysisEvaluations :many
-- Crash-recovery claim. Ownership is soft and time-bounded: a reserved row is
-- owned while its updated_at is younger than @claim_lease, so a second claimer
-- inside the lease selects nothing and the model call never has to hold a
-- transaction open. A null lease claims every reserved row committed before the
-- statement, which is the unleased read-back.
UPDATE chat_analysis_evaluations e
SET updated_at = clock_timestamp()
WHERE e.project_id = @project_id
  AND e.id IN (
    SELECT c.id
    FROM chat_analysis_evaluations c
    WHERE c.project_id = @project_id
      AND c.state = 'reserved'
      AND c.updated_at < now() - coalesce(sqlc.narg('claim_lease')::interval, interval '0')
    ORDER BY c.observed_at DESC, c.id DESC
    LIMIT @batch_size
    FOR UPDATE SKIP LOCKED
  )
RETURNING
  e.id,
  e.organization_id,
  e.project_id,
  e.chat_id,
  e.session_id,
  e.judge,
  e.observed_at,
  e.state,
  e.reserved_on,
  e.attempts,
  e.last_error,
  e.scored_at,
  e.created_at,
  e.updated_at;

-- name: ResetStaleChatAnalysisReservations :execrows
-- Returns a crashed reservation to the queue. attempts is preserved so a
-- poisonous unit still terminates at the attempt ceiling.
UPDATE chat_analysis_evaluations
SET state = 'pending',
    reserved_on = NULL,
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND state = 'reserved'
  AND updated_at < now() - @stale_after::interval;

-- name: MarkChatAnalysisEvaluationScored :execrows
UPDATE chat_analysis_evaluations
SET state = 'scored',
    scored_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND id = @id
  AND state = 'reserved';

-- name: RecordChatAnalysisEvaluationAttempt :one
-- Model, sink, or row-validation failure. The row never returns to pending:
-- that would free the budget and let a second reservation re-spend the same unit.
UPDATE chat_analysis_evaluations
SET attempts = attempts + 1,
    last_error = @last_error,
    state = CASE WHEN attempts + 1 >= @max_attempts::integer THEN 'failed' ELSE 'reserved' END,
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND id = @id
  AND state = 'reserved'
RETURNING state, attempts;

-- name: GetChatAnalysisJudgeInputs :many
-- evaluation_created_at is the row's birth stamp, which no transition rewrites.
-- It is the publication guard's lower bound: reserved_on moves forward when a
-- stale reservation is reset and re-reserved on a later day, so a bound derived
-- from it can end up after a score an earlier pass already inserted.
--
-- The project and chat liveness the reservation checked is rechecked here: a
-- deletion that lands between reserving and publishing drops the row from this
-- read, so the batch judges nothing and writes no score for it.
SELECT
  e.id,
  e.organization_id,
  e.chat_id,
  e.session_id,
  e.judge,
  e.observed_at,
  e.reserved_on,
  e.created_at AS evaluation_created_at,
  e.attempts
FROM chat_analysis_evaluations e
JOIN projects p
  ON p.id = e.project_id
  AND p.deleted IS FALSE
JOIN chats c
  ON c.project_id = e.project_id
  AND c.id = e.chat_id
  AND c.deleted IS FALSE
WHERE e.project_id = @project_id
  AND e.state = 'reserved'
  AND e.id = ANY(@ids::uuid[])
ORDER BY e.observed_at DESC, e.id DESC;

-- name: GetChatAnalysisEvaluation :one
-- Test fixture read: pipeline state assertions load the row directly.
SELECT *
FROM chat_analysis_evaluations
WHERE project_id = @project_id
  AND id = @id;

-- name: ListProjectsWithPendingChatAnalysisWork :many
-- Projects holding analysis work the pipeline has not finished: evaluations
-- still pending, or reservations whose owner is gone. Each source is walked one
-- project at a time and the recursion merges them, so a page costs the page
-- size rather than the size of the backlog behind it.
WITH RECURSIVE pending_projects AS (
  (
    SELECT candidate.project_id, 1 AS sequence
    FROM (
      (
        SELECT e.project_id
        FROM chat_analysis_evaluations e
        JOIN projects p
          ON p.id = e.project_id
          AND p.deleted IS FALSE
        JOIN chats c
          ON c.project_id = e.project_id
          AND c.id = e.chat_id
          AND c.deleted IS FALSE
        WHERE e.state = 'pending'
          AND e.project_id > @project_cursor
        ORDER BY e.project_id
        LIMIT 1
      )
      UNION ALL
      (
        SELECT e.project_id
        FROM chat_analysis_evaluations e
        JOIN projects p
          ON p.id = e.project_id
          AND p.deleted IS FALSE
        WHERE e.state = 'reserved'
          AND e.updated_at < now() - @stale_after::interval
          AND e.project_id > @project_cursor
        ORDER BY e.project_id
        LIMIT 1
      )
    ) candidate
    ORDER BY candidate.project_id
    LIMIT 1
  )
  UNION ALL
  SELECT next_project.project_id, current_project.sequence + 1
  FROM pending_projects current_project
  CROSS JOIN LATERAL (
    SELECT candidate.project_id
    FROM (
      (
        SELECT e.project_id
        FROM chat_analysis_evaluations e
        JOIN projects p
          ON p.id = e.project_id
          AND p.deleted IS FALSE
        JOIN chats c
          ON c.project_id = e.project_id
          AND c.id = e.chat_id
          AND c.deleted IS FALSE
        WHERE e.state = 'pending'
          AND e.project_id > current_project.project_id
        ORDER BY e.project_id
        LIMIT 1
      )
      UNION ALL
      (
        SELECT e.project_id
        FROM chat_analysis_evaluations e
        JOIN projects p
          ON p.id = e.project_id
          AND p.deleted IS FALSE
        WHERE e.state = 'reserved'
          AND e.updated_at < now() - @stale_after::interval
          AND e.project_id > current_project.project_id
        ORDER BY e.project_id
        LIMIT 1
      )
    ) candidate
    ORDER BY candidate.project_id
    LIMIT 1
  ) next_project
  WHERE current_project.sequence < @page_limit::int
)
SELECT
  pending.project_id,
  EXISTS (
    SELECT 1
    FROM chat_analysis_evaluations e
    WHERE e.project_id = pending.project_id
      AND e.state = 'reserved'
      AND e.updated_at < now() - @stale_after::interval
  ) AS has_stale
FROM pending_projects pending
ORDER BY pending.project_id;
