-- name: UpsertChunk :one
INSERT INTO corpus_chunks (
    project_id,
    organization_id,
    chunk_id,
    file_path,
    heading_path,
    breadcrumb,
    content,
    content_text,
    embedding,
    metadata,
    strategy,
    manifest_fingerprint,
    content_fingerprint
) VALUES (
    @project_id,
    @organization_id,
    @chunk_id,
    @file_path,
    @heading_path,
    @breadcrumb,
    @content,
    @content_text,
    @embedding,
    @metadata,
    @strategy,
    @manifest_fingerprint,
    @content_fingerprint
)
ON CONFLICT (project_id, chunk_id) DO UPDATE SET
    file_path = EXCLUDED.file_path,
    heading_path = EXCLUDED.heading_path,
    breadcrumb = EXCLUDED.breadcrumb,
    content = EXCLUDED.content,
    content_text = EXCLUDED.content_text,
    embedding = EXCLUDED.embedding,
    metadata = EXCLUDED.metadata,
    strategy = EXCLUDED.strategy,
    manifest_fingerprint = EXCLUDED.manifest_fingerprint,
    content_fingerprint = EXCLUDED.content_fingerprint,
    updated_at = clock_timestamp()
RETURNING *;

-- name: DeleteChunksByFilePath :exec
DELETE FROM corpus_chunks
WHERE project_id = @project_id
  AND file_path = @file_path;

-- name: ListChunksByProject :many
SELECT *
FROM corpus_chunks
WHERE project_id = @project_id
ORDER BY file_path, chunk_id;

-- name: ListChunkFingerprintsByProject :many
SELECT chunk_id, content_fingerprint
FROM corpus_chunks
WHERE project_id = @project_id;

-- name: UpsertIndexState :one
INSERT INTO corpus_index_state (
    project_id,
    organization_id,
    last_indexed_sha,
    embedding_model
) VALUES (
    @project_id,
    @organization_id,
    @last_indexed_sha,
    @embedding_model
)
ON CONFLICT (project_id) DO UPDATE SET
    last_indexed_sha = EXCLUDED.last_indexed_sha,
    embedding_model = EXCLUDED.embedding_model,
    updated_at = clock_timestamp()
RETURNING *;

-- name: GetIndexState :one
SELECT *
FROM corpus_index_state
WHERE project_id = @project_id;

-- name: ListPendingPublishEvents :many
SELECT *
FROM corpus_publish_events
WHERE status = 'pending'
ORDER BY created_at ASC
LIMIT @max_rows;

-- name: UpdatePublishEventStatus :one
UPDATE corpus_publish_events
SET status = @status,
    updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
RETURNING *;

-- name: InsertPublishEvent :one
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

-- name: GetPublishEvent :one
SELECT *
FROM corpus_publish_events
WHERE id = @id
  AND project_id = @project_id;
