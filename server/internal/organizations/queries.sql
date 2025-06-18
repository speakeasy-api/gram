-- name: UpsertOrganizationMetadata :one
INSERT INTO organization_metadata (
    id,
    name,
    slug
) VALUES (
    @id,
    @name,
    @slug
)
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name,
    slug = EXCLUDED.slug,
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
