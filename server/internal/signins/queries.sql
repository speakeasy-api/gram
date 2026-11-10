-- name: LockSignInsForUser :exec
-- Serializes recording a sign-in with revoking the user's sign-ins in the
-- organization, including when no sign-in row exists yet. Every writer takes
-- these locks first, in organization order.
SELECT pg_advisory_xact_lock(hashtextextended('sign_in:' || @organization_id::text || ':' || @user_id::text, 0));

-- name: LockAllSignInsForUser :exec
-- Serializes recording a sign-in in any organization with revoking the
-- user's sign-ins, so a first-ever recording in a new organization can't slip
-- past a revocation. Taken before the per-organization locks.
SELECT pg_advisory_xact_lock(hashtextextended('sign_in_user:' || @user_id::text, 0));

-- name: UpsertSignIn :one
-- Never reassigns a row to another user, clears a revocation or moves the
-- recorded times later. A WorkOS session already seen in another of the
-- user's organizations keeps its earliest times, so recording it elsewhere
-- cannot restart the age window, and inherits a sign-out of that session
-- (signed_out_at survives organization-scoped revocations) or a sign out
-- everywhere; admin and deprovisioning revocations stay where they were made.
-- A sign-in whose real authentication or exchange started at or before a
-- revocation cutoff is recorded revoked, including when a later exchange
-- reveals an earlier authentication time. Evidence missing from an earlier
-- exchange of the same method is filled in. An exchange that changes nothing
-- writes nothing.
WITH prior AS (
    SELECT p.authenticated_at, p.idp_authenticated_at, p.revoked_at, p.revoke_reason, p.session_signed_out_at
    FROM sign_ins AS p
    WHERE p.organization_id = ANY(ARRAY(
            SELECT r.organization_id
            FROM organization_user_relationships AS r
            WHERE r.user_id = @user_id::text
            UNION
            SELECT own.organization_id
            FROM sign_ins AS own
            WHERE own.user_id = @user_id
            UNION
            SELECT @organization_id::text
        ))
      AND p.user_id = @user_id
      AND p.workos_session_id = @workos_session_id
),
auth AS (
    SELECT
        LEAST(@authenticated_at::timestamptz, (SELECT min(prior.authenticated_at) FROM prior)) AS at,
        LEAST(sqlc.narg('idp_authenticated_at')::timestamptz, (SELECT min(prior.idp_authenticated_at) FROM prior)) AS idp_at,
        (SELECT min(prior.session_signed_out_at) FROM prior) AS signed_out_at
),
inherited AS (
    SELECT CASE
        WHEN EXISTS (SELECT 1 FROM prior WHERE prior.revoked_at IS NOT NULL AND prior.revoke_reason = 'sign_out_everywhere') THEN 'sign_out_everywhere'
        WHEN (SELECT auth.signed_out_at FROM auth) IS NOT NULL THEN 'sign_out'
    END AS reason
),
fence AS (
    SELECT EXISTS (
        SELECT 1
        FROM sign_in_revocation_cutoffs AS c, auth
        WHERE c.organization_id = @organization_id
          AND c.user_id = @user_id
          AND c.cutoff_at >= LEAST(auth.at, COALESCE(auth.idp_at, auth.at))
    ) AS fenced
),
upsert AS (
    INSERT INTO sign_ins (
        organization_id,
        user_id,
        workos_session_id,
        authentication_method,
        idp_hosted_domain,
        idp_tenant_id,
        verified_email,
        idp_authenticated_at,
        authenticated_at,
        revoked_at,
        revoke_reason,
        session_signed_out_at
    )
    SELECT
        @organization_id,
        @user_id,
        @workos_session_id,
        @authentication_method,
        sqlc.narg('idp_hosted_domain')::text,
        sqlc.narg('idp_tenant_id')::text,
        sqlc.narg('verified_email')::text,
        auth.idp_at,
        auth.at,
        CASE WHEN fence.fenced OR inherited.reason IS NOT NULL THEN clock_timestamp() END,
        CASE WHEN fence.fenced THEN 'cutoff' ELSE inherited.reason END,
        auth.signed_out_at
    FROM auth, fence, inherited
    ON CONFLICT (organization_id, workos_session_id) DO UPDATE
    SET idp_hosted_domain = COALESCE(
            sign_ins.idp_hosted_domain,
            CASE WHEN sign_ins.authentication_method = EXCLUDED.authentication_method THEN EXCLUDED.idp_hosted_domain END
        ),
        idp_tenant_id = COALESCE(
            sign_ins.idp_tenant_id,
            CASE WHEN sign_ins.authentication_method = EXCLUDED.authentication_method THEN EXCLUDED.idp_tenant_id END
        ),
        verified_email = COALESCE(sign_ins.verified_email, EXCLUDED.verified_email),
        idp_authenticated_at = LEAST(sign_ins.idp_authenticated_at, EXCLUDED.idp_authenticated_at),
        revoked_at = COALESCE(sign_ins.revoked_at, EXCLUDED.revoked_at),
        revoke_reason = CASE WHEN sign_ins.revoked_at IS NULL THEN EXCLUDED.revoke_reason ELSE sign_ins.revoke_reason END,
        session_signed_out_at = COALESCE(sign_ins.session_signed_out_at, EXCLUDED.session_signed_out_at),
        updated_at = clock_timestamp()
    WHERE sign_ins.user_id = EXCLUDED.user_id
      AND (
          (sign_ins.idp_hosted_domain IS NULL AND sign_ins.authentication_method = EXCLUDED.authentication_method AND EXCLUDED.idp_hosted_domain IS NOT NULL)
          OR (sign_ins.idp_tenant_id IS NULL AND sign_ins.authentication_method = EXCLUDED.authentication_method AND EXCLUDED.idp_tenant_id IS NOT NULL)
          OR (sign_ins.verified_email IS NULL AND EXCLUDED.verified_email IS NOT NULL)
          OR (EXCLUDED.idp_authenticated_at IS NOT NULL AND (sign_ins.idp_authenticated_at IS NULL OR EXCLUDED.idp_authenticated_at < sign_ins.idp_authenticated_at))
          OR (sign_ins.revoked_at IS NULL AND EXCLUDED.revoked_at IS NOT NULL)
          OR (sign_ins.session_signed_out_at IS NULL AND EXCLUDED.session_signed_out_at IS NOT NULL)
      )
    RETURNING sign_ins.id, sign_ins.revoked_at
)
SELECT upsert.id, upsert.revoked_at
FROM upsert
UNION ALL
SELECT own.id, own.revoked_at
FROM sign_ins AS own
WHERE own.organization_id = @organization_id
  AND own.workos_session_id = @workos_session_id
  AND own.user_id = @user_id
  AND NOT EXISTS (SELECT 1 FROM upsert);

-- name: GetSignInBySession :one
-- The sign-in recorded for a WorkOS session, revoked or not.
SELECT id
FROM sign_ins
WHERE organization_id = @organization_id
  AND user_id = @user_id
  AND workos_session_id = @workos_session_id;

-- name: GetLiveSignInBySession :one
SELECT id
FROM sign_ins
WHERE organization_id = @organization_id
  AND user_id = @user_id
  AND workos_session_id = @workos_session_id
  AND revoked_at IS NULL;

-- name: GetSignInEligibility :one
-- Both runtime grants and pre-grant consent use one set of identity checks.
-- A consent record proves sign-in eligibility only; it never represents a grant.
WITH evidence AS (
    SELECT s.id, s.subject_urn, s.sign_in_id,
           (s.refresh_expires_at IS NOT NULL AND s.refresh_expires_at > clock_timestamp())::boolean AS session_live
    FROM user_sessions AS s
    WHERE s.id = @user_session_id
      AND s.organization_id = @organization_id
      AND s.deleted IS FALSE
    UNION ALL
    SELECT '00000000-0000-0000-0000-000000000000'::uuid,
           @consent_subject_urn::text,
           NULLIF(@consent_sign_in_id::uuid, '00000000-0000-0000-0000-000000000000'::uuid),
           TRUE
    WHERE @user_session_id::uuid = '00000000-0000-0000-0000-000000000000'::uuid
      AND @consent_sign_in_id::uuid != '00000000-0000-0000-0000-000000000000'::uuid
)
SELECT
    s.id AS user_session_id,
    s.subject_urn,
    s.sign_in_id AS linked_sign_in_id,
    s.session_live,
    si.id AS sign_in_id,
    si.user_id AS sign_in_user_id,
    si.authentication_method,
    si.idp_hosted_domain,
    si.idp_tenant_id,
    si.verified_email,
    si.idp_authenticated_at,
    si.revoked_at,
    EXISTS (
        SELECT 1
        FROM sign_in_revocation_cutoffs AS c
        WHERE c.organization_id = @organization_id
          AND c.user_id = si.user_id
          AND c.cutoff_at >= LEAST(si.authenticated_at, COALESCE(si.idp_authenticated_at, si.authenticated_at))
    )::boolean AS fenced,
    u.email AS user_email,
    om.verified_domains,
    (isi.allowed_sign_in_methods IS NULL)::boolean AS all_sign_in_methods_allowed,
    COALESCE(isi.allowed_sign_in_methods, '{}'::text[])::text[] AS allowed_sign_in_methods,
    COALESCE(isi.allowed_microsoft_tenant_ids, '{}'::text[])::text[] AS allowed_microsoft_tenant_ids,
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
FROM evidence AS s
JOIN organization_metadata AS om ON om.id = @organization_id
LEFT JOIN sign_ins AS si
    ON si.id = s.sign_in_id
   AND si.organization_id = @organization_id
LEFT JOIN users AS u
    ON u.id = si.user_id
   AND u.deleted_at IS NULL
   AND u.workos_deleted_at IS NULL
LEFT JOIN internal_session_issuers AS isi
    ON isi.organization_id = @organization_id
   AND isi.deleted IS FALSE
;

-- name: LockSignInsBySession :many
-- Locks a signed-out WorkOS session's sign-ins in id order, as every other
-- revocation and grant issuance does.
SELECT id
FROM sign_ins
WHERE organization_id = ANY(@organization_ids::text[])
  AND user_id = @user_id
  AND workos_session_id = @workos_session_id
ORDER BY id
FOR UPDATE;

-- name: RevokeSignInsBySession :many
-- A single sign-out revokes only the signed-out WorkOS session's rows and
-- marks the session signed out on every one of them, already revoked or not,
-- so the mark survives an organization-scoped revocation. was_live tells
-- which rows this sign-out actually revoked.
WITH target AS (
    SELECT si.id, si.revoked_at IS NULL AS was_live
    FROM sign_ins AS si
    WHERE si.organization_id = ANY(@organization_ids::text[])
      AND si.user_id = @user_id
      AND si.workos_session_id = @workos_session_id
)
UPDATE sign_ins
SET revoked_at = COALESCE(sign_ins.revoked_at, clock_timestamp()),
    revoke_reason = CASE WHEN sign_ins.revoked_at IS NULL THEN @revoke_reason ELSE sign_ins.revoke_reason END,
    session_signed_out_at = COALESCE(sign_ins.session_signed_out_at, clock_timestamp()),
    updated_at = clock_timestamp()
FROM target
WHERE sign_ins.id = target.id
  AND sign_ins.organization_id = ANY(@organization_ids::text[])
  AND (sign_ins.revoked_at IS NULL OR sign_ins.session_signed_out_at IS NULL)
RETURNING sign_ins.id, sign_ins.organization_id, target.was_live::boolean AS was_live;

-- name: ListSignInOrganizationsForUser :many
-- Self-scoped: the organizations the caller has ever signed in to, including
-- ones they have since left.
SELECT DISTINCT organization_id
FROM sign_ins
WHERE user_id = @user_id;

-- name: LockUserSignIns :many
-- Locks the user's sign-ins in id order before any revocation write, so
-- concurrent revokes and grant issuance (which also locks sign-ins first)
-- never deadlock.
SELECT id
FROM sign_ins
WHERE organization_id = ANY(@organization_ids::text[])
  AND user_id = @user_id
ORDER BY id
FOR UPDATE;

-- name: RaiseRevocationCutoffs :many
-- Fences out every sign-in authenticated up to now, including ones a WorkOS
-- exchange already in flight records later. The cutoff only moves forward.
-- Returns the cutoffs so callers can align other revocation boundaries.
INSERT INTO sign_in_revocation_cutoffs (organization_id, user_id, cutoff_at)
SELECT org_id, @user_id, clock_timestamp()
FROM unnest(@organization_ids::text[]) AS org_id
ON CONFLICT (organization_id, user_id) DO UPDATE
SET cutoff_at = GREATEST(sign_in_revocation_cutoffs.cutoff_at, EXCLUDED.cutoff_at),
    updated_at = clock_timestamp()
RETURNING cutoff_at;

-- name: RevokeUserSignIns :many
-- Also upgrades sign-ins a single sign-out revoked, so the stronger reason
-- blocks their outstanding codes and refreshes. The first revocation time is
-- kept.
UPDATE sign_ins
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
  AND s.sign_in_id IN (
      SELECT si.id
      FROM sign_ins AS si
      WHERE si.organization_id = ANY(@organization_ids::text[])
        AND si.user_id = @user_id
  )
RETURNING s.id, s.organization_id, s.project_id, s.subject_urn, s.jti;

-- name: RevokeUnlinkedUserSessions :many
-- Soft-deletes the user's live grants in these organizations that no
-- recorded sign-in backs (minted before linking, or while it was off), so a
-- broad revocation reaches them too.
UPDATE user_sessions AS s
SET deleted_at = clock_timestamp()
WHERE s.organization_id = ANY(@organization_ids::text[])
  AND s.deleted IS FALSE
  AND s.sign_in_id IS NULL
  AND s.subject_urn = @subject_urn
RETURNING s.id, s.organization_id, s.project_id, s.subject_urn, s.jti;

-- name: EnqueueRevocationDeliveries :many
-- One pending delivery per target; an already pending one is returned as is.
INSERT INTO sign_in_revocation_deliveries (organization_id, kind, target)
SELECT @organization_id, @kind, target
FROM unnest(@targets::text[]) AS target
ON CONFLICT (organization_id, kind, target) WHERE delivered_at IS NULL
DO UPDATE SET updated_at = clock_timestamp()
RETURNING id;

-- name: ClaimRevocationDeliveries :many
-- Leases the given pending deliveries to one worker. The lease commits on
-- its own, so pushes happen outside any transaction.
UPDATE sign_in_revocation_deliveries
SET claimed_until = clock_timestamp() + make_interval(secs => @lease_seconds::double precision),
    updated_at = clock_timestamp()
WHERE id IN (
    SELECT d.id
    FROM sign_in_revocation_deliveries AS d
    WHERE d.organization_id = ANY(@organization_ids::text[])
      AND d.id = ANY(@ids::uuid[])
      AND d.delivered_at IS NULL
      AND (d.claimed_until IS NULL OR d.claimed_until < clock_timestamp())
    ORDER BY d.id
    FOR UPDATE SKIP LOCKED
)
RETURNING id, organization_id, kind, target, effective_at, attempts;

-- name: ClaimPendingRevocationDeliveries :many
-- The retry sweep spans every organization by design. Leases due deliveries,
-- oldest first; nothing is ever given up on.
UPDATE sign_in_revocation_deliveries
SET claimed_until = clock_timestamp() + make_interval(secs => @lease_seconds::double precision),
    updated_at = clock_timestamp()
WHERE id IN (
    SELECT d.id
    FROM sign_in_revocation_deliveries AS d
    WHERE d.delivered_at IS NULL
      AND (d.claimed_until IS NULL OR d.claimed_until < clock_timestamp())
      AND (d.next_attempt_at IS NULL OR d.next_attempt_at <= clock_timestamp())
    -- Cache-backed kinds first: they keep revoked tokens and dashboard
    -- sessions out, and a WorkOS outage backlog must not starve them.
    ORDER BY (d.kind = 'workos_session'), d.next_attempt_at NULLS FIRST, d.created_at
    LIMIT @batch_size
    FOR UPDATE SKIP LOCKED
)
RETURNING id, organization_id, kind, target, effective_at, attempts;

-- name: EnqueueUserEpochDelivery :one
-- A pending epoch moves forward rather than duplicating.
INSERT INTO sign_in_revocation_deliveries (organization_id, kind, target, effective_at)
VALUES (@organization_id, 'user_epoch', @user_id, @effective_at)
ON CONFLICT (organization_id, kind, target) WHERE delivered_at IS NULL
DO UPDATE SET effective_at = GREATEST(sign_in_revocation_deliveries.effective_at, EXCLUDED.effective_at),
    next_attempt_at = NULL,
    claimed_until = NULL,
    updated_at = clock_timestamp()
RETURNING id;

-- name: MarkRevocationDelivered :execrows
-- Leaves the delivery pending if its effective time moved during the push.
UPDATE sign_in_revocation_deliveries
SET delivered_at = clock_timestamp(),
    attempts = attempts + 1,
    last_error = NULL,
    claimed_until = NULL,
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND effective_at = @effective_at;

-- name: MarkRevocationDeliveryFailed :execrows
-- Backs off exponentially from one minute, capped at six hours. A delivery
-- whose effective time moved during the push is newer work and is left alone.
UPDATE sign_in_revocation_deliveries
SET attempts = attempts + 1,
    last_error = @last_error,
    claimed_until = NULL,
    next_attempt_at = clock_timestamp() + LEAST(interval '6 hours', interval '1 minute' * power(2, LEAST(attempts, 20))),
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND effective_at = @effective_at;

-- name: ReleaseRevocationDelivery :exec
-- Drops a superseded attempt's lease so the newer work is claimable at once.
UPDATE sign_in_revocation_deliveries
SET claimed_until = NULL,
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND delivered_at IS NULL;

-- name: WorkOSSessionLiveOutsideOrganization :one
-- Fleet-wide by design: whether a WorkOS session (unique to one user) still
-- backs a live sign-in outside the organization that queued ending it.
SELECT EXISTS (
    SELECT 1
    FROM sign_ins AS si
    WHERE si.workos_session_id = @workos_session_id
      AND si.organization_id <> @organization_id
      AND si.revoked_at IS NULL
)::boolean AS live;

-- name: RevocationDeliveryBacklog :one
-- Fleet-wide pending-delivery gauges for the sweep.
SELECT
    count(*)::bigint AS pending,
    count(*) FILTER (WHERE attempts >= @stuck_attempts::integer)::bigint AS stuck,
    COALESCE(EXTRACT(EPOCH FROM clock_timestamp() - min(created_at)), 0)::bigint AS oldest_seconds
FROM sign_in_revocation_deliveries
WHERE delivered_at IS NULL;

-- name: MarkWorkOSSessionSignedOut :exec
-- Leaves a revoked marker for a signed-out WorkOS session with no sign-in yet
-- in the organization, so an exchange still in flight records revoked here or,
-- by inheritance, in any other organization.
INSERT INTO sign_ins (organization_id, user_id, workos_session_id, authentication_method, authenticated_at, revoked_at, revoke_reason, session_signed_out_at)
VALUES (@organization_id, @user_id, @workos_session_id, '', clock_timestamp(), clock_timestamp(), 'sign_out', clock_timestamp())
ON CONFLICT (organization_id, workos_session_id) DO UPDATE
SET session_signed_out_at = COALESCE(sign_ins.session_signed_out_at, clock_timestamp()),
    updated_at = clock_timestamp()
WHERE sign_ins.user_id = EXCLUDED.user_id;

-- name: OtherOrganizationsUsingWorkOSSession :many
-- Organizations other than these where the WorkOS session still backs an
-- unrevoked sign-in; ending the WorkOS session would sign the user out there.
SELECT DISTINCT si.organization_id
FROM sign_ins AS si
WHERE si.organization_id = ANY(ARRAY(
        SELECT r.organization_id
        FROM organization_user_relationships AS r
        WHERE r.user_id = @user_id::text
        UNION
        SELECT own.organization_id
        FROM sign_ins AS own
        WHERE own.user_id = @user_id
    ))
  AND si.user_id = @user_id
  AND si.workos_session_id = @workos_session_id
  AND si.revoked_at IS NULL
  AND NOT (si.organization_id = ANY(@organization_ids::text[]));

-- name: GetOrganizationIDByWorkOSID :one
SELECT id
FROM organization_metadata
WHERE workos_id = @workos_id;
