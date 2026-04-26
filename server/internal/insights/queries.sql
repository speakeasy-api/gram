-- name: InsertProposal :one
INSERT INTO insights_proposals (
  project_id,
  organization_id,
  kind,
  target_ref,
  current_value,
  proposed_value,
  reasoning,
  source_chat_id,
  status
) VALUES (
  @project_id,
  @organization_id,
  @kind,
  @target_ref,
  @current_value,
  @proposed_value,
  @reasoning,
  @source_chat_id,
  'pending'
)
RETURNING *;

-- name: GetProposalByID :one
SELECT *
FROM insights_proposals
WHERE id = @id
  AND project_id = @project_id;

-- name: ListProposalsByStatus :many
SELECT *
FROM insights_proposals
WHERE project_id = @project_id
  AND (CAST(@status_filter AS TEXT) = '' OR status = @status_filter)
ORDER BY created_at DESC
LIMIT 200;

-- name: MarkProposalApplied :one
UPDATE insights_proposals
SET status = 'applied',
    applied_value = @applied_value,
    applied_at = clock_timestamp(),
    applied_by_user_id = @applied_by_user_id,
    updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND status = 'pending'
RETURNING *;

-- name: MarkProposalSuperseded :one
UPDATE insights_proposals
SET status = 'superseded',
    updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND status = 'pending'
RETURNING *;

-- name: MarkProposalDismissed :one
UPDATE insights_proposals
SET status = 'dismissed',
    dismissed_at = clock_timestamp(),
    dismissed_by_user_id = @dismissed_by_user_id,
    updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND status = 'pending'
RETURNING *;

-- name: MarkProposalRolledBack :one
UPDATE insights_proposals
SET status = 'rolled_back',
    rolled_back_at = clock_timestamp(),
    rolled_back_by_user_id = @rolled_back_by_user_id,
    updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND status = 'applied'
RETURNING *;

-- name: InsertMemory :one
INSERT INTO insights_memories (
  project_id,
  organization_id,
  kind,
  content,
  tags,
  source_chat_id,
  expires_at
) VALUES (
  @project_id,
  @organization_id,
  @kind,
  @content,
  @tags,
  @source_chat_id,
  @expires_at
)
RETURNING *;

-- name: GetMemoryByID :one
SELECT *
FROM insights_memories
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: SoftDeleteMemory :one
UPDATE insights_memories
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: ListMemories :many
SELECT *
FROM insights_memories
WHERE project_id = @project_id
  AND deleted IS FALSE
  -- Hide expired memories: findings get a 7-day TTL on insert; the future
  -- 90-day no-use pruner will set expires_at on fact/playbook rows that have
  -- not been recalled in a long time. Recall queries should never surface
  -- expired memories regardless of why they expired.
  AND (expires_at IS NULL OR expires_at > clock_timestamp())
  AND (CAST(@kind_filter AS TEXT) = '' OR kind = @kind_filter)
  AND (
    coalesce(array_length(@tag_filter::text[], 1), 0) = 0
    OR tags && @tag_filter::text[]
  )
ORDER BY
  CASE
    WHEN coalesce(array_length(@tag_filter::text[], 1), 0) = 0 THEN 0
    ELSE coalesce(array_length(ARRAY(SELECT unnest(tags) INTERSECT SELECT unnest(@tag_filter::text[])), 1), 0)
  END DESC,
  last_used_at DESC
LIMIT @result_limit::int;

-- name: BumpMemoryUsage :exec
UPDATE insights_memories
SET usefulness_score = usefulness_score + 1,
    last_used_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = ANY(@ids::uuid[])
  AND project_id = @project_id
  AND deleted IS FALSE;
