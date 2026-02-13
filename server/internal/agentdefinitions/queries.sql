-- name: CreateAgentDefinition :one
INSERT INTO agent_definitions (
    organization_id
  , project_id
  , name
  , tool_urn
  , model
  , title
  , description
  , instruction
  , tools
)
VALUES (
    @organization_id
  , @project_id
  , @name
  , @tool_urn
  , @model
  , NULLIF(@title, '')
  , @description
  , @instruction
  , @tools
)
RETURNING *;

-- name: GetAgentDefinition :one
SELECT *
FROM agent_definitions
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: GetAgentDefinitionByName :one
SELECT *
FROM agent_definitions
WHERE name = @name
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: ListAgentDefinitions :many
SELECT *
FROM agent_definitions
WHERE project_id = @project_id
  AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: UpdateAgentDefinition :one
UPDATE agent_definitions
SET
    model = COALESCE(NULLIF(@model, ''), model)
  , title = COALESCE(NULLIF(@title, ''), title)
  , description = COALESCE(NULLIF(@description, ''), description)
  , instruction = COALESCE(NULLIF(@instruction, ''), instruction)
  , tools = COALESCE(@tools, tools)
  , updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: DeleteAgentDefinition :exec
UPDATE agent_definitions
SET deleted_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE;
