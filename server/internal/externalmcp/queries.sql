-- name: ListMCPRegistries :many
SELECT id, name, url, created_at, updated_at
FROM mcp_registries
WHERE deleted IS FALSE
ORDER BY name ASC;

-- name: GetMCPRegistryByID :one
SELECT id, name, url, created_at, updated_at
FROM mcp_registries
WHERE id = $1 AND deleted IS FALSE;
