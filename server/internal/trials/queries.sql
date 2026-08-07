-- name: GetActiveTrial :one
SELECT organization_id, created_at, ends_at
FROM trials
WHERE organization_id = @organization_id
  AND converted_at IS NULL
  AND demoted_at IS NULL
  AND ends_at > now();

-- name: InsertTrialFixture :exec
-- Test-only fixture for exercising active trial lifecycle states.
INSERT INTO trials (organization_id, tier, created_at, ends_at, converted_at, demoted_at)
VALUES (
    @organization_id,
    'enterprise',
    @created_at,
    @ends_at,
    sqlc.narg('converted_at')::timestamptz,
    sqlc.narg('demoted_at')::timestamptz
);
