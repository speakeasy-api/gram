-- name: CreateDraft :one
INSERT INTO corpus_drafts (
    project_id,
    organization_id,
    file_path,
    content,
    operation,
    source,
    author_type,
    labels
) VALUES (
    @project_id,
    @organization_id,
    @file_path,
    @content,
    @operation,
    @source,
    @author_type,
    @labels
)
RETURNING *;

-- name: GetDraft :one
SELECT *
FROM corpus_drafts
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: ListDrafts :many
SELECT *
FROM corpus_drafts
WHERE project_id = @project_id
  AND deleted IS FALSE
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
ORDER BY created_at DESC;

-- name: UpdateDraftContent :one
UPDATE corpus_drafts
SET content = @content,
    updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
  AND status = 'open'
RETURNING *;

-- name: SoftDeleteDraft :one
UPDATE corpus_drafts
SET status = 'rejected',
    deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
  AND status = 'open'
RETURNING *;

-- name: MarkDraftsPublished :many
UPDATE corpus_drafts
SET status = 'published',
    commit_sha = @commit_sha,
    updated_at = clock_timestamp()
WHERE id = ANY(@ids::uuid[])
  AND project_id = @project_id
  AND status = 'open'
  AND deleted IS FALSE
RETURNING *;

-- name: CreatePublishEvent :one
INSERT INTO corpus_publish_events (
    project_id,
    organization_id,
    commit_sha
) VALUES (
    @project_id,
    @organization_id,
    @commit_sha
)
RETURNING *;

-- name: ListOpenDraftsByProject :many
SELECT *
FROM corpus_drafts
WHERE project_id = @project_id
  AND status = 'open'
  AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: CountOpenDraftsByFilePath :many
SELECT file_path, COUNT(*) AS open_drafts
FROM corpus_drafts
WHERE project_id = @project_id
  AND status = 'open'
  AND deleted IS FALSE
GROUP BY file_path;
