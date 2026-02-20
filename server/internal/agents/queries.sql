-- name: CreateAgentDefinition :one
INSERT INTO agent_definitions (project_id, tool_urn, name, description, title, instructions, tools, model, read_only_hint, destructive_hint, idempotent_hint, open_world_hint)
VALUES (@project_id, @tool_urn, @name, @description, @title, @instructions, @tools, @model, @read_only_hint, @destructive_hint, @idempotent_hint, @open_world_hint)
RETURNING *;

-- name: GetAgentDefinitionByID :one
SELECT * FROM agent_definitions WHERE id = @id AND project_id = @project_id AND deleted IS FALSE;

-- name: ListAgentDefinitions :many
SELECT * FROM agent_definitions WHERE project_id = @project_id AND deleted IS FALSE ORDER BY created_at DESC;

-- name: UpdateAgentDefinition :one
UPDATE agent_definitions SET
  description = COALESCE(sqlc.narg('description'), description),
  title = COALESCE(sqlc.narg('title'), title),
  instructions = COALESCE(sqlc.narg('instructions'), instructions),
  tools = COALESCE(sqlc.narg('tools'), tools),
  model = COALESCE(sqlc.narg('model'), model),
  read_only_hint = COALESCE(sqlc.narg('read_only_hint'), read_only_hint),
  destructive_hint = COALESCE(sqlc.narg('destructive_hint'), destructive_hint),
  idempotent_hint = COALESCE(sqlc.narg('idempotent_hint'), idempotent_hint),
  open_world_hint = COALESCE(sqlc.narg('open_world_hint'), open_world_hint),
  updated_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: DeleteAgentDefinition :exec
UPDATE agent_definitions SET deleted_at = clock_timestamp() WHERE id = @id AND project_id = @project_id AND deleted_at IS NULL;
