-- name: UpsertOrganizationMetadata :one
INSERT INTO organization_metadata (
    id,
    name,
    slug,
    account_type
) VALUES (
    @id,
    @name,
    @slug,
    @account_type
)
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name,
    slug = EXCLUDED.slug,
    account_type = EXCLUDED.account_type,
    updated_at = clock_timestamp()
RETURNING *;

-- name: GetOrganizationMetadata :one
SELECT *
FROM organization_metadata
WHERE id = @id;
