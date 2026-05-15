-- name: GetProjectByID :one
SELECT id, slug
FROM projects
WHERE id = @id
  AND deleted IS FALSE;

-- name: GetProjectBySlug :one
SELECT id, slug
FROM projects
WHERE slug = @slug
  AND deleted IS FALSE;

-- name: AdminListOrganizations :many
SELECT
    om.id,
    om.name,
    om.slug,
    om.gram_account_type AS account_type,
    om.workos_id,
    om.whitelisted,
    om.disabled_at,
    om.free_trial_started_at,
    om.free_trial_ends_at,
    om.created_at,
    om.updated_at,
    (
        SELECT count(*)
        FROM organization_user_relationships our
        WHERE our.organization_id = om.id
          AND our.deleted IS FALSE
    )::bigint AS member_count
FROM organization_metadata om
WHERE
    (sqlc.narg('q')::text IS NULL OR om.name ILIKE '%' || sqlc.narg('q')::text || '%' OR om.slug ILIKE '%' || sqlc.narg('q')::text || '%')
    AND (sqlc.narg('account_type')::text IS NULL OR om.gram_account_type = sqlc.narg('account_type')::text)
    AND (sqlc.arg('include_disabled')::boolean OR om.disabled_at IS NULL)
    AND (sqlc.narg('after_id')::text IS NULL OR om.id > sqlc.narg('after_id')::text)
ORDER BY om.id ASC
LIMIT sqlc.arg('page_limit')::int;
