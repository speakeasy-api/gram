-- name: CreateAgent :one
INSERT INTO agents (
  organization_id,
  owner_user_id,
  name
) VALUES (
  @organization_id,
  @owner_user_id,
  @name
)
RETURNING *;

-- name: CreateAgentWithID :one
INSERT INTO agents (
  id,
  organization_id,
  owner_user_id,
  name
) VALUES (
  @id,
  @organization_id,
  @owner_user_id,
  @name
)
RETURNING *;

-- name: GetAgentByID :one
SELECT *
FROM agents
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE
LIMIT 1;

-- name: GetAgentByIDForUpdate :one
SELECT *
FROM agents
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE
LIMIT 1
FOR UPDATE;

-- name: RenameAgent :one
UPDATE agents
SET name = @name,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE
RETURNING *;

-- name: SuspendAgent :one
UPDATE agents
SET suspended_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE
  AND suspended_at IS NULL
  AND revoked_at IS NULL
RETURNING *;

-- name: ResumeAgent :one
UPDATE agents
SET suspended_at = NULL,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE
  AND suspended_at IS NOT NULL
  AND revoked_at IS NULL
RETURNING *;

-- name: RevokeAgent :one
UPDATE agents
SET suspended_at = NULL,
    revoked_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE
  AND revoked_at IS NULL
RETURNING *;

-- name: DeleteAgent :one
UPDATE agents
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE
RETURNING *;
