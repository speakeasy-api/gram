-- name: CreateOrganizationMcpCollection :one
INSERT INTO organization_mcp_collections (organization_id, name, description, slug, visibility)
VALUES
  (@organization_id, @name, @description, @slug, @visibility)
RETURNING id, organization_id, name, description, slug, visibility, created_at, updated_at;

-- name: EnsureOrganizationMcpCollection :one
INSERT INTO organization_mcp_collections (organization_id, name, description, slug, visibility)
VALUES
  (@organization_id, @name, @description, @slug, @visibility)
ON CONFLICT (slug, organization_id) WHERE deleted IS FALSE
DO UPDATE SET updated_at = organization_mcp_collections.updated_at
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

-- name: EnsureOrganizationMcpCollectionRegistry :one
WITH org_collection AS (
  SELECT omc.id FROM organization_mcp_collections omc
  WHERE omc.id = @collection_id AND omc.organization_id = @organization_id AND omc.deleted IS FALSE
)
INSERT INTO organization_mcp_collection_registries (collection_id, namespace)
SELECT id, @namespace
FROM org_collection
ON CONFLICT (collection_id) WHERE deleted IS FALSE
DO UPDATE SET updated_at = organization_mcp_collection_registries.updated_at
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
SELECT t.*, rt.published_at AS published_at FROM toolsets t
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

-- name: GetMcpServerForOrganizationAttachment :one
-- Resolve an mcp_server for collection-publishing validation, scoped to the
-- organization through its project (mcp_servers.project_id -> projects).
-- has_endpoint reports whether the server has at least one usable endpoint so
-- the caller can reject unpublishable servers.
SELECT
  s.id,
  s.visibility,
  EXISTS (
    SELECT 1 FROM mcp_endpoints e
    WHERE e.mcp_server_id = s.id AND e.deleted IS FALSE
  ) AS has_endpoint
FROM mcp_servers s
JOIN projects p ON p.id = s.project_id
WHERE
  s.id = @mcp_server_id
  AND p.organization_id = @organization_id
  AND s.deleted IS FALSE;

-- name: AttachMcpServerToOrganizationMcpCollection :one
WITH org_collection AS (
  SELECT omc.id FROM organization_mcp_collections omc
  WHERE omc.id = @collection_id AND omc.organization_id = @organization_id AND omc.deleted IS FALSE
)
INSERT INTO organization_mcp_collection_server_attachments (collection_id, mcp_server_id, published_by)
SELECT id, @mcp_server_id, @published_by
FROM org_collection
ON CONFLICT (collection_id, mcp_server_id) WHERE deleted IS FALSE DO UPDATE
SET published_by = EXCLUDED.published_by, published_at = clock_timestamp(), deleted_at = NULL, updated_at = clock_timestamp()
RETURNING *;

-- name: DetachMcpServerFromOrganizationMcpCollection :exec
WITH org_collection AS (
  SELECT omc.id FROM organization_mcp_collections omc
  WHERE omc.id = @collection_id AND omc.organization_id = @organization_id AND omc.deleted IS FALSE
)
UPDATE organization_mcp_collection_server_attachments SET deleted_at = clock_timestamp()
WHERE
  collection_id = (SELECT id FROM org_collection)
  AND mcp_server_id = @mcp_server_id
  AND deleted IS FALSE;

-- name: IsMcpServerAttachedToOrganizationMcpCollection :one
SELECT EXISTS (
  SELECT 1 FROM organization_mcp_collection_server_attachments a
  JOIN organization_mcp_collections c ON c.id = a.collection_id
  WHERE
    a.collection_id = @collection_id
    AND c.organization_id = @organization_id
    AND c.deleted IS FALSE
    AND a.mcp_server_id = @mcp_server_id
    AND a.deleted IS FALSE
);

-- name: ListOrganizationMcpCollectionMcpServerAttachments :many
-- mcp_server-backed attachments for a collection. Scoped through the
-- collection's organization and the mcp_server's project -> organization so
-- IDs alone are never trusted. Each server resolves to a single published
-- endpoint via a lateral pick: custom-domain endpoints win over platform
-- endpoints, then oldest created_at, limit 1 (AGE-2651; per-plugin endpoint
-- preference is a follow-up). Servers without a usable endpoint are dropped.
SELECT
  s.id AS mcp_server_id,
  s.name AS mcp_server_name,
  s.slug AS mcp_server_slug,
  s.visibility AS mcp_server_visibility,
  ep.slug AS endpoint_slug,
  ep.custom_domain_id AS endpoint_custom_domain_id,
  ep.custom_domain AS endpoint_custom_domain,
  rt.published_at AS published_at
FROM organization_mcp_collection_server_attachments rt
JOIN organization_mcp_collections c ON c.id = rt.collection_id
JOIN mcp_servers s ON s.id = rt.mcp_server_id
JOIN projects p ON p.id = s.project_id
LEFT JOIN LATERAL (
  SELECT e.slug, e.custom_domain_id, cd.domain AS custom_domain, e.created_at
  FROM mcp_endpoints e
  LEFT JOIN custom_domains cd
    ON cd.id = e.custom_domain_id
    AND cd.organization_id = @organization_id
    AND cd.deleted IS FALSE
  WHERE e.mcp_server_id = s.id
    AND e.deleted IS FALSE
    -- Only endpoints with a resolvable host: platform endpoints (no custom
    -- domain), or custom-domain endpoints whose domain is still live in this
    -- org. Resolving the host inside the selection keeps endpoint choice and
    -- URL-host construction in lockstep, so a dangling custom-domain endpoint
    -- is never picked and then emitted as a (wrong) platform URL.
    AND (e.custom_domain_id IS NULL OR cd.id IS NOT NULL)
  ORDER BY (e.custom_domain_id IS NULL) ASC, e.created_at ASC
  LIMIT 1
) ep ON TRUE
WHERE
  rt.collection_id = @collection_id
  AND c.organization_id = @organization_id
  AND p.organization_id = @organization_id
  AND c.deleted IS FALSE
  AND rt.deleted IS FALSE
  AND s.deleted IS FALSE
  AND s.visibility <> 'disabled'
  AND ep.slug IS NOT NULL
ORDER BY rt.published_at DESC;
