-- name: GetAutoPublishConfig :one
SELECT *
FROM corpus_auto_publish_configs
WHERE project_id = @project_id;

-- name: UpsertAutoPublishConfig :one
INSERT INTO corpus_auto_publish_configs (
    project_id,
    organization_id,
    enabled,
    interval_minutes,
    min_upvotes,
    author_type_filter,
    label_filter,
    min_age_hours
) VALUES (
    @project_id,
    @organization_id,
    @enabled,
    @interval_minutes,
    @min_upvotes,
    @author_type_filter,
    @label_filter,
    @min_age_hours
)
ON CONFLICT (project_id) DO UPDATE SET
    enabled = EXCLUDED.enabled,
    interval_minutes = EXCLUDED.interval_minutes,
    min_upvotes = EXCLUDED.min_upvotes,
    author_type_filter = EXCLUDED.author_type_filter,
    label_filter = EXCLUDED.label_filter,
    min_age_hours = EXCLUDED.min_age_hours,
    updated_at = clock_timestamp()
RETURNING *;

-- name: QueryEligibleDrafts :many
SELECT d.*
FROM corpus_drafts d
LEFT JOIN (
    SELECT project_id, file_path, COUNT(*) FILTER (WHERE direction = 'up') AS upvotes
    FROM corpus_feedback
    GROUP BY project_id, file_path
) f ON f.project_id = d.project_id AND f.file_path = d.file_path
WHERE d.project_id = @project_id
  AND d.status = 'open'
  AND d.deleted IS FALSE
  AND (sqlc.arg('min_upvotes')::int = 0 OR COALESCE(f.upvotes, 0) >= sqlc.arg('min_upvotes')::int)
  AND (sqlc.narg('author_type_filter')::text IS NULL OR d.author_type = sqlc.narg('author_type_filter'))
  AND (sqlc.arg('min_age_hours')::int = 0 OR d.created_at <= clock_timestamp() - make_interval(hours => sqlc.arg('min_age_hours')::int))
ORDER BY d.created_at ASC;

-- name: InsertFeedback :one
INSERT INTO corpus_feedback (
    project_id,
    organization_id,
    file_path,
    user_id,
    direction
) VALUES (
    @project_id,
    @organization_id,
    @file_path,
    @user_id,
    @direction
)
RETURNING *;
