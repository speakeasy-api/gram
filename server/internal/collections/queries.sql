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
