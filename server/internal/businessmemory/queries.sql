-- name: InsertBusinessMemory :exec
INSERT INTO business_memories (
  project_id,
  organization_id,
  body,
  memory_type,
  structural_scope,
  content_scope,
  embedding,
  embedding_model,
  extraction_model,
  source_evaluation_id,
  source_candidate_index,
  source_chat_id,
  source_turn,
  source_author_id
) VALUES (
  @project_id,
  @organization_id,
  @body,
  @memory_type,
  @structural_scope,
  @content_scope,
  @embedding,
  @embedding_model,
  @extraction_model,
  @source_evaluation_id,
  @source_candidate_index,
  @source_chat_id,
  @source_turn,
  @source_author_id
)
ON CONFLICT (source_evaluation_id, source_candidate_index) DO NOTHING;

-- name: LockBusinessMemoryExtraction :exec
SELECT pg_advisory_xact_lock(hashtextextended(@lock_key, 0));

-- name: CompleteBusinessMemoryEvaluation :execrows
UPDATE chat_analysis_evaluations
SET state = 'scored',
    scored_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND id = @id
  AND state = 'reserved';

-- name: GetNearestActiveBusinessMemory :one
SELECT
  id,
  (1 - (embedding <=> @query_embedding))::float8 AS similarity
FROM business_memories
WHERE project_id = @project_id
  AND organization_id = @organization_id
  AND embedding_model = @embedding_model
  AND deleted IS FALSE
  AND lifecycle_state = 'active'
  AND (
    source_evaluation_id IS DISTINCT FROM @source_evaluation_id
    OR source_candidate_index <> @source_candidate_index
  )
ORDER BY embedding <=> @query_embedding
LIMIT 1;

-- name: ListBusinessMemories :many
SELECT
  id,
  project_id,
  organization_id,
  body,
  memory_type,
  structural_scope,
  content_scope,
  embedding_model,
  extraction_model,
  source_chat_id,
  source_turn,
  source_author_id,
  extracted_at,
  lifecycle_state,
  created_at,
  updated_at
FROM business_memories
WHERE project_id = @project_id
  AND organization_id = @organization_id
  AND deleted IS FALSE
  AND lifecycle_state = 'active'
  AND (
    sqlc.narg(content_scope)::text IS NULL
    OR content_scope ? sqlc.narg(content_scope)::text
  )
  AND (
    sqlc.narg(content_scope_namespace)::text IS NULL
    OR EXISTS (
        SELECT 1
        FROM jsonb_array_elements_text(content_scope) AS scope(value)
        WHERE (
          scope.value = sqlc.narg(content_scope_namespace)::text
          OR starts_with(
            scope.value,
            sqlc.narg(content_scope_namespace)::text || ':'
          )
        )
      )
  )
  AND (
    sqlc.narg(cursor_created_at)::timestamptz IS NULL
    OR (created_at, id) < (
      sqlc.narg(cursor_created_at)::timestamptz,
      sqlc.narg(cursor_id)::uuid
    )
  )
ORDER BY created_at DESC, id DESC
LIMIT @page_limit;

-- name: ListBusinessMemoryContentScopes :many
WITH expanded AS (
  SELECT
    business_memories.id AS memory_id,
    scope.value::text AS content_scope,
    split_part(scope.value::text, ':', 1) AS namespace
  FROM business_memories
  CROSS JOIN LATERAL jsonb_array_elements_text(content_scope) AS scope(value)
  WHERE project_id = @project_id
    AND organization_id = @organization_id
    AND deleted IS FALSE
    AND lifecycle_state = 'active'
)
SELECT
  namespace AS scope,
  NULL::text AS parent_scope,
  count(DISTINCT memory_id)::bigint AS memory_count
FROM expanded
WHERE namespace <> ''
GROUP BY namespace
UNION ALL
SELECT
  content_scope AS scope,
  namespace AS parent_scope,
  count(DISTINCT memory_id)::bigint AS memory_count
FROM expanded
WHERE namespace <> ''
  AND content_scope <> namespace
GROUP BY namespace, content_scope
ORDER BY parent_scope NULLS FIRST, scope;

-- name: CountBusinessMemories :one
SELECT count(*)::bigint
FROM business_memories
WHERE project_id = @project_id
  AND organization_id = @organization_id
  AND deleted IS FALSE
  AND lifecycle_state = 'active';

-- name: SearchBusinessMemories :many
SELECT
  id,
  project_id,
  organization_id,
  body,
  memory_type,
  structural_scope,
  content_scope,
  embedding_model,
  extraction_model,
  source_chat_id,
  source_turn,
  source_author_id,
  extracted_at,
  lifecycle_state,
  created_at,
  updated_at,
  (1 - (embedding <=> @query_embedding))::float8 AS similarity
FROM business_memories
WHERE project_id = @project_id
  AND organization_id = @organization_id
  AND embedding_model = @embedding_model
  AND deleted IS FALSE
  AND lifecycle_state = 'active'
  AND (
    sqlc.narg(content_scope)::text IS NULL
    OR content_scope ? sqlc.narg(content_scope)::text
  )
  AND (
    sqlc.narg(content_scope_namespace)::text IS NULL
    OR EXISTS (
        SELECT 1
        FROM jsonb_array_elements_text(content_scope) AS scope(value)
        WHERE (
          scope.value = sqlc.narg(content_scope_namespace)::text
          OR starts_with(
            scope.value,
            sqlc.narg(content_scope_namespace)::text || ':'
          )
        )
      )
  )
ORDER BY embedding <=> @query_embedding
LIMIT @result_limit;
