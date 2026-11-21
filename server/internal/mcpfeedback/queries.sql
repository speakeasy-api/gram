-- name: GetFeedbackConfig :one
SELECT *
FROM mcp_server_feedback_configs
WHERE mcp_server_id = @mcp_server_id
  AND project_id = @project_id;

-- name: GetFeedbackConfigForUpdate :one
SELECT *
FROM mcp_server_feedback_configs
WHERE mcp_server_id = @mcp_server_id
  AND project_id = @project_id
FOR UPDATE;

-- name: GetFeedbackConfigEditor :one
-- The email of the person who last changed a server's feedback settings. No
-- row when an API key made the change or the person has since been removed.
SELECT editor.email
FROM mcp_server_feedback_configs config
JOIN users editor
  ON editor.id = config.updated_by_user_id
JOIN organization_user_relationships membership
  ON membership.user_id = editor.id
  AND membership.organization_id = config.organization_id
  AND membership.deleted_at IS NULL
WHERE config.mcp_server_id = @mcp_server_id
  AND config.project_id = @project_id;

-- name: UpsertFeedbackConfig :one
INSERT INTO mcp_server_feedback_configs (
    mcp_server_id
  , organization_id
  , project_id
  , enabled
  , tool_name
  , tool_description
  , updated_by_user_id
)
VALUES (
    @mcp_server_id
  , @organization_id
  , @project_id
  , @enabled
  , sqlc.narg(tool_name)
  , sqlc.narg(tool_description)
  , sqlc.narg(updated_by_user_id)
)
ON CONFLICT (mcp_server_id) DO UPDATE SET
    enabled = EXCLUDED.enabled
  , tool_name = EXCLUDED.tool_name
  , tool_description = EXCLUDED.tool_description
  , updated_by_user_id = EXCLUDED.updated_by_user_id
  , updated_at = clock_timestamp()
WHERE mcp_server_feedback_configs.project_id = EXCLUDED.project_id
RETURNING *;

-- name: InsertFeedback :one
INSERT INTO mcp_server_feedback (
    organization_id
  , project_id
  , mcp_server_id
  , content
  , severity
  , workaround
  , intention
  , tool_name
  , model
  , user_id
  , external_user_id
  , client_name
  , client_version
)
VALUES (
    @organization_id
  , @project_id
  , @mcp_server_id
  , @content
  , sqlc.narg(severity)
  , sqlc.narg(workaround)
  , sqlc.narg(intention)
  , sqlc.narg(tool_name)
  , sqlc.narg(model)
  , sqlc.narg(user_id)
  , sqlc.narg(external_user_id)
  , sqlc.narg(client_name)
  , sqlc.narg(client_version)
)
RETURNING id, created_at;

-- name: ListFeedback :many
-- A page of one server's feedback, newest first. The email belongs to the
-- member who sent the report and is absent for callers outside the
-- organization and for members who have since been removed.
SELECT
    feedback.*
  , member.email AS user_email
FROM mcp_server_feedback feedback
LEFT JOIN organization_user_relationships membership
  ON membership.user_id = feedback.user_id
  AND membership.organization_id = feedback.organization_id
  AND membership.deleted_at IS NULL
LEFT JOIN users member
  ON member.id = membership.user_id
WHERE feedback.project_id = @project_id
  AND feedback.mcp_server_id = @mcp_server_id
  AND (sqlc.narg(severity)::text IS NULL OR feedback.severity = sqlc.narg(severity)::text)
  AND (sqlc.narg(tool_name)::text IS NULL OR feedback.tool_name = sqlc.narg(tool_name)::text)
  AND (
    sqlc.narg(cursor_created_at)::timestamptz IS NULL
    OR (feedback.created_at, feedback.id) < (
      sqlc.narg(cursor_created_at)::timestamptz,
      sqlc.narg(cursor_id)::uuid
    )
  )
ORDER BY feedback.created_at DESC, feedback.id DESC
LIMIT @page_limit;

-- name: CountFeedbackBySeverity :one
-- Feedback for one server over the last window_days days, by severity. The
-- window is measured on the database clock, the one that stamps each report,
-- and starts at midnight UTC so the days it covers are whole.
WITH bounds AS (
  SELECT
      clock_timestamp()::timestamptz AS window_end
    , (date_trunc('day', clock_timestamp(), 'UTC') - (sqlc.arg(window_days)::int - 1) * interval '1 day')::timestamptz AS window_start
)
SELECT
    bounds.window_start
  , bounds.window_end
  , COUNT(feedback.id)::bigint AS total
  , COUNT(feedback.id) FILTER (WHERE feedback.severity = 'blocked')::bigint AS blocked
  , COUNT(feedback.id) FILTER (WHERE feedback.severity = 'degraded')::bigint AS degraded
  , COUNT(feedback.id) FILTER (WHERE feedback.severity = 'cosmetic')::bigint AS cosmetic
  , COUNT(feedback.id) FILTER (WHERE feedback.severity IS NULL)::bigint AS unspecified
FROM bounds
LEFT JOIN mcp_server_feedback feedback
  ON feedback.project_id = @project_id
  AND feedback.mcp_server_id = @mcp_server_id
  AND feedback.created_at >= bounds.window_start
GROUP BY bounds.window_start, bounds.window_end;

-- name: ListFeedbackToolCounts :many
-- The tools named most often in one server's feedback since window_start, and
-- how many of each tool's reports said the agent was blocked.
SELECT
    tool_name::text AS tool_name
  , COUNT(*)::bigint AS feedback_count
  , (COUNT(*) FILTER (WHERE severity = 'blocked'))::bigint AS blocked_count
FROM mcp_server_feedback
WHERE project_id = @project_id
  AND mcp_server_id = @mcp_server_id
  AND created_at >= @window_start
  AND tool_name IS NOT NULL
GROUP BY tool_name
ORDER BY feedback_count DESC, tool_name ASC
LIMIT @tool_limit;
