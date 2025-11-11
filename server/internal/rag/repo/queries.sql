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
) ON CONFLICT (toolset_id, entry_key)
WHERE deleted IS FALSE
DO UPDATE SET
    embedding_model = EXCLUDED.embedding_model,
    embedding_1536 = EXCLUDED.embedding_1536,
    payload = EXCLUDED.payload,
    updated_at = clock_timestamp(),
    deleted_at = NULL
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
latest_toolset_version AS (
  SELECT created_at
  FROM toolset_versions
  WHERE toolset_versions.toolset_id = @toolset_id
  ORDER BY created_at DESC
  LIMIT 1
),
latest_embedding AS (
  SELECT MAX(created_at) as created_at
  FROM toolset_embeddings
  WHERE toolset_embeddings.toolset_id = @toolset_id
    AND entry_key LIKE 'tool:%'
    AND deleted IS FALSE
)
SELECT
  CASE
    WHEN le.created_at IS NULL THEN FALSE
    WHEN ld.created_at IS NOT NULL AND le.created_at < ld.created_at THEN FALSE
    WHEN ltv.created_at IS NOT NULL AND le.created_at < ltv.created_at THEN FALSE
    ELSE TRUE
  END as indexed
FROM latest_embedding le
LEFT JOIN latest_deployment ld ON true
LEFT JOIN latest_toolset_version ltv ON true;

-- name: SearchToolsetToolEmbeddings :many
SELECT
    id,
    project_id,
    toolset_id,
    entry_key,
    embedding_model,
    payload,
    created_at,
    updated_at,
    (1 - (embedding_1536 <=> @query_embedding_1536))::float8 AS similarity
FROM toolset_embeddings
WHERE project_id = @project_id
  AND toolset_id = @toolset_id
  AND entry_key LIKE 'tool:%'
  AND deleted IS FALSE
ORDER BY embedding_1536 <=> @query_embedding_1536
LIMIT @result_limit;