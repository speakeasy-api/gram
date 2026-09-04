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

-- name: GetAgentByID :one
SELECT *
FROM agents
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE
LIMIT 1;
