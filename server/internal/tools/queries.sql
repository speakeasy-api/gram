-- name: ListHttpToolDefinitions :many
SELECT 
  id
, organization_id
, project_id
, openapiv3_document_id
, name
, description
, tags
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
FROM http_tool_definitions
WHERE project_id = @project_id 
  AND deleted IS FALSE
  AND (sqlc.narg(cursor)::uuid IS NULL OR id < sqlc.narg(cursor))
ORDER BY id DESC
LIMIT 100;
