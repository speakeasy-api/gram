-- name: UpsertSSOSignIn :one
-- A repeated code exchange for the same WorkOS session refreshes the row but
-- never reassigns it to another user or clears a revocation.
INSERT INTO sso_sign_ins (
    organization_id,
    user_id,
    workos_session_id,
    authentication_method,
    authenticated_at
) VALUES (
    @organization_id,
    @user_id,
    @workos_session_id,
    @authentication_method,
    @authenticated_at
)
ON CONFLICT (organization_id, workos_session_id) DO UPDATE
SET updated_at = clock_timestamp()
WHERE sso_sign_ins.user_id = EXCLUDED.user_id
RETURNING id, revoked_at;

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
WHERE s.id = @user_session_id
  AND s.organization_id = @organization_id
  AND s.deleted IS FALSE;

-- name: RevokeSSOSignInsBySession :execrows
-- A single sign-out revokes only the signed-out WorkOS session's rows.
UPDATE sso_sign_ins
SET revoked_at = clock_timestamp(),
    revoke_reason = @revoke_reason,
    updated_at = clock_timestamp()
WHERE organization_id = ANY(@organization_ids::text[])
  AND user_id = @user_id
  AND workos_session_id = @workos_session_id
  AND revoked_at IS NULL;

-- name: RevokeUserSSOSignIns :many
UPDATE sso_sign_ins
SET revoked_at = clock_timestamp(),
    revoke_reason = @revoke_reason,
    updated_at = clock_timestamp()
WHERE organization_id = ANY(@organization_ids::text[])
  AND user_id = @user_id
  AND revoked_at IS NULL
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

-- name: GetOrganizationIDByWorkOSID :one
SELECT id
FROM organization_metadata
WHERE workos_id = @workos_id;
