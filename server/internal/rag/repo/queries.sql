-- name: DeleteToolsetEmbeddings :exec
-- NOTE: Hard delete while in experimentation phase to preserve space.
-- Consider switching to soft delete when feature is production-ready.
DELETE FROM toolset_embeddings
WHERE toolset_id = @toolset_id
  AND entry_key LIKE 'tools:%'
  AND deleted IS FALSE;

-- name: LockToolsetEmbeddings :exec
-- Serialize replacement of one toolset's embeddings while permitting
-- different toolsets to index concurrently.
SELECT pg_advisory_xact_lock(hashtextextended('toolset-embeddings:' || (@toolset_id::uuid)::text, 0));

-- name: ToolsetIndexRevisionIsCurrent :one
SELECT EXISTS (
  SELECT 1
  FROM toolsets t
  WHERE t.id = @toolset_id
    AND t.project_id = @project_id
    AND t.deleted IS FALSE
    AND (
      SELECT tv.version
      FROM toolset_versions tv
      WHERE tv.toolset_id = t.id
        AND tv.deleted IS FALSE
      ORDER BY tv.version DESC
      LIMIT 1
    ) = @toolset_version
    AND (
      SELECT d.id
      FROM deployments d
      WHERE d.project_id = t.project_id
        AND EXISTS (
          SELECT 1
          FROM deployment_statuses ds
          WHERE ds.deployment_id = d.id
            AND ds.status = 'completed'
        )
      ORDER BY d.seq DESC
      LIMIT 1
    ) = @deployment_id
);

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
  SELECT d.id
  FROM deployments d
  JOIN deployment_statuses ds ON d.id = ds.deployment_id
  WHERE d.project_id = @project_id
    AND ds.status = 'completed'
  ORDER BY d.seq DESC
  LIMIT 1
)
SELECT EXISTS (
  SELECT 1
  FROM toolset_embeddings
  WHERE toolset_embeddings.toolset_id = @toolset_id
    AND toolset_embeddings.toolset_version = @toolset_version
    AND toolset_embeddings.entry_key LIKE 'tools:%'
    AND toolset_embeddings.payload ->> '_gramIndexDeploymentId' = (SELECT id::text FROM latest_deployment)
    AND toolset_embeddings.deleted IS FALSE
) AS indexed;

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

-- name: SearchToolsetToolEmbeddingsAnyTagsMatchExact :many
WITH candidates AS MATERIALIZED (
  SELECT
      id,
      project_id,
      toolset_id,
      toolset_version,
      entry_key,
      embedding_model,
      embedding_1536,
      payload,
      tags,
      created_at,
      updated_at
  FROM toolset_embeddings
  WHERE project_id = @project_id
    AND toolset_id = @toolset_id
    AND toolset_version = @toolset_version
    AND entry_key LIKE 'tools:%'
    AND (cardinality(sqlc.arg('tags')::text[]) = 0 OR tags && sqlc.arg('tags')::text[])
    AND deleted IS FALSE
)
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
FROM candidates
ORDER BY embedding_1536 <=> @query_embedding_1536
LIMIT @result_limit;

-- name: SearchToolsetToolEmbeddingsAllTagsMatchExact :many
WITH candidates AS MATERIALIZED (
  SELECT
      id,
      project_id,
      toolset_id,
      toolset_version,
      entry_key,
      embedding_model,
      embedding_1536,
      payload,
      tags,
      created_at,
      updated_at
  FROM toolset_embeddings
  WHERE project_id = @project_id
    AND toolset_id = @toolset_id
    AND toolset_version = @toolset_version
    AND entry_key LIKE 'tools:%'
    AND (cardinality(@tags::text[]) = 0 OR tags @> @tags)
    AND deleted IS FALSE
)
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
FROM candidates
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
