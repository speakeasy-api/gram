-- name: ListMCPRegistries :many
SELECT id, name, url, created_at, updated_at
FROM mcp_registries
WHERE deleted IS FALSE
ORDER BY name ASC;

-- name: CreateDeploymentExternalMCP :one
INSERT INTO deployments_external_mcps (deployment_id, registry_id, name, slug)
VALUES ($1, $2, $3, $4)
RETURNING id, deployment_id, registry_id, name, slug, created_at, updated_at;

-- name: ListDeploymentExternalMCPs :many
SELECT id, deployment_id, registry_id, name, slug, created_at, updated_at
FROM deployments_external_mcps
WHERE deployment_id = $1 AND deleted IS FALSE
ORDER BY created_at ASC;
