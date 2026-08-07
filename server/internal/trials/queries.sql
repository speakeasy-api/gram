-- name: CreateTrial :exec
-- One row per organization forever: extend a trial by moving ends_at forward,
-- never by inserting a second row.
INSERT INTO trials (organization_id, tier, ends_at)
VALUES (@organization_id, @tier, @ends_at);

-- name: GetTrial :one
SELECT *
FROM trials
WHERE organization_id = @organization_id;

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

-- name: ListExpiredTrials :many
-- Trials past their end date that neither converted nor were already demoted.
SELECT organization_id
FROM trials
WHERE ends_at < clock_timestamp()
  AND converted_at IS NULL
  AND demoted_at IS NULL
ORDER BY ends_at;

-- name: MarkTrialConverted :execrows
-- Records that the trial became a signed contract. The first conversion wins.
-- Zero rows means the trial already converted.
UPDATE trials
SET converted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND converted_at IS NULL;

-- name: MarkTrialDemoted :one
-- Demotes a trial that is expired, unconverted, and not already demoted. No
-- rows means the trial no longer meets those conditions.
UPDATE trials
SET demoted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND ends_at < clock_timestamp()
  AND converted_at IS NULL
  AND demoted_at IS NULL
RETURNING *;

-- name: DemoteOrganizationToFree :one
-- Drops the organization to the free tier and out of the enterprise alert
-- cohort. Returns the pre-update account type.
WITH previous AS (
    SELECT organization_metadata.gram_account_type
    FROM organization_metadata
    WHERE organization_metadata.id = @organization_id
)
UPDATE organization_metadata
SET gram_account_type = 'free',
    whitelisted = FALSE,
    updated_at = clock_timestamp()
WHERE organization_metadata.id = @organization_id
RETURNING organization_metadata.name, organization_metadata.slug, (SELECT previous.gram_account_type FROM previous) AS previous_account_type;
