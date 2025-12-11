-- name: ListMCPRegistries :many
SELECT id, name, url, created_at, updated_at
FROM mcp_registries
WHERE deleted IS FALSE
ORDER BY name ASC;

-- name: GetMCPRegistry :one
SELECT id, name, url, created_at, updated_at
FROM mcp_registries
WHERE id = $1 AND deleted IS FALSE;

-- name: CreateDeploymentExternalMCP :one
INSERT INTO deployments_external_mcps (deployment_id, registry_id, name, slug)
VALUES ($1, $2, $3, $4)
RETURNING id, deployment_id, registry_id, name, slug, created_at, updated_at;

-- name: ListDeploymentExternalMCPs :many
SELECT id, deployment_id, registry_id, name, slug, created_at, updated_at
FROM deployments_external_mcps
WHERE deployment_id = $1 AND deleted IS FALSE
ORDER BY created_at ASC;

-- name: CreateExternalMCPToolDefinition :one
INSERT INTO external_mcp_tool_definitions (deployment_external_mcp_id, tool_urn, remote_url, requires_oauth, authenticate_header)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, deployment_external_mcp_id, tool_urn, remote_url, requires_oauth, authenticate_header, created_at, updated_at;

-- name: ListExternalMCPToolDefinitions :many
SELECT
  t.id,
  t.deployment_external_mcp_id,
  t.tool_urn,
  t.remote_url,
  t.requires_oauth,
  t.authenticate_header,
  t.created_at,
  t.updated_at,
  e.deployment_id,
  e.registry_id,
  e.name,
  e.slug
FROM external_mcp_tool_definitions t
JOIN deployments_external_mcps e ON t.deployment_external_mcp_id = e.id
WHERE e.deployment_id = $1
  AND t.deleted IS FALSE
  AND e.deleted IS FALSE
ORDER BY e.slug ASC;

-- name: GetExternalMCPToolDefinitionByURN :one
SELECT
  t.id,
  t.deployment_external_mcp_id,
  t.tool_urn,
  t.remote_url,
  t.requires_oauth,
  t.authenticate_header,
  t.created_at,
  t.updated_at,
  e.deployment_id,
  e.registry_id,
  e.name,
  e.slug
FROM external_mcp_tool_definitions t
JOIN deployments_external_mcps e ON t.deployment_external_mcp_id = e.id
WHERE t.tool_urn = $1
  AND t.deleted IS FALSE
  AND e.deleted IS FALSE;

-- name: GetExternalMCPToolsRequiringOAuth :many
SELECT
  t.id,
  t.deployment_external_mcp_id,
  t.tool_urn,
  t.remote_url,
  t.requires_oauth,
  t.authenticate_header,
  t.created_at,
  t.updated_at,
  e.deployment_id,
  e.registry_id,
  e.name,
  e.slug
FROM external_mcp_tool_definitions t
JOIN deployments_external_mcps e ON t.deployment_external_mcp_id = e.id
WHERE e.deployment_id = $1
  AND t.requires_oauth = TRUE
  AND t.deleted IS FALSE
  AND e.deleted IS FALSE;
