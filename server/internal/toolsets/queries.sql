-- name: GetToolset :one
SELECT 
    id
  , organization_id
  , project_id
  , name
  , slug
  , description
  , http_tool_ids
  , created_at
  , updated_at
  , deleted_at
  , deleted
FROM toolsets
WHERE id = @id AND project_id = @project_id;

-- name: CreateToolset :one
INSERT INTO toolsets (
    organization_id
  , project_id
  , name
  , slug
  , description
  , http_tool_ids
) VALUES (
    @organization_id
  , @project_id
  , @name
  , @slug
  , @description
  , NULLIF(@http_tool_ids::uuid[], '{}'::uuid[])
)
RETURNING 
    id
  , organization_id
  , project_id
  , name
  , slug
  , description
  , http_tool_ids
  , created_at
  , updated_at
  , deleted_at
  , deleted;

-- name: ListToolsetsByProject :many
SELECT 
    id
  , organization_id
  , project_id
  , name
  , slug
  , description
  , http_tool_ids
  , created_at
  , updated_at
  , deleted_at
  , deleted
FROM toolsets
WHERE project_id = @project_id
  AND deleted_at IS NULL
ORDER BY created_at DESC;

-- name: UpdateToolset :one
UPDATE toolsets
SET 
    name = COALESCE(@name, name)
  , description = COALESCE(@description, description)
  , http_tool_ids = COALESCE(NULLIF(@http_tool_ids::uuid[], '{}'::uuid[]), http_tool_ids)
  , updated_at = clock_timestamp()
WHERE id = @id
RETURNING 
    id
  , organization_id
  , project_id
  , name
  , slug
  , description
  , http_tool_ids
  , created_at
  , updated_at
  , deleted_at
  , deleted;

-- name: DeleteToolset :exec
UPDATE toolsets
SET deleted_at = clock_timestamp()
WHERE id = @id;

-- name: GetHTTPToolDefinitions :many
SELECT 
    id
  , organization_id
  , project_id
  , name
  , description
  , server_env_var
  , security_type
  , bearer_env_var
  , apikey_env_var
  , username_env_var
  , password_env_var
  , http_method
  , path
  , headers_schema
  , queries_schema
  , pathparams_schema
  , body_schema
  , created_at
  , updated_at
  , deleted_at
  , deleted
FROM http_tool_definitions
WHERE id = ANY(@ids::uuid[])
  AND deleted_at IS NULL;

