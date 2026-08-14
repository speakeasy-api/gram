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
    'enterprise',
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

-- name: ExtendTrial :execrows
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
-- are here rather than in the handler so the database enforces them. Zero rows
-- means either that the trial cannot be extended or that the organization does
-- not exist at all; the two share a row count but not an operator action, so the
-- handler tells them apart with a follow-up read rather than merging them.
--
-- The three conditions are not equally load-bearing today, and it is worth
-- saying which is which:
--
--   * converted_at IS NULL is load-bearing. A mid-trial conversion leaves ends_at
--     in the future, so nothing else would reject it.
--   * ends_at > clock_timestamp() is load-bearing. It is the ordinary case.
--   * demoted_at IS NULL is forward-looking. MarkTrialDemoted only demotes an
--     already-expired trial and nothing moves ends_at forward afterwards, so in
--     every state the application can currently reach it is subsumed by the
--     ends_at condition. It is kept as defence against a future re-arm path.
--
-- Extending a demoted trial is deliberately not the same as re-arming it:
-- demotion also disabled the organization's model provider keys and nothing in
-- this repository can re-enable them, so clearing demoted_at would advertise a
-- running trial whose keys stay dead (AGE-3208).
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
UPDATE trials
SET ends_at = ends_at + make_interval(days => @extend_by_days::int),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND converted_at IS NULL
  AND demoted_at IS NULL
  AND ends_at > clock_timestamp();

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
