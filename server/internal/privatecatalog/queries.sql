-- name: CreatePrivateCatalogServer :one
INSERT INTO private_catalog_servers (organization_id, project_id, published_by, name, slug, description)
VALUES (@organization_id, @project_id, @published_by, @name, @slug, @description)
RETURNING *;

-- name: GetPrivateCatalogServerByID :one
SELECT *
FROM private_catalog_servers
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE;

-- name: GetPrivateCatalogServerBySlug :one
SELECT *
FROM private_catalog_servers
WHERE organization_id = @organization_id AND slug = @slug AND deleted IS FALSE;

-- name: GetPrivateCatalogServerByProjectID :one
SELECT *
FROM private_catalog_servers
WHERE project_id = @project_id AND deleted IS FALSE;

-- name: ListPrivateCatalogServersByOrganization :many
SELECT *
FROM private_catalog_servers
WHERE organization_id = @organization_id AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: UpdatePrivateCatalogServer :one
UPDATE private_catalog_servers
SET
  name = COALESCE(sqlc.narg('name'), name),
  slug = COALESCE(sqlc.narg('slug'), slug),
  description = COALESCE(sqlc.narg('description'), description),
  updated_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: DeletePrivateCatalogServer :one
UPDATE private_catalog_servers
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;
