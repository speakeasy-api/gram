-- name: ListMCPRegistries :many
SELECT id, name, url, created_at, updated_at
FROM mcp_registries
WHERE deleted IS FALSE
ORDER BY name ASC;

-- name: GetMCPRegistryByID :one
SELECT id, name, url, created_at, updated_at
FROM mcp_registries
WHERE id = @id AND deleted IS FALSE;

-- name: CreateOrganizationMcpCollection :one
INSERT INTO organization_mcp_collections (organization_id, name, description, slug, visibility)
VALUES (@organization_id, @name, @description, @slug, @visibility)
RETURNING id, organization_id, name, description, slug, visibility, created_at, updated_at;

-- name: GetOrganizationMcpCollectionByID :one
SELECT id, organization_id, name, description, slug, visibility, created_at, updated_at
FROM organization_mcp_collections
WHERE id = @id AND deleted IS FALSE;

-- name: GetOrganizationMcpCollectionBySlugAndOrg :one
SELECT id, organization_id, name, description, slug, visibility, created_at, updated_at
FROM organization_mcp_collections
WHERE slug = @slug AND organization_id = @organization_id AND deleted IS FALSE;

-- name: ListOrganizationMcpCollections :many
SELECT id, organization_id, name, description, slug, visibility, created_at, updated_at
FROM organization_mcp_collections
WHERE organization_id = @organization_id AND deleted IS FALSE
ORDER BY name ASC;

-- name: UpdateOrganizationMcpCollection :one
UPDATE organization_mcp_collections
SET name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    visibility = COALESCE(sqlc.narg('visibility'), visibility),
    updated_at = clock_timestamp()
WHERE id = @id AND deleted IS FALSE
RETURNING id, organization_id, name, description, slug, visibility, created_at, updated_at;

-- name: DeleteOrganizationMcpCollection :exec
UPDATE organization_mcp_collections SET deleted_at = clock_timestamp()
WHERE id = @id AND deleted IS FALSE;

-- name: DeleteOrganizationMcpCollectionRegistriesByID :exec
UPDATE organization_mcp_collection_registries SET deleted_at = clock_timestamp()
WHERE collection_id = @collection_id AND deleted IS FALSE;

-- name: DeleteOrganizationMcpCollectionServerAttachmentsByID :exec
UPDATE organization_mcp_collection_server_attachments SET deleted_at = clock_timestamp()
WHERE collection_id = @collection_id AND deleted IS FALSE;

-- name: CreateOrganizationMcpCollectionRegistry :one
INSERT INTO organization_mcp_collection_registries (collection_id, namespace)
VALUES (@collection_id, @namespace)
RETURNING *;

-- name: GetOrganizationMcpCollectionRegistryByID :one
SELECT * FROM organization_mcp_collection_registries
WHERE collection_id = @collection_id AND deleted IS FALSE;

-- name: GetOrganizationMcpCollectionRegistryByNamespace :one
SELECT * FROM organization_mcp_collection_registries
WHERE namespace = @namespace AND deleted IS FALSE;

-- name: AttachServerToOrganizationMcpCollection :one
INSERT INTO organization_mcp_collection_server_attachments (collection_id, toolset_id, published_by)
VALUES (@collection_id, @toolset_id, @published_by)
ON CONFLICT (collection_id, toolset_id) WHERE deleted IS FALSE DO UPDATE
SET published_by = EXCLUDED.published_by, published_at = clock_timestamp(), deleted_at = NULL, updated_at = clock_timestamp()
RETURNING *;

-- name: DetachServerFromOrganizationMcpCollection :exec
UPDATE organization_mcp_collection_server_attachments SET deleted_at = clock_timestamp()
WHERE collection_id = @collection_id AND toolset_id = @toolset_id AND deleted IS FALSE;

-- name: ListOrganizationMcpCollectionServerAttachments :many
SELECT t.* FROM toolsets t
JOIN organization_mcp_collection_server_attachments rt ON t.id = rt.toolset_id
WHERE rt.collection_id = @collection_id AND rt.deleted IS FALSE AND t.mcp_enabled IS TRUE AND t.deleted IS FALSE
ORDER BY rt.published_at DESC;

-- name: IsServerAttachedToOrganizationMcpCollection :one
SELECT EXISTS (
  SELECT 1 FROM organization_mcp_collection_server_attachments
  WHERE collection_id = @collection_id AND toolset_id = @toolset_id AND deleted IS FALSE
);

-- name: IsAttachedServerOwnedByProject :one
SELECT EXISTS (
  SELECT 1 FROM organization_mcp_collection_server_attachments rt
  JOIN toolsets t ON rt.toolset_id = t.id
  WHERE rt.collection_id = @collection_id
    AND t.mcp_slug = @mcp_slug
    AND t.project_id = @project_id
    AND rt.deleted IS FALSE
    AND t.deleted IS FALSE
);

-- name: IsToolsetInstalledFromCatalog :one
SELECT EXISTS (
  SELECT 1 FROM toolset_versions tv, unnest(tv.tool_urns) AS urn
  WHERE tv.toolset_id = @toolset_id AND tv.deleted IS FALSE
  AND urn LIKE 'tools:externalmcp:%'
  AND tv.version = (
    SELECT MAX(tv2.version) FROM toolset_versions tv2
    WHERE tv2.toolset_id = @toolset_id AND tv2.deleted IS FALSE
  )
);

-- name: CreateExternalMCPAttachment :one
INSERT INTO external_mcp_attachments (deployment_id, registry_id, organization_mcp_collection_registry_id, name, slug, registry_server_specifier)
VALUES (@deployment_id, @registry_id, @organization_mcp_collection_registry_id, @name, @slug, @registry_server_specifier)
RETURNING id, deployment_id, registry_id, organization_mcp_collection_registry_id, name, slug, registry_server_specifier, created_at, updated_at;

-- name: ListExternalMCPAttachments :many
SELECT id, deployment_id, registry_id, organization_mcp_collection_registry_id, name, slug, registry_server_specifier, created_at, updated_at
FROM external_mcp_attachments
WHERE deployment_id = @deployment_id AND deleted IS FALSE
ORDER BY created_at ASC;

-- name: CreateExternalMCPToolDefinition :one
INSERT INTO external_mcp_tool_definitions (
  external_mcp_attachment_id,
  tool_urn,
  type,
  name,
  description,
  schema,
  remote_url,
  transport_type,
  requires_oauth,
  oauth_version,
  oauth_authorization_endpoint,
  oauth_token_endpoint,
  oauth_registration_endpoint,
  oauth_scopes_supported,
  header_definitions,
  title,
  read_only_hint,
  destructive_hint,
  idempotent_hint,
  open_world_hint
)
VALUES (
  @external_mcp_attachment_id,
  @tool_urn,
  @type,
  @name,
  @description,
  @schema,
  @remote_url,
  @transport_type,
  @requires_oauth,
  @oauth_version,
  @oauth_authorization_endpoint,
  @oauth_token_endpoint,
  @oauth_registration_endpoint,
  @oauth_scopes_supported,
  @header_definitions,
  @title,
  @read_only_hint,
  @destructive_hint,
  @idempotent_hint,
  @open_world_hint
)
RETURNING id, external_mcp_attachment_id, tool_urn, type, name, description, schema, remote_url, requires_oauth,
  oauth_version, oauth_authorization_endpoint, oauth_token_endpoint,
  oauth_registration_endpoint, oauth_scopes_supported, header_definitions,
  title, read_only_hint, destructive_hint, idempotent_hint, open_world_hint,
  created_at, updated_at;

-- name: ListExternalMCPToolDefinitions :many
SELECT
  t.id,
  t.external_mcp_attachment_id,
  t.tool_urn,
  t.type,
  t.name,
  t.description,
  t.schema,
  t.remote_url,
  t.transport_type,
  t.requires_oauth,
  t.oauth_version,
  t.oauth_authorization_endpoint,
  t.oauth_token_endpoint,
  t.oauth_registration_endpoint,
  t.oauth_scopes_supported,
  t.header_definitions,
  t.title,
  t.read_only_hint,
  t.destructive_hint,
  t.idempotent_hint,
  t.open_world_hint,
  t.created_at,
  t.updated_at,
  e.deployment_id,
  e.registry_id,
  e.organization_mcp_collection_registry_id,
  e.name as registry_server_name,
  e.slug,
  e.registry_server_specifier
FROM external_mcp_tool_definitions t
JOIN external_mcp_attachments e ON t.external_mcp_attachment_id = e.id
WHERE e.deployment_id = @deployment_id
  AND t.deleted IS FALSE
  AND e.deleted IS FALSE
ORDER BY e.slug ASC;

-- name: GetExternalMCPToolDefinitionByURN :one
WITH deployment AS (
    SELECT d.id
    FROM deployments d
    JOIN deployment_statuses ds ON d.id = ds.deployment_id
    WHERE d.project_id = @project_id
    AND ds.status = 'completed'
    ORDER BY d.seq DESC
    LIMIT 1
)
SELECT
  t.id,
  t.external_mcp_attachment_id,
  t.tool_urn,
  t.type,
  t.name,
  t.description,
  t.schema,
  t.remote_url,
  t.transport_type,
  t.requires_oauth,
  t.oauth_version,
  t.oauth_authorization_endpoint,
  t.oauth_token_endpoint,
  t.oauth_registration_endpoint,
  t.oauth_scopes_supported,
  t.header_definitions,
  t.title,
  t.read_only_hint,
  t.destructive_hint,
  t.idempotent_hint,
  t.open_world_hint,
  t.created_at,
  t.updated_at,
  e.deployment_id,
  e.registry_id,
  e.organization_mcp_collection_registry_id,
  e.name as registry_server_name,
  e.slug,
  e.registry_server_specifier
FROM external_mcp_tool_definitions t
JOIN external_mcp_attachments e ON t.external_mcp_attachment_id = e.id
WHERE t.tool_urn = @tool_urn
  AND e.deployment_id = (SELECT id FROM deployment)
  AND t.deleted IS FALSE
  AND e.deleted IS FALSE;

-- name: GetExternalMCPToolsRequiringOAuth :many
SELECT
  t.id,
  t.external_mcp_attachment_id,
  t.tool_urn,
  t.type,
  t.name,
  t.description,
  t.schema,
  t.remote_url,
  t.requires_oauth,
  t.oauth_version,
  t.oauth_authorization_endpoint,
  t.oauth_token_endpoint,
  t.oauth_registration_endpoint,
  t.oauth_scopes_supported,
  t.header_definitions,
  t.title,
  t.read_only_hint,
  t.destructive_hint,
  t.idempotent_hint,
  t.open_world_hint,
  t.created_at,
  t.updated_at,
  e.deployment_id,
  e.registry_id,
  e.organization_mcp_collection_registry_id,
  e.name as registry_server_name,
  e.slug,
  e.registry_server_specifier
FROM external_mcp_tool_definitions t
JOIN external_mcp_attachments e ON t.external_mcp_attachment_id = e.id
WHERE e.deployment_id = @deployment_id
  AND t.requires_oauth = TRUE
  AND t.deleted IS FALSE
  AND e.deleted IS FALSE;
