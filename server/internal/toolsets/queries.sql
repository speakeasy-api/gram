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
  , mcp_slug = COALESCE(@mcp_slug, mcp_slug)
  , mcp_is_public = COALESCE(@mcp_is_public, mcp_is_public)
  , custom_domain_id = COALESCE(@custom_domain_id, custom_domain_id)
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

-- name: GetToolsetByMcpSlug :one
SELECT *
FROM toolsets
WHERE mcp_slug = @mcp_slug
  AND custom_domain_id IS NULL
  AND deleted IS FALSE;

-- name: GetToolsetByMcpSlugAndCustomDomain :one
SELECT *
FROM toolsets
WHERE mcp_slug = @mcp_slug
  AND custom_domain_id = @custom_domain_id
  AND deleted IS FALSE;

-- name: GetPromptTemplatesForToolset :many
SELECT rel.id, sqlc.embed(pt)
FROM toolset_prompts rel
INNER JOIN prompt_templates pt ON rel.project_id = pt.project_id AND rel.prompt_history_id = pt.history_id
WHERE 
  rel.project_id = @project_id
  AND rel.toolset_id = @toolset_id
  AND (rel.prompt_template_id IS NULL OR pt.id = rel.prompt_template_id);

-- name: ClearToolsetPromptTemplates :exec
DELETE FROM toolset_prompts
WHERE project_id = @project_id
  AND toolset_id = @toolset_id;


-- name: AddToolsetPromptTemplates :copyfrom
INSERT INTO toolset_prompts (
    project_id
  , toolset_id
  , prompt_history_id
  , prompt_template_id
  , prompt_name
) VALUES (@project_id, @toolset_id, @prompt_history_id, @prompt_template_id, @prompt_name);