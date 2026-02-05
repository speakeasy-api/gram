-- name: UpsertOrganizationMetadata :one
INSERT INTO organization_metadata (
    id,
    name,
    slug,
    sso_connection_id
) VALUES (
    @id,
    @name,
    @slug,
    @sso_connection_id
)
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name,
    slug = EXCLUDED.slug,
    sso_connection_id = EXCLUDED.sso_connection_id,
    updated_at = clock_timestamp()
RETURNING *;

-- name: SetAccountType :exec
UPDATE organization_metadata
SET gram_account_type = @gram_account_type,
    updated_at = clock_timestamp()
WHERE id = @id;

-- name: GetOrganizationMetadata :one
SELECT *
FROM organization_metadata
WHERE id = @id;

-- name: UpsertOrganizationUserRelationship :one
INSERT INTO organization_user_relationships (
    organization_id,
    user_id
) VALUES (
    @organization_id,
    @user_id
)
ON CONFLICT (organization_id, user_id) DO UPDATE SET
    updated_at = clock_timestamp()
RETURNING *;

-- name: HasOrganizationUserRelationship :one
SELECT EXISTS(
  SELECT 1
  FROM organization_user_relationships
  WHERE organization_id = @organization_id
    AND user_id = @user_id
    AND deleted_at IS NULL
) AS exists;

-- name: ListOrganizationUsers :many
SELECT *
FROM organization_user_relationships
WHERE organization_id = @organization_id
  AND deleted_at IS NULL;    

