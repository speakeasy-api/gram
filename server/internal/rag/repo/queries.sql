-- name: DeleteToolsetEmbeddings :exec
-- NOTE: Hard delete while in experimentation phase to preserve space.
-- Consider switching to soft delete when feature is production-ready.
DELETE FROM toolset_embeddings
WHERE toolset_id = @toolset_id
  AND entry_key LIKE 'tools:%'
  AND deleted IS FALSE;

-- name: InsertToolsetEmbedding :one
INSERT INTO toolset_embeddings (
    project_id,
    toolset_id,
    toolset_version,
    entry_key,
    embedding_model,
    embedding_1536,
    payload,
    tags
) VALUES (
    @project_id,
    @toolset_id,
    @toolset_version,
    @entry_key,
    @embedding_model,
    @embedding_1536,
    @payload,
    @tags
)
RETURNING *;

-- name: ToolsetToolsAreIndexed :one
WITH latest_deployment AS (
  SELECT d.created_at
  FROM deployments d
  JOIN deployment_statuses ds ON d.id = ds.deployment_id
  WHERE d.project_id = @project_id
    AND ds.status = 'completed'
  ORDER BY d.created_at DESC
  LIMIT 1
),
latest_embedding AS (
  SELECT MAX(created_at) as created_at
  FROM toolset_embeddings
  WHERE toolset_embeddings.toolset_id = @toolset_id
    AND toolset_embeddings.toolset_version = @toolset_version
    AND entry_key LIKE 'tools:%'
    AND deleted IS FALSE
)
SELECT
  CASE
    -- If no embeddings exist for this version, not indexed
    WHEN (SELECT created_at FROM latest_embedding) IS NULL THEN FALSE
    -- If embeddings exist but are older than latest deployment, not indexed
    WHEN (SELECT created_at FROM latest_deployment) IS NOT NULL
         AND (SELECT created_at FROM latest_embedding) < (SELECT created_at FROM latest_deployment) THEN FALSE
    -- Otherwise, embeddings are up to date
    ELSE TRUE
  END as indexed;

-- name: SearchToolsetToolEmbeddingsAnyTagsMatch :many
SELECT
    id,
    project_id,
    toolset_id,
    toolset_version,
    entry_key,
    embedding_model,
    payload,
    tags,
    created_at,
    updated_at,
    (1 - (embedding_1536 <=> @query_embedding_1536))::float8 AS similarity
FROM toolset_embeddings
WHERE project_id = @project_id
  AND toolset_id = @toolset_id
  AND toolset_version = @toolset_version
  AND entry_key LIKE 'tools:%'
  AND (cardinality(sqlc.arg('tags')::text[]) = 0 OR tags && sqlc.arg('tags')::text[])
  AND deleted IS FALSE
ORDER BY embedding_1536 <=> @query_embedding_1536
LIMIT @result_limit;

-- name: SearchToolsetToolEmbeddingsAllTagsMatch :many
SELECT
    id,
    project_id,
    toolset_id,
    toolset_version,
    entry_key,
    embedding_model,
    payload,
    tags,
    created_at,
    updated_at,
    (1 - (embedding_1536 <=> @query_embedding_1536))::float8 AS similarity
FROM toolset_embeddings
WHERE project_id = @project_id
  AND toolset_id = @toolset_id
  AND toolset_version = @toolset_version
  AND entry_key LIKE 'tools:%'
  AND (cardinality(@tags::text[]) = 0 OR tags @> @tags)
  AND deleted IS FALSE
ORDER BY embedding_1536 <=> @query_embedding_1536
LIMIT @result_limit;

-- name: ToolsetAvailableTags :many
SELECT DISTINCT unnest(tags)::text as tag
FROM toolset_embeddings
WHERE project_id = @project_id
  AND toolset_id = @toolset_id
  AND toolset_version = @toolset_version
  AND entry_key LIKE 'tools:%'
  AND deleted IS FALSE
ORDER BY tag;