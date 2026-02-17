-- name: UpsertAgentExecution :one
INSERT INTO agent_executions (
    id
  , project_id
  , deployment_id
  , status
  , started_at
  , completed_at
)
VALUES (
    @id
  , @project_id
  , @deployment_id
  , @status
  , @started_at
  , @completed_at
)
ON CONFLICT (id) DO UPDATE SET
    status = @status,
    started_at = COALESCE(@started_at, agent_executions.started_at),
    completed_at = COALESCE(@completed_at, agent_executions.completed_at),
    updated_at = clock_timestamp()
RETURNING *;

-- name: DeleteAgentExecution :exec
UPDATE agent_executions
SET deleted_at = clock_timestamp()
WHERE id = @id
  AND deleted_at IS NULL;
