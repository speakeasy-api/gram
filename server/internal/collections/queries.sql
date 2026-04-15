-- name: CreateOrganizationMcpCollection :one
INSERT INTO organization_mcp_collections (organization_id, name, description, slug, visibility)
VALUES
  (@organization_id, @name, @description, @slug, @visibility)
RETURNING id, organization_id, name, description, slug, visibility, created_at, updated_at;

-- name: GetOrganizationMcpCollectionByID :one
SELECT id, organization_id, name, description, slug, visibility, created_at, updated_at
FROM organization_mcp_collections
WHERE
  id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE;

-- name: GetOrganizationMcpCollectionBySlugAndOrg :one
SELECT id, organization_id, name, description, slug, visibility, created_at, updated_at
FROM organization_mcp_collections
WHERE
  slug = @slug
  AND organization_id = @organization_id
  AND deleted IS FALSE;

-- name: ListOrganizationMcpCollections :many
SELECT id, organization_id, name, description, slug, visibility, created_at, updated_at
FROM organization_mcp_collections
WHERE
  organization_id = @organization_id
  AND deleted IS FALSE
ORDER BY name ASC;

-- name: UpdateOrganizationMcpCollection :one
UPDATE organization_mcp_collections
SET name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    visibility = COALESCE(sqlc.narg('visibility'), visibility),
    updated_at = clock_timestamp()
WHERE
  id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE
RETURNING id, organization_id, name, description, slug, visibility, created_at, updated_at;

-- name: DeleteOrganizationMcpCollection :exec
UPDATE organization_mcp_collections SET deleted_at = clock_timestamp()
WHERE
  id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE;

-- name: DeleteOrganizationMcpCollectionRegistriesByID :exec
WITH org_collection AS (
  SELECT omc.id FROM organization_mcp_collections omc
  WHERE omc.id = @collection_id AND omc.organization_id = @organization_id AND omc.deleted IS FALSE
)
UPDATE organization_mcp_collection_registries SET deleted_at = clock_timestamp()
WHERE collection_id = (SELECT id FROM org_collection) AND deleted IS FALSE;

-- name: DeleteOrganizationMcpCollectionServerAttachmentsByID :exec
WITH org_collection AS (
  SELECT omc.id FROM organization_mcp_collections omc
  WHERE omc.id = @collection_id AND omc.organization_id = @organization_id AND omc.deleted IS FALSE
)
UPDATE organization_mcp_collection_server_attachments SET deleted_at = clock_timestamp()
WHERE collection_id = (SELECT id FROM org_collection) AND deleted IS FALSE;

-- name: CreateOrganizationMcpCollectionRegistry :one
WITH org_collection AS (
  SELECT omc.id FROM organization_mcp_collections omc
  WHERE omc.id = @collection_id AND omc.organization_id = @organization_id AND omc.deleted IS FALSE
)
INSERT INTO organization_mcp_collection_registries (collection_id, namespace)
SELECT id, @namespace
FROM org_collection
RETURNING *;

-- name: GetOrganizationMcpCollectionRegistryByID :one
SELECT r.* FROM organization_mcp_collection_registries r
JOIN organization_mcp_collections c ON c.id = r.collection_id
WHERE
  r.collection_id = @collection_id
  AND c.organization_id = @organization_id
  AND c.deleted IS FALSE
  AND r.deleted IS FALSE;

-- name: GetOrganizationMcpCollectionRegistryByNamespace :one
SELECT r.* FROM organization_mcp_collection_registries r
JOIN organization_mcp_collections c ON c.id = r.collection_id
WHERE
  r.namespace = @namespace
  AND c.organization_id = @organization_id
  AND c.deleted IS FALSE
  AND r.deleted IS FALSE;

-- name: AttachServerToOrganizationMcpCollection :one
WITH org_collection AS (
  SELECT omc.id FROM organization_mcp_collections omc
  WHERE omc.id = @collection_id AND omc.organization_id = @organization_id AND omc.deleted IS FALSE
)
INSERT INTO organization_mcp_collection_server_attachments (collection_id, toolset_id, published_by)
SELECT id, @toolset_id, @published_by
FROM org_collection
ON CONFLICT (collection_id, toolset_id) WHERE deleted IS FALSE DO UPDATE
SET published_by = EXCLUDED.published_by, published_at = clock_timestamp(), deleted_at = NULL, updated_at = clock_timestamp()
RETURNING *;

-- name: DetachServerFromOrganizationMcpCollection :exec
WITH org_collection AS (
  SELECT omc.id FROM organization_mcp_collections omc
  WHERE omc.id = @collection_id AND omc.organization_id = @organization_id AND omc.deleted IS FALSE
)
UPDATE organization_mcp_collection_server_attachments SET deleted_at = clock_timestamp()
WHERE
  collection_id = (SELECT id FROM org_collection)
  AND toolset_id = @toolset_id
  AND deleted IS FALSE;

-- name: ListOrganizationMcpCollectionServerAttachments :many
SELECT t.* FROM toolsets t
JOIN organization_mcp_collection_server_attachments rt ON t.id = rt.toolset_id
JOIN organization_mcp_collections c ON c.id = rt.collection_id
WHERE
  rt.collection_id = @collection_id
  AND c.organization_id = @organization_id
  AND c.deleted IS FALSE
  AND rt.deleted IS FALSE
  AND t.mcp_enabled IS TRUE
  AND t.deleted IS FALSE
ORDER BY rt.published_at DESC;

-- name: IsServerAttachedToOrganizationMcpCollection :one
SELECT EXISTS (
  SELECT 1 FROM organization_mcp_collection_server_attachments a
  JOIN organization_mcp_collections c ON c.id = a.collection_id
  WHERE
    a.collection_id = @collection_id
    AND c.organization_id = @organization_id
    AND c.deleted IS FALSE
    AND a.toolset_id = @toolset_id
    AND a.deleted IS FALSE
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
