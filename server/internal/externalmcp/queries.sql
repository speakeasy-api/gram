-- name: CreatePeer :one
INSERT INTO peered_organizations (super_organization_id, sub_organization_id)
VALUES (@super_organization_id, @sub_organization_id)
ON CONFLICT (super_organization_id, sub_organization_id) DO UPDATE SET
  super_organization_id = EXCLUDED.super_organization_id
RETURNING id, super_organization_id, sub_organization_id, created_at;

-- name: ListPeers :many
SELECT p.id, p.super_organization_id, p.sub_organization_id, p.created_at,
       o.name as sub_organization_name, o.slug as sub_organization_slug
FROM peered_organizations p
JOIN organization_metadata o ON p.sub_organization_id = o.id
WHERE p.super_organization_id = @super_organization_id
ORDER BY p.created_at ASC;

-- name: DeletePeer :exec
DELETE FROM peered_organizations
WHERE super_organization_id = @super_organization_id
  AND sub_organization_id = @sub_organization_id;

-- name: IsPeer :one
SELECT EXISTS (
  SELECT 1 FROM peered_organizations
  WHERE super_organization_id = @super_organization_id
    AND sub_organization_id = @sub_organization_id
) AS is_peer;

-- name: ListMCPRegistries :many
SELECT id, name, url, slug, source, visibility, organization_id, project_id, created_at, updated_at
FROM mcp_registries
WHERE deleted IS FALSE
ORDER BY name ASC;

-- name: GetMCPRegistryByID :one
SELECT id, name, url, slug, source, visibility, organization_id, project_id, created_at, updated_at
FROM mcp_registries
WHERE id = @id AND deleted IS FALSE;

-- name: GetMCPRegistryBySlug :one
SELECT id, name, url, slug, source, visibility, organization_id, project_id, created_at, updated_at
FROM mcp_registries
WHERE slug = @slug AND deleted IS FALSE;

-- name: CreateInternalRegistry :one
INSERT INTO mcp_registries (name, slug, source, visibility, organization_id, project_id)
VALUES (@name, @slug, 'internal', @visibility, @organization_id, @project_id)
RETURNING id, name, url, slug, source, visibility, organization_id, project_id, created_at, updated_at;

-- name: SetRegistryToolsets :exec
WITH deleted AS (
  DELETE FROM mcp_registry_toolset_links WHERE registry_id = @registry_id
)
INSERT INTO mcp_registry_toolset_links (registry_id, toolset_id)
SELECT @registry_id, unnest(@toolset_ids::uuid[]);

-- name: ListRegistryToolsetLinks :many
SELECT id, registry_id, toolset_id, created_at
FROM mcp_registry_toolset_links
WHERE registry_id = @registry_id
ORDER BY created_at ASC;

-- name: CreateRegistryGrant :one
INSERT INTO mcp_registry_grants (registry_id, organization_id)
VALUES (@registry_id, @organization_id)
ON CONFLICT (registry_id, organization_id) DO UPDATE SET
  registry_id = EXCLUDED.registry_id
RETURNING id, registry_id, organization_id, created_at;

-- name: DeleteRegistryGrant :exec
DELETE FROM mcp_registry_grants
WHERE registry_id = @registry_id AND organization_id = @organization_id;

-- name: CheckRegistryGrant :one
SELECT EXISTS (
  SELECT 1 FROM mcp_registry_grants
  WHERE registry_id = @registry_id AND organization_id = @organization_id
) AS has_grant;

-- name: ListRegistriesForOrganization :many
SELECT r.id, r.name, r.url, r.slug, r.source, r.visibility, r.organization_id, r.project_id, r.created_at, r.updated_at
FROM mcp_registries r
WHERE r.deleted IS FALSE
  AND (
    r.visibility = 'public'
    OR r.organization_id = @organization_id
    OR EXISTS (
      SELECT 1 FROM mcp_registry_grants g
      WHERE g.registry_id = r.id AND g.organization_id = @organization_id
    )
  )
ORDER BY r.name ASC;

-- name: GetToolsetForServe :one
SELECT t.id, t.name, t.slug, t.description, t.mcp_slug, t.mcp_enabled, t.organization_id, t.project_id
FROM toolsets t
WHERE t.id = @id AND t.deleted IS FALSE;

-- name: GetRegistryToolsetByMCPSlug :one
SELECT t.id, t.name, t.slug, t.description, t.mcp_slug, t.mcp_enabled, t.organization_id, t.project_id
FROM toolsets t
JOIN mcp_registry_toolset_links l ON l.toolset_id = t.id
WHERE l.registry_id = @registry_id AND t.mcp_slug = @mcp_slug AND t.deleted IS FALSE;

-- name: CreateExternalMCPAttachment :one
INSERT INTO external_mcp_attachments (deployment_id, registry_id, name, slug, registry_server_specifier)
VALUES (@deployment_id, @registry_id, @name, @slug, @registry_server_specifier)
RETURNING id, deployment_id, registry_id, name, slug, registry_server_specifier, created_at, updated_at;

-- name: ListExternalMCPAttachments :many
SELECT id, deployment_id, registry_id, name, slug, registry_server_specifier, created_at, updated_at
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
  e.name as registry_server_name,
  e.slug,
  e.registry_server_specifier
FROM external_mcp_tool_definitions t
JOIN external_mcp_attachments e ON t.external_mcp_attachment_id = e.id
WHERE e.deployment_id = @deployment_id
  AND t.requires_oauth = TRUE
  AND t.deleted IS FALSE
  AND e.deleted IS FALSE;
