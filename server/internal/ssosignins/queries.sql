-- name: UpsertSSOSignIn :one
-- Never reassigns a row to another user, clears a revocation or moves
-- authenticated_at. A new row for a WorkOS session already seen in another of
-- the user's organizations keeps that session's earliest sign-in time, so
-- recording it in a second organization cannot restart the age window. A
-- sign-in authenticated at or before a revocation cutoff is recorded revoked.
WITH auth AS (
    SELECT LEAST(
        @authenticated_at::timestamptz,
        COALESCE(
            (
                SELECT min(prior.authenticated_at)
                FROM sso_sign_ins AS prior
                WHERE prior.organization_id = ANY(ARRAY(
                        SELECT r.organization_id
                        FROM organization_user_relationships AS r
                        WHERE r.user_id = @user_id
                        UNION
                        SELECT own.organization_id
                        FROM sso_sign_ins AS own
                        WHERE own.user_id = @user_id
                        UNION
                        SELECT @organization_id::text
                    ))
                  AND prior.user_id = @user_id
                  AND prior.workos_session_id = @workos_session_id
            ),
            @authenticated_at::timestamptz
        )
    ) AS at
),
fence AS (
    SELECT EXISTS (
        SELECT 1
        FROM sso_sign_in_revocation_cutoffs AS c, auth
        WHERE c.organization_id = @organization_id
          AND c.user_id = @user_id
          AND c.cutoff_at >= auth.at
    ) AS fenced
)
INSERT INTO sso_sign_ins (
    organization_id,
    user_id,
    workos_session_id,
    authentication_method,
    authenticated_at,
    revoked_at,
    revoke_reason
)
SELECT
    @organization_id,
    @user_id,
    @workos_session_id,
    @authentication_method,
    auth.at,
    CASE WHEN fence.fenced THEN clock_timestamp() END,
    CASE WHEN fence.fenced THEN 'cutoff' END
FROM auth, fence
ON CONFLICT (organization_id, workos_session_id) DO UPDATE
SET updated_at = clock_timestamp()
WHERE sso_sign_ins.user_id = EXCLUDED.user_id
RETURNING id, revoked_at;

-- name: GetSSOSignInBySession :one
-- The sign-in recorded for a WorkOS session, revoked or not.
SELECT id
FROM sso_sign_ins
WHERE organization_id = @organization_id
  AND user_id = @user_id
  AND workos_session_id = @workos_session_id;

-- name: GetLiveSSOSignInBySession :one
SELECT id
FROM sso_sign_ins
WHERE organization_id = @organization_id
  AND user_id = @user_id
  AND workos_session_id = @workos_session_id
  AND revoked_at IS NULL;

-- name: GetSignInEligibility :one
-- Everything CheckSignInIdentity judges, read in one statement. The sign-in
-- joins only when it belongs to the same organization as the session.
SELECT
    s.id AS user_session_id,
    s.subject_urn,
    s.sso_sign_in_id,
    (s.refresh_expires_at IS NOT NULL AND s.refresh_expires_at > clock_timestamp())::boolean AS session_live,
    si.id AS sign_in_id,
    si.user_id AS sign_in_user_id,
    si.authentication_method,
    si.authenticated_at,
    si.revoked_at,
    u.email AS user_email,
    om.verified_domains,
    om.disabled_at AS organization_disabled_at,
    EXISTS (
        SELECT 1
        FROM organization_user_relationships AS r
        WHERE r.organization_id = @organization_id
          AND r.user_id = si.user_id
          AND r.deleted IS FALSE
    )::boolean AS is_member,
    (
        EXISTS (
            SELECT 1
            FROM directory_users AS d
            WHERE d.organization_id = @organization_id
              AND d.user_id = si.user_id
              AND d.deleted IS TRUE
        )
        AND NOT EXISTS (
            SELECT 1
            FROM directory_users AS d
            WHERE d.organization_id = @organization_id
              AND d.user_id = si.user_id
              AND d.deleted IS FALSE
        )
    )::boolean AS is_deprovisioned
FROM user_sessions AS s
JOIN organization_metadata AS om ON om.id = @organization_id
LEFT JOIN sso_sign_ins AS si
    ON si.id = s.sso_sign_in_id
   AND si.organization_id = @organization_id
LEFT JOIN users AS u
    ON u.id = si.user_id
   AND u.deleted_at IS NULL
   AND u.workos_deleted_at IS NULL
WHERE s.id = @user_session_id
  AND s.organization_id = @organization_id
  AND s.deleted IS FALSE;

-- name: LockSSOSignInsBySession :many
-- Locks a signed-out WorkOS session's sign-ins in id order, as every other
-- revocation and grant issuance does.
SELECT id
FROM sso_sign_ins
WHERE organization_id = ANY(@organization_ids::text[])
  AND user_id = @user_id
  AND workos_session_id = @workos_session_id
ORDER BY id
FOR UPDATE;

-- name: RevokeSSOSignInsBySession :many
-- A single sign-out revokes only the signed-out WorkOS session's rows.
UPDATE sso_sign_ins
SET revoked_at = clock_timestamp(),
    revoke_reason = @revoke_reason,
    updated_at = clock_timestamp()
WHERE organization_id = ANY(@organization_ids::text[])
  AND user_id = @user_id
  AND workos_session_id = @workos_session_id
  AND revoked_at IS NULL
RETURNING id, organization_id;

-- name: ListSSOSignInOrganizationsForUser :many
-- Self-scoped: the organizations the caller has ever signed in to, including
-- ones they have since left.
SELECT DISTINCT organization_id
FROM sso_sign_ins
WHERE user_id = @user_id;

-- name: LockUserSSOSignIns :many
-- Locks the user's sign-ins in id order before any revocation write, so
-- concurrent revokes and grant issuance (which also locks sign-ins first)
-- never deadlock.
SELECT id
FROM sso_sign_ins
WHERE organization_id = ANY(@organization_ids::text[])
  AND user_id = @user_id
ORDER BY id
FOR UPDATE;

-- name: RaiseRevocationCutoffs :exec
-- Fences out every sign-in authenticated up to now, including ones a WorkOS
-- exchange already in flight records later. The cutoff only moves forward.
INSERT INTO sso_sign_in_revocation_cutoffs (organization_id, user_id, cutoff_at)
SELECT org_id, @user_id, clock_timestamp()
FROM unnest(@organization_ids::text[]) AS org_id
ON CONFLICT (organization_id, user_id) DO UPDATE
SET cutoff_at = GREATEST(sso_sign_in_revocation_cutoffs.cutoff_at, EXCLUDED.cutoff_at),
    updated_at = clock_timestamp();

-- name: RevokeUserSSOSignIns :many
-- Also upgrades sign-ins a single sign-out revoked, so the stronger reason
-- blocks their outstanding codes and refreshes. The first revocation time is
-- kept.
UPDATE sso_sign_ins
SET revoked_at = COALESCE(revoked_at, clock_timestamp()),
    revoke_reason = @revoke_reason,
    updated_at = clock_timestamp()
WHERE organization_id = ANY(@organization_ids::text[])
  AND user_id = @user_id
  AND (revoked_at IS NULL OR revoke_reason = 'sign_out')
RETURNING id, organization_id, workos_session_id;

-- name: RevokeUserSessionsForUserSignIns :many
-- Soft-deletes every live grant made from any of the user's sign-ins in these
-- organizations, including sign-ins an earlier single sign-out revoked.
-- Grants without a recorded sign-in are untouched.
UPDATE user_sessions AS s
SET deleted_at = clock_timestamp()
WHERE s.organization_id = ANY(@organization_ids::text[])
  AND s.deleted IS FALSE
  AND s.sso_sign_in_id IN (
      SELECT si.id
      FROM sso_sign_ins AS si
      WHERE si.organization_id = ANY(@organization_ids::text[])
        AND si.user_id = @user_id
  )
RETURNING s.id, s.organization_id, s.project_id, s.subject_urn, s.jti;

-- name: ListRecentlyRevokedSessionJTIs :many
-- Grants of the user's sign-ins soft-deleted recently, so a retried
-- revocation re-pushes JTIs a failed earlier push may have missed.
SELECT s.jti
FROM user_sessions AS s
JOIN sso_sign_ins AS si
    ON si.id = s.sso_sign_in_id
   AND si.organization_id = s.organization_id
WHERE s.organization_id = ANY(@organization_ids::text[])
  AND si.user_id = @user_id
  AND s.deleted IS TRUE
  AND s.deleted_at > @since;

-- name: EnqueueRevocationDeliveries :many
-- One pending delivery per target; an already pending one is returned as is.
INSERT INTO sso_sign_in_revocation_deliveries (organization_id, kind, target)
SELECT @organization_id, @kind, target
FROM unnest(@targets::text[]) AS target
ON CONFLICT (organization_id, kind, target) WHERE delivered_at IS NULL
DO UPDATE SET updated_at = clock_timestamp()
RETURNING id;

-- name: ClaimRevocationDeliveries :many
SELECT id, organization_id, kind, target, effective_at
FROM sso_sign_in_revocation_deliveries
WHERE organization_id = ANY(@organization_ids::text[])
  AND id = ANY(@ids::uuid[])
  AND delivered_at IS NULL
ORDER BY id
FOR UPDATE SKIP LOCKED;

-- name: ClaimPendingRevocationDeliveries :many
-- The retry sweep spans every organization by design.
SELECT id, organization_id, kind, target, effective_at
FROM sso_sign_in_revocation_deliveries
WHERE delivered_at IS NULL
  AND attempts < @max_attempts
ORDER BY created_at
LIMIT @batch_size
FOR UPDATE SKIP LOCKED;

-- name: MarkRevocationDelivered :exec
UPDATE sso_sign_in_revocation_deliveries
SET delivered_at = clock_timestamp(),
    attempts = attempts + 1,
    last_error = NULL,
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id;

-- name: MarkRevocationDeliveryFailed :exec
UPDATE sso_sign_in_revocation_deliveries
SET attempts = attempts + 1,
    last_error = @last_error,
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id;

-- name: GetOrganizationIDByWorkOSID :one
SELECT id
FROM organization_metadata
WHERE workos_id = @workos_id;
