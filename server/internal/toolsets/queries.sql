-- name: GetToolset :one
SELECT *
FROM toolsets
WHERE slug = @slug AND project_id = @project_id AND deleted IS FALSE;

-- name: GetToolsetForUpdate :one
SELECT *
FROM toolsets
WHERE slug = @slug AND project_id = @project_id AND deleted IS FALSE
FOR UPDATE;

-- name: GetToolsetByIDAndProject :one
SELECT *
FROM toolsets
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE;

-- name: GetToolsetByIDAndOrganization :one
SELECT *
FROM toolsets
WHERE id = @id AND organization_id = @organization_id AND deleted IS FALSE;

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

-- name: CreateToolsetOrigin :one
INSERT INTO toolset_origins (
    organization_id
  , toolset_id
  , origin_registry_specifier
) VALUES (
    @organization_id
  , @toolset_id
  , @registry_specifier
)
RETURNING
    id
  , toolset_id
  , origin_registry_specifier AS registry_specifier
  , created_at
  , updated_at;

-- name: GetToolsetOriginByToolsetID :one
SELECT
    id
  , toolset_id
  , origin_registry_specifier AS registry_specifier
  , created_at
  , updated_at
FROM toolset_origins
WHERE toolset_id = @toolset_id
  AND organization_id = @organization_id
  AND deleted IS FALSE;

-- name: GetToolsetOriginsByToolsetIDs :many
SELECT
    id
  , toolset_id
  , origin_registry_specifier AS registry_specifier
  , created_at
  , updated_at
FROM toolset_origins
WHERE toolset_id = ANY(@toolset_ids::uuid[])
  AND organization_id = @organization_id
  AND deleted IS FALSE;

-- name: ListToolsetsByProject :many
SELECT *
FROM toolsets
WHERE project_id = @project_id
  AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: UpdateToolsetDetails :one
-- Leaves the MCP hosting columns alone: they are a frozen alias, and the canonical wrapper owns hosting state.
UPDATE toolsets
SET
    name = COALESCE(@name, name)
  , description = COALESCE(@description, description)
  , default_environment_slug = COALESCE(@default_environment_slug, default_environment_slug)
  , tool_selection_mode = COALESCE(@tool_selection_mode, tool_selection_mode)
  , updated_at = clock_timestamp()
WHERE slug = @slug AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: UpdateToolset :one
-- Test fixture: also writes the frozen MCP hosting alias columns, which production code never writes.
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
WHERE slug = @slug AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: DeleteToolset :one
UPDATE toolsets
SET deleted_at = clock_timestamp()
WHERE slug = @slug
  AND project_id = @project_id AND deleted IS FALSE
RETURNING id, name, slug;

-- name: GetHTTPSecurityDefinitions :many
SELECT *
FROM http_security
WHERE key = ANY(@security_keys::TEXT[])
  AND deployment_id = ANY(@deployment_ids::UUID[])
  AND (
    cardinality(@openapiv3_document_ids::UUID[]) = 0
    OR openapiv3_document_id = ANY(@openapiv3_document_ids::UUID[])
  );

-- name: GetToolsetByMcpSlug :one
SELECT *
FROM toolsets
WHERE mcp_slug = @mcp_slug
  AND deleted IS FALSE
ORDER BY (custom_domain_id IS NULL) DESC
LIMIT 1;

-- name: GetToolsetByPlatformMcpSlug :one
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

-- name: GetToolsetByMcpSlugAndProject :one
SELECT *
FROM toolsets
WHERE mcp_slug = @mcp_slug
  AND project_id = @project_id
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

-- name: GetPromptTemplatesForToolsets :many
WITH ranked_templates AS (
  SELECT
    pt.*,
    ROW_NUMBER() OVER (PARTITION BY pt.history_id ORDER BY pt.id DESC) as rn
  FROM prompt_templates pt
  WHERE project_id = @project_id
    AND pt.deleted IS FALSE
)
SELECT rel.toolset_id, rel.id as tp_id, rt.*
FROM toolset_prompts rel
JOIN ranked_templates rt ON (
  (rel.prompt_template_id IS NOT NULL AND rt.id = rel.prompt_template_id)
  OR
  (rel.prompt_template_id IS NULL AND rt.history_id = rel.prompt_history_id AND rt.rn = 1)
)
WHERE rel.toolset_id = ANY(@toolset_ids::uuid[])
  AND rel.project_id = @project_id
ORDER BY rel.toolset_id, rel.prompt_name;

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

-- name: ListToolsetsByOrganization :many
SELECT t.*
FROM toolsets t
JOIN projects p ON t.project_id = p.id
WHERE p.organization_id = @organization_id
  AND t.deleted IS FALSE
  AND p.deleted IS FALSE
ORDER BY t.created_at DESC;

-- name: ListToolsetsWithVersionsByOrganization :many
SELECT
  t.id,
  t.organization_id,
  t.project_id,
  t.name,
  t.slug,
  t.description,
  t.default_environment_slug,
  t.mcp_slug,
  t.mcp_is_public,
  t.mcp_enabled,
  t.tool_selection_mode,
  t.created_at,
  t.updated_at,
  COALESCE(tv.tool_urns, ARRAY[]::TEXT[]) AS latest_tool_urns,
  COALESCE(tv.resource_urns, ARRAY[]::TEXT[]) AS latest_resource_urns
FROM toolsets t
JOIN projects p ON t.project_id = p.id
LEFT JOIN LATERAL (
  SELECT tool_urns, resource_urns
  FROM toolset_versions
  WHERE toolset_id = t.id AND deleted IS FALSE
  ORDER BY version DESC
  LIMIT 1
) tv ON TRUE
WHERE p.organization_id = @organization_id
  AND t.deleted IS FALSE
  AND p.deleted IS FALSE
ORDER BY t.created_at DESC;

-- name: UpdateToolsetExternalOAuthServer :one
UPDATE toolsets
SET
    external_oauth_server_id = @external_oauth_server_id
  , updated_at = clock_timestamp()
WHERE slug = @slug
  AND project_id = @project_id
  AND deleted IS FALSE
  AND external_oauth_server_id IS NULL
RETURNING *;

-- name: UpdateToolsetUserSessionIssuer :one
-- Test fixture: production code never writes the frozen issuer alias column.
UPDATE toolsets
SET
    user_session_issuer_id = @user_session_issuer_id
  , updated_at = clock_timestamp()
WHERE slug = @slug AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: UpdateToolsetToolVariationsGroup :one
UPDATE toolsets
SET
    tool_variations_group_id = @tool_variations_group_id
  , updated_at = clock_timestamp()
WHERE slug = @slug AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: ClearToolsetOAuthServers :one
UPDATE toolsets
SET
    external_oauth_server_id = NULL
  , oauth_proxy_server_id = NULL
  , updated_at = clock_timestamp()
WHERE slug = @slug AND project_id = @project_id AND deleted IS FALSE
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

-- name: GetLatestToolsetVersionsBatch :many
SELECT DISTINCT ON (toolset_id) *
FROM toolset_versions
WHERE toolset_id = ANY(@toolset_ids::uuid[])
  AND deleted IS FALSE
ORDER BY toolset_id, version DESC;

-- name: ToolsetHasExternalMCPProxy :one
WITH latest_toolset_version AS (
  SELECT tv.tool_urns
  FROM toolsets t
  JOIN toolset_versions tv ON tv.toolset_id = t.id
  WHERE t.id = @toolset_id
    AND t.project_id = @project_id
    AND t.deleted IS FALSE
    AND tv.deleted IS FALSE
  ORDER BY tv.version DESC
  LIMIT 1
),
active_deployment AS (
  SELECT d.id
  FROM deployments d
  JOIN deployment_statuses ds ON ds.deployment_id = d.id
  WHERE d.project_id = @project_id
    AND ds.status = 'completed'
  ORDER BY d.seq DESC
  LIMIT 1
)
SELECT EXISTS (
  SELECT 1
  FROM latest_toolset_version tv
  CROSS JOIN LATERAL unnest(tv.tool_urns) AS tool_urn(value)
  JOIN external_mcp_tool_definitions etd ON etd.tool_urn = tool_urn.value
  JOIN external_mcp_attachments ea ON ea.id = etd.external_mcp_attachment_id
  WHERE ea.deployment_id = (SELECT id FROM active_deployment)
    AND etd.type = 'proxy'
    AND etd.deleted IS FALSE
    AND ea.deleted IS FALSE
);

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

-- name: GetToolsetByIDIncludingDeletedFixture :one
SELECT *
FROM toolsets
WHERE id = @id AND project_id = @project_id;

-- name: GetToolsetByMcpSlugAndProjectAndCustomDomain :one
SELECT * FROM toolsets
WHERE mcp_slug = @mcp_slug AND project_id = @project_id
  AND organization_id = @organization_id
  AND custom_domain_id IS NOT DISTINCT FROM @custom_domain_id AND deleted IS FALSE;

-- name: ListCanonicalHostingByToolsetIDs :many
-- The server row holding each toolset's id: its hosting state when it is the canonical wrapper, its preferred
-- address (the oldest platform endpoint, else the oldest endpoint on a verified, activated custom domain), and
-- the slugs of all its live endpoints. A non-canonical row means the toolset's id was kept by a server that left it.
SELECT
    s.id AS toolset_id
  , (s.toolset_id IS NOT DISTINCT FROM s.id)::boolean AS canonical
  , s.visibility
  , s.user_session_issuer_id
  , (address.slug IS NOT NULL)::boolean AS has_endpoint
  , COALESCE(address.slug, '')::text AS endpoint_slug
  , address.custom_domain_id AS endpoint_custom_domain_id
  , COALESCE((
      SELECT array_agg(e.slug ORDER BY e.slug)
      FROM mcp_endpoints e
      WHERE e.mcp_server_id = s.id
        AND e.project_id = s.project_id
        AND e.deleted IS FALSE
        AND s.toolset_id IS NOT DISTINCT FROM s.id
    ), ARRAY[]::text[])::text[] AS endpoint_slugs
FROM mcp_servers s
JOIN toolsets t
  ON t.id = s.id
 AND t.project_id = s.project_id
 AND t.organization_id = @organization_id
 AND t.deleted IS FALSE
LEFT JOIN LATERAL (
  SELECT e.slug, e.custom_domain_id
  FROM mcp_endpoints e
  LEFT JOIN custom_domains cd
    ON cd.id = e.custom_domain_id
   AND cd.organization_id = @organization_id
   AND cd.deleted IS FALSE
   AND cd.verified
   AND cd.activated
  WHERE e.mcp_server_id = s.id
    AND e.project_id = s.project_id
    AND e.deleted IS FALSE
    AND s.toolset_id IS NOT DISTINCT FROM s.id
    AND (e.custom_domain_id IS NULL OR cd.id IS NOT NULL)
  ORDER BY (e.custom_domain_id IS NULL) DESC, e.created_at, e.id
  LIMIT 1
) address ON TRUE
WHERE s.id = ANY(@toolset_ids::uuid[])
  AND s.deleted IS FALSE;

-- name: MirrorToolsetHosting :one
-- Writes a hosted toolset's hosting columns from its canonical server so pods that still read them stay consistent.
UPDATE toolsets
SET
    name = @name
  , mcp_slug = @mcp_slug
  , mcp_enabled = @mcp_enabled
  , mcp_is_public = @mcp_is_public
  , custom_domain_id = @custom_domain_id
  , user_session_issuer_id = @user_session_issuer_id
  , tool_variations_group_id = @tool_variations_group_id
  , updated_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;
