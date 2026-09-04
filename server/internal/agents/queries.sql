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

-- name: TransferAgent :one
UPDATE agents
SET owner_user_id = @owner_user_id,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE
  AND owner_reassignment_required_at IS NULL
  AND owner_user_id <> @owner_user_id
RETURNING *;

-- name: ReassignAgent :one
UPDATE agents
SET owner_user_id = @owner_user_id,
    owner_reassignment_required_at = NULL,
    owner_reassignment_reason = NULL,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE
  AND owner_reassignment_required_at IS NOT NULL
RETURNING *;

-- name: LatchAgentsForOwnerLossByUser :many
UPDATE agents
SET owner_reassignment_required_at = clock_timestamp(),
    owner_reassignment_reason = @owner_reassignment_reason,
    updated_at = clock_timestamp()
WHERE owner_user_id = @owner_user_id
  AND owner_reassignment_required_at IS NULL
RETURNING *;

-- name: LatchAgentsForOwnerLossByMembership :many
UPDATE agents
SET owner_reassignment_required_at = clock_timestamp(),
    owner_reassignment_reason = @owner_reassignment_reason,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND owner_user_id = @owner_user_id
  AND owner_reassignment_required_at IS NULL
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
