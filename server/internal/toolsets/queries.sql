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
  , http_tool_names
  , default_environment_slug
) VALUES (
    @organization_id
  , @project_id
  , @name
  , @slug
  , @description
  , COALESCE(@http_tool_names::text[], '{}'::text[])
  , @default_environment_slug
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
  , http_tool_names = COALESCE(@http_tool_names::text[], http_tool_names)
  , default_environment_slug = COALESCE(@default_environment_slug, default_environment_slug)
  , updated_at = clock_timestamp()
WHERE slug = @slug AND project_id = @project_id
RETURNING *;

-- name: DeleteToolset :exec
UPDATE toolsets
SET deleted_at = clock_timestamp()
WHERE slug = @slug
  AND project_id = @project_id AND deleted IS FALSE;

-- name: GetHTTPSecurityDefinitions :many
SELECT *
FROM http_security
WHERE key = ANY(@security_keys::TEXT[]) AND deployment_id = ANY(@deployment_ids::UUID[]);