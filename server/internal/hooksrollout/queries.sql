-- name: GetDefaultHooksRolloutPin :one
SELECT *
FROM hooks_rollout_pins
WHERE organization_id IS NULL
ORDER BY seq DESC
LIMIT 1;

-- name: GetOrganizationHooksRolloutPin :one
-- The newest row for the organization. A NULL version means the override was
-- cleared.
SELECT *
FROM hooks_rollout_pins
WHERE organization_id = @organization_id
ORDER BY seq DESC
LIMIT 1;

-- name: InsertHooksRolloutPin :one
INSERT INTO hooks_rollout_pins (organization_id, version, set_by)
VALUES (sqlc.narg(organization_id), sqlc.narg(version), @set_by)
RETURNING *;

-- name: ListOrganizationHooksRolloutOverrides :many
-- Every organization whose newest row still carries a version, i.e. every
-- override that has not been cleared.
SELECT
  latest.organization_id::text AS organization_id,
  latest.version::integer AS version,
  latest.set_by,
  latest.created_at,
  o.name AS organization_name,
  o.slug AS organization_slug
FROM (
  SELECT DISTINCT ON (p.organization_id) p.organization_id, p.version, p.set_by, p.created_at
  FROM hooks_rollout_pins p
  WHERE p.organization_id IS NOT NULL
  ORDER BY p.organization_id, p.seq DESC
) latest
JOIN organization_metadata o ON o.id = latest.organization_id
WHERE latest.version IS NOT NULL
ORDER BY o.slug;

-- name: ListRecentHooksRolloutChanges :many
SELECT
  p.organization_id,
  p.version,
  p.set_by,
  p.created_at,
  o.slug AS organization_slug
FROM hooks_rollout_pins p
LEFT JOIN organization_metadata o ON o.id = p.organization_id
ORDER BY p.seq DESC
LIMIT @max_rows;
