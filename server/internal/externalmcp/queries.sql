-- name: ListMCPRegistries :many
SELECT
  id,
  name,
  url,
  source_type,
  auth_profile,
  enabled,
  certification_state,
  certification_version,
  priority,
  source_key,
  created_at,
  updated_at
FROM mcp_registries
WHERE deleted IS FALSE
ORDER BY name ASC;

-- name: GetMCPRegistryByID :one
SELECT
  id,
  name,
  url,
  source_type,
  auth_profile,
  enabled,
  certification_state,
  certification_version,
  priority,
  source_key,
  created_at,
  updated_at
FROM mcp_registries
WHERE id = @id AND deleted IS FALSE;

-- name: CreateMCPRegistry :one
INSERT INTO mcp_registries (name, url)
VALUES (@name, @url)
RETURNING id;

-- name: SetMCPRegistryEnabledFixture :exec
UPDATE mcp_registries SET enabled = @enabled WHERE id = @id;

-- name: SetMCPRegistryURLFixture :exec
UPDATE mcp_registries SET url = @url WHERE id = @id;

-- name: EnsureNativeCatalogRegistry :exec
-- Never changes an existing row, including an operator-disabled native source.
INSERT INTO mcp_registries
  (id, name, url, source_type, auth_profile, enabled, certification_state, source_key)
VALUES (@id, 'Speakeasy', @url, 'native_v1', 'none', true, 'certified', 'speakeasy')
ON CONFLICT (id) DO NOTHING;

-- name: NativeCatalogRegistryMatches :one
SELECT url = @url::text AND source_type = 'native_v1' AND auth_profile = 'none' AND source_key = 'speakeasy' AS valid
FROM mcp_registries
WHERE id = @id;
