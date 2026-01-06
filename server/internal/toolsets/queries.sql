-- name: GetToolset :one
SELECT *
FROM toolsets
WHERE slug = @slug AND project_id = @project_id AND deleted IS FALSE;

-- name: GetToolsetByMCPSlug :one
-- project_id is required to ensure uniqueness since mcp_slug is only unique within a project
SELECT *
FROM toolsets
WHERE mcp_slug = @mcp_slug AND project_id = @project_id AND deleted IS FALSE;

-- name: CreateToolset :one
INSERT INTO toolsets (
    organization_id
  , project_id
  , name
  , slug
  , description
  , default_environment_slug
  , mcp_slug
  , mcp_enabled
) VALUES (
    @organization_id
  , @project_id
  , @name
  , @slug
  , @description
  , @default_environment_slug
  , @mcp_slug
  , @mcp_enabled
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
  , default_environment_slug = COALESCE(@default_environment_slug, default_environment_slug)
  , mcp_slug = COALESCE(@mcp_slug, mcp_slug)
  , mcp_is_public = COALESCE(@mcp_is_public, mcp_is_public)
  , custom_domain_id = COALESCE(@custom_domain_id, custom_domain_id)
  , mcp_enabled = COALESCE(@mcp_enabled, mcp_enabled)
  , tool_selection_mode = COALESCE(@tool_selection_mode, tool_selection_mode)
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
WITH ranked_templates AS (
  SELECT
    pt.*,
    ROW_NUMBER() OVER (PARTITION BY pt.history_id ORDER BY pt.id DESC) as rn
  FROM prompt_templates pt
  WHERE project_id = @project_id
    AND pt.deleted IS FALSE
)
SELECT rel.id as tp_id, rt.*
FROM toolset_prompts rel
JOIN ranked_templates rt ON (
  (rel.prompt_template_id IS NOT NULL AND rt.id = rel.prompt_template_id)
  OR
  (rel.prompt_template_id IS NULL AND rt.history_id = rel.prompt_history_id AND rt.rn = 1)
)
WHERE rel.toolset_id = @toolset_id
  AND rel.project_id = @project_id
ORDER BY rel.prompt_name;

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

-- name: CheckMCPSlugAvailability :one
SELECT EXISTS (
  SELECT 1
  FROM toolsets
  WHERE mcp_slug = @mcp_slug
  AND deleted IS FALSE
);

-- name: ListEnabledToolsetsByOrganization :many
SELECT t.*
FROM toolsets t
JOIN projects p ON t.project_id = p.id
WHERE p.organization_id = @organization_id
  AND t.mcp_enabled IS TRUE
  AND t.deleted IS FALSE
  AND p.deleted IS FALSE
ORDER BY t.created_at DESC;

-- name: UpdateToolsetExternalOAuthServer :one
UPDATE toolsets
SET
    external_oauth_server_id = @external_oauth_server_id
  , updated_at = clock_timestamp()
WHERE slug = @slug AND project_id = @project_id
RETURNING *;

-- name: UpdateToolsetOAuthProxyServer :one
UPDATE toolsets
SET
    oauth_proxy_server_id = @oauth_proxy_server_id
  , updated_at = clock_timestamp()
WHERE slug = @slug AND project_id = @project_id
RETURNING *;

-- name: ClearToolsetOAuthServers :one
UPDATE toolsets
SET
    external_oauth_server_id = NULL
  , oauth_proxy_server_id = NULL
  , updated_at = clock_timestamp()
WHERE slug = @slug AND project_id = @project_id
RETURNING *;

-- name: GetPromptTemplateUrnsByNames :many
SELECT DISTINCT pt.tool_urn
FROM prompt_templates pt
WHERE pt.name = ANY(@template_names::TEXT[])
  AND pt.project_id = @project_id
  AND pt.deleted IS FALSE
  AND pt.tool_urn IS NOT NULL
ORDER BY pt.tool_urn;

-- name: CreateToolsetVersion :one
INSERT INTO toolset_versions (
    toolset_id
  , version
  , tool_urns
  , resource_urns
  , predecessor_id
) VALUES (
    @toolset_id
  , @version
  , @tool_urns
  , @resource_urns
  , @predecessor_id
)
RETURNING *;

-- name: GetLatestToolsetVersion :one
SELECT *
FROM toolset_versions
WHERE toolset_id = @toolset_id
  AND deleted IS FALSE
ORDER BY version DESC
LIMIT 1;

-- name: GetToolsetPromptTemplateNames :many
SELECT tp.prompt_name
FROM toolset_prompts tp
WHERE tp.toolset_id = @toolset_id
  AND tp.project_id = @project_id
ORDER BY tp.prompt_name;

-- name: GetToolsetsByToolURN :many
SELECT
    t.*,
    tv.version as latest_version
FROM toolsets t
JOIN toolset_versions tv ON t.id = tv.toolset_id
WHERE t.project_id = @project_id
  AND t.deleted IS FALSE
  AND tv.deleted IS FALSE
  AND @tool_urn::TEXT = ANY(tv.tool_urns)
  AND tv.version = (
    SELECT MAX(version)
    FROM toolset_versions tv2
    WHERE tv2.toolset_id = t.id
      AND tv2.deleted IS FALSE
  );

-- ============================================================================
-- Draft/Staging Workflow Queries
-- ============================================================================

-- name: UpdateToolsetIterationMode :one
UPDATE toolsets
SET
    iteration_mode = @iteration_mode
  , updated_at = clock_timestamp()
WHERE id = @id AND deleted IS FALSE
RETURNING *;

-- name: UpdateToolsetDraftState :one
UPDATE toolsets
SET
    has_draft_changes = @has_draft_changes
  , updated_at = clock_timestamp()
WHERE id = @id AND deleted IS FALSE
RETURNING *;

-- name: GetDraftToolsetVersion :one
SELECT *
FROM draft_toolset_versions
WHERE toolset_id = @toolset_id AND deleted IS FALSE;

-- name: CreateDraftToolsetVersion :one
INSERT INTO draft_toolset_versions (
    toolset_id
  , tool_urns
  , resource_urns
) VALUES (
    @toolset_id
  , @tool_urns
  , @resource_urns
)
RETURNING *;

-- name: UpdateDraftToolsetVersion :one
UPDATE draft_toolset_versions
SET
    tool_urns = @tool_urns
  , resource_urns = @resource_urns
  , updated_at = clock_timestamp()
WHERE toolset_id = @toolset_id AND deleted IS FALSE
RETURNING *;

-- name: DeleteDraftToolsetVersion :exec
UPDATE draft_toolset_versions
SET deleted_at = clock_timestamp()
WHERE toolset_id = @toolset_id AND deleted IS FALSE;

-- name: GetDraftToolVariations :many
SELECT *
FROM draft_tool_variations
WHERE draft_version_id = @draft_version_id AND deleted IS FALSE;

-- name: UpsertDraftToolVariation :one
INSERT INTO draft_tool_variations (
    draft_version_id
  , src_tool_urn
  , src_tool_name
  , confirm
  , confirm_prompt
  , name
  , summary
  , description
  , tags
  , summarizer
) VALUES (
    @draft_version_id
  , @src_tool_urn
  , @src_tool_name
  , @confirm
  , @confirm_prompt
  , @name
  , @summary
  , @description
  , @tags
  , @summarizer
)
ON CONFLICT (draft_version_id, src_tool_urn) WHERE deleted IS FALSE
DO UPDATE SET
    src_tool_name = EXCLUDED.src_tool_name
  , confirm = EXCLUDED.confirm
  , confirm_prompt = EXCLUDED.confirm_prompt
  , name = EXCLUDED.name
  , summary = EXCLUDED.summary
  , description = EXCLUDED.description
  , tags = EXCLUDED.tags
  , summarizer = EXCLUDED.summarizer
  , updated_at = clock_timestamp()
RETURNING *;

-- name: DeleteDraftToolVariations :exec
UPDATE draft_tool_variations
SET deleted_at = clock_timestamp()
WHERE draft_version_id = @draft_version_id AND deleted IS FALSE;

-- name: DeleteDraftToolVariation :exec
UPDATE draft_tool_variations
SET deleted_at = clock_timestamp()
WHERE draft_version_id = @draft_version_id
  AND src_tool_urn = @src_tool_urn
  AND deleted IS FALSE;
