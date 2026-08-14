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

-- name: GetSessionTrial :one
-- Backs the trial status the dashboard renders for a session. Ended and demoted
-- rows are deliberately kept so the dashboard can tell the user their trial
-- ended, while converted rows are dropped so a paying customer never sees
-- trial UI.
SELECT organization_id, created_at, ends_at
FROM trials
WHERE organization_id = @organization_id
  AND converted_at IS NULL;

-- name: InsertTrialFixture :exec
-- Test-only fixture for exercising active trial lifecycle states.
INSERT INTO trials (organization_id, tier, created_at, ends_at, converted_at, demoted_at)
VALUES (
    @organization_id,
    @tier,
    @created_at,
    @ends_at,
    sqlc.narg('converted_at')::timestamptz,
    sqlc.narg('demoted_at')::timestamptz
);

-- name: ListExpiredTrials :many
SELECT organization_id
FROM trials
WHERE ends_at < clock_timestamp()
  AND converted_at IS NULL
  AND demoted_at IS NULL
ORDER BY ends_at;

-- name: MarkTrialConverted :execrows
-- Records that the trial became a signed contract. Zero rows means the trial
-- already converted.
UPDATE trials
SET converted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND converted_at IS NULL;

-- name: MarkTrialDemoted :one
-- No rows means the trial no longer meets the sweep conditions.
UPDATE trials
SET demoted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND ends_at < clock_timestamp()
  AND converted_at IS NULL
  AND demoted_at IS NULL
RETURNING *;

-- name: ExtendTrial :one
-- Operator-initiated extension. The interval is added to the existing ends_at
-- rather than to the current time: "give them another two weeks" means two weeks
-- on top of whatever the trial has left, and adding to now would silently
-- shorten a trial that still had three weeks to run.
--
-- make_interval(days => N) on a timestamptz is calendar-day arithmetic evaluated
-- in the session TimeZone, so it is 23 hours across a spring-forward rather than
-- 24. Calendar days are the right semantics for "another two weeks", and every
-- session here is UTC in any case: the database container defaults to Etc/UTC and
-- GRAM_DATABASE_URL sets no TimeZone. That is what makes the exact 24-hour
-- assertions in the tests sound, and a deployment that ever sets a non-UTC
-- session TimeZone has to revisit them.
--
-- Only a running trial can be extended, and the conditions that define running
-- are here rather than in the handler so the database enforces them. No row
-- means either that the trial cannot be extended or that the organization does
-- not exist at all; the two share an empty result but not an operator action, so
-- the handler tells them apart with a follow-up read rather than merging them.
--
-- The previous ends_at comes back from a CTE that reads the row before the
-- update rather than from subtracting the interval afterwards: the calendar-day
-- arithmetic above has no exact inverse across a daylight saving boundary, and
-- the audit entry has to carry the date the row actually held.
--
-- Every condition sits on the locking read and the UPDATE keeps only the join.
-- That placement is load-bearing under READ COMMITTED. FOR UPDATE re-evaluates
-- its own conditions against the newest row version once it stops waiting on a
-- competing writer; the UPDATE's scan cannot, because a row its snapshot already
-- rejects is skipped before the lock is ever taken. With the conditions on the
-- UPDATE, a second operator extending a trial in its last moments unblocks onto
-- a row that just gained two weeks and is told there is no running trial.
--
-- The three conditions are not equally load-bearing today, and it is worth
-- saying which is which:
--
--   * converted_at IS NULL is load-bearing. A mid-trial conversion leaves ends_at
--     in the future, so nothing else would reject it.
--   * ends_at > clock_timestamp() is load-bearing. It is the ordinary case.
--   * demoted_at IS NULL is defence in depth: no writer leaves demoted_at set
--     with ends_at in the future.
--
-- Extending a demoted trial is not re-arming it: a re-arm also revives the
-- model provider keys and restores the account type. See RearmTrial.
--
-- Two things this query deliberately does not check:
--
--   * tier. The error message and the API description both say enterprise, and
--     enterprise is the only tier the application writes, so the claim holds
--     today. schema.sql anticipates further tiers, and the first one that arrives
--     must revisit this query and that wording together, because this statement
--     would otherwise extend it while reporting an enterprise trial.
--   * organization_metadata.disabled_at. A disabled organization can still be
--     extended, on purpose. Disabled and trial state are independent axes: an
--     operator who disables an organization while investigating and re-enables it
--     afterwards should not have silently lost the ability to extend in between,
--     and a trial that keeps expiring during the investigation punishes the
--     investigation.
WITH previous AS (
    SELECT trials.organization_id, trials.ends_at
    FROM trials
    WHERE trials.organization_id = @organization_id
      AND trials.converted_at IS NULL
      AND trials.demoted_at IS NULL
      AND trials.ends_at > clock_timestamp()
    FOR UPDATE
)
UPDATE trials
SET ends_at = trials.ends_at + make_interval(days => @extend_by_days::int),
    updated_at = clock_timestamp()
FROM previous
WHERE trials.organization_id = previous.organization_id
RETURNING previous.ends_at AS previous_ends_at, trials.ends_at;

-- name: RearmTrial :one
-- Operator-initiated reinstatement of a demoted trial. Returns the tier the
-- trial grants, which the handler writes back onto the organization.
--
-- ends_at moves to a window measured from now, not left where it is:
-- MarkTrialDemoted only demotes an already-past ends_at, so clearing demoted_at
-- alone leaves a row the next sweep demotes again.
--
-- converted_at IS NULL guards nothing today, because MarkTrialConverted has no
-- production caller. It is written for the conversion path that will (AGE-3218).
UPDATE trials
SET demoted_at = NULL,
    ends_at = clock_timestamp() + make_interval(days => @rearm_for_days::int),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND demoted_at IS NOT NULL
  AND converted_at IS NULL
RETURNING tier, ends_at;

-- name: RestoreOrganizationFromTrial :one
-- Undoes DemoteOrganizationToFree's two writes. whitelisted is set
-- unconditionally because demotion cleared it and the signup arming path never
-- writes it, so replaying that path would leave the book-a-demo gate up.
UPDATE organization_metadata
SET gram_account_type = @account_type,
    whitelisted = TRUE,
    updated_at = clock_timestamp()
WHERE id = @organization_id
RETURNING name, slug;

-- name: DemoteOrganizationToFree :one
-- Drops the organization to the free tier and back behind the dashboard
-- book-a-demo gate. Returns the pre-update account type.
-- The UPDATE joins the CTE so that the locking read runs first: a row this
-- statement has already updated is invisible to FOR UPDATE.
WITH previous AS (
    SELECT organization_metadata.id, organization_metadata.gram_account_type
    FROM organization_metadata
    WHERE organization_metadata.id = @organization_id
    FOR UPDATE
)
UPDATE organization_metadata
SET gram_account_type = 'free',
    whitelisted = FALSE,
    updated_at = clock_timestamp()
FROM previous
WHERE organization_metadata.id = previous.id
RETURNING organization_metadata.name, organization_metadata.slug, previous.gram_account_type AS previous_account_type;
