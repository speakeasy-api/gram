-- name: ListMCPRegistries :many
SELECT id, name, url, created_at, updated_at
FROM mcp_registries
WHERE deleted IS FALSE
ORDER BY name ASC;

-- name: GetMCPRegistryByID :one
SELECT id, name, url, created_at, updated_at
FROM mcp_registries
WHERE id = @id AND deleted IS FALSE;

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
  header_definitions
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
  @header_definitions
)
RETURNING id, external_mcp_attachment_id, tool_urn, type, name, description, schema, remote_url, requires_oauth,
  oauth_version, oauth_authorization_endpoint, oauth_token_endpoint,
  oauth_registration_endpoint, oauth_scopes_supported, header_definitions, created_at, updated_at;

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
