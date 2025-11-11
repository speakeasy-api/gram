-- name: UpsertToolsetEmbedding :one
INSERT INTO toolset_embeddings (
    project_id,
    toolset_id,
    entry_key,
    embedding_model,
    embedding_1536,
    payload
) VALUES (
    @project_id,
    @toolset_id,
    @entry_key,
    @embedding_model,
    @embedding_1536,
    @payload
) ON CONFLICT (project_id, toolset_id, entry_key)
WHERE deleted IS FALSE
DO UPDATE SET
    embedding_model = EXCLUDED.embedding_model,
    embedding_1536 = EXCLUDED.embedding_1536,
    payload = EXCLUDED.payload,
    updated_at = clock_timestamp(),
    deleted_at = NULL
RETURNING *;

-- name: GetToolsetEmbedding :one
SELECT *
FROM toolset_embeddings
WHERE toolset_id = @toolset_id
  AND project_id = @project_id
  AND entry_key = @entry_key
  AND deleted IS FALSE;

-- name: SearchToolsetEmbeddings :many
SELECT
    id,
    project_id,
    toolset_id,
    entry_key,
    embedding_model,
    payload,
    created_at,
    updated_at,
    1 - (embedding_1536 <=> @query_embedding_1536) AS similarity
FROM toolset_embeddings
WHERE project_id = @project_id
  AND toolset_id = @toolset_id
  AND deleted IS FALSE
ORDER BY embedding_1536 <=> @query_embedding_1536
LIMIT @result_limit;