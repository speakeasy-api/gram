-- name: CreateEnterpriseTrial :exec
-- Arms a trial on an organization the signup transaction just created. One row
-- per organization forever: a trial is extended by moving ends_at forward, not
-- by inserting a second row.
INSERT INTO enterprise_trials (organization_id, ends_at)
VALUES (@organization_id, @ends_at);

-- name: ListExpiredEnterpriseTrials :many
-- Trials past their end date that neither converted nor were already demoted.
-- The table gains one row per trial signup ever and demoted_at bounds each row
-- to a single demotion, so the result set stays small enough to sweep in one
-- pass without a cursor.
SELECT organization_id
FROM enterprise_trials
WHERE ends_at < clock_timestamp()
  AND converted_at IS NULL
  AND demoted_at IS NULL
ORDER BY ends_at;

-- name: MarkEnterpriseTrialConverted :execrows
-- Records that the trial became a signed contract. The first conversion wins,
-- and a converted trial is out of the sweeper's reach for good. Zero rows means
-- the trial already converted.
UPDATE enterprise_trials
SET converted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND converted_at IS NULL;

-- name: GetEnterpriseTrial :one
SELECT *
FROM enterprise_trials
WHERE organization_id = @organization_id;

-- name: MarkEnterpriseTrialDemoted :one
-- Repeats the sweep predicate so a conversion or a manual reinstatement that
-- lands between the list and this write wins, and so a retried sweep cannot
-- demote the same trial twice. No rows means another writer got there first.
UPDATE enterprise_trials
SET demoted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND ends_at < clock_timestamp()
  AND converted_at IS NULL
  AND demoted_at IS NULL
RETURNING *;

-- name: DemoteOrganizationToFree :one
-- Locks the organization out and drops it out of the enterprise alert cohort.
-- A trial only ever belongs to an organization the signup transaction created,
-- so 'free' is the account type the organization would have had without it.
-- Returns the pre-update account type for the audit entry.
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
