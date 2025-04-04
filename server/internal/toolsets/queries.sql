-- name: GetToolset :one
SELECT *
FROM toolsets
WHERE slug = @slug AND project_id = @project_id AND deleted IS FALSE;

-- name: CreateToolset :one
INSERT INTO toolsets (
    organization_id
  , project_id
  , name
  , slug
  , description
  , http_tool_ids
  , default_environment_id
) VALUES (
    @organization_id
  , @project_id
  , @name
  , @slug
  , @description
  , NULLIF(@http_tool_ids::uuid[], '{}'::uuid[])
  , @default_environment_id
)
RETURNING *;

-- name: ListToolsetsByProject :many
SELECT *
FROM toolsets
WHERE project_id = @project_id
  AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: UpdateToolset :one
UPDATE toolsets
SET 
    name = COALESCE(@name, name)
  , description = COALESCE(@description, description)
  , http_tool_ids = COALESCE(NULLIF(@http_tool_ids::uuid[], '{}'::uuid[]), http_tool_ids)
  , default_environment_id = COALESCE(@default_environment_id, default_environment_id)
  , updated_at = clock_timestamp()
WHERE slug = @slug AND project_id = @project_id
RETURNING *;

-- name: DeleteToolset :exec
UPDATE toolsets
SET deleted_at = clock_timestamp()
WHERE slug = @slug
  AND project_id = @project_id AND deleted IS FALSE;

-- name: GetHTTPToolDefinitions :many
SELECT *
FROM http_tool_definitions
WHERE id = ANY(@ids::uuid[])
  AND deleted IS FALSE;

