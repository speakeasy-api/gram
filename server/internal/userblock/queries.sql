-- name: LockUserForBlacklist :one
-- Locks the user row to serialize concurrent block transitions and expose the
-- prior blacklisted_at timestamp for idempotent cascade reporting.
SELECT id, email, display_name, workos_id, admin, deleted_at, blacklisted_at
FROM users
WHERE id = @id
FOR UPDATE;

-- name: MarkUserBlacklisted :one
-- Sets users.blacklisted_at while preserving any earlier timestamp.
UPDATE users
SET blacklisted_at = COALESCE(blacklisted_at, clock_timestamp()),
    updated_at = clock_timestamp()
WHERE id = @id
RETURNING id, email, display_name, workos_id, admin, blacklisted_at;

-- name: ListOrganizationsForBlockedUser :many
-- Returns every organization where the user holds an active membership,
-- matched by local user_id or linked WorkOS user id.
SELECT DISTINCT om.id
FROM organization_user_relationships AS rel
JOIN organization_metadata AS om
  ON om.id = rel.organization_id
WHERE rel.deleted IS FALSE
  AND (
    rel.user_id = @user_id
    OR (sqlc.narg('workos_user_id')::text IS NOT NULL AND rel.workos_user_id = sqlc.narg('workos_user_id')::text)
  )
ORDER BY om.id;

-- name: DisableSoleNonStaffOrganizationsForUser :many
-- Disables organizations where the blocked user is an active non-staff member
-- and no other active non-staff member remains. Staff are identified by either
-- users.admin = true (and not soft-deleted) or an active membership in the
-- deployment's configured staff organization. If the blocked user is itself
-- soft-deleted or staff, no organizations are disabled. Shared organizations,
-- already disabled organizations, the shared demo workspace, and the staff
-- organization itself are never modified.
WITH target_orgs AS (
  SELECT om.id
  FROM organization_metadata AS om
  WHERE om.disabled_at IS NULL
    AND om.id <> @demo_organization_id
    AND (sqlc.narg('staff_organization_id')::text IS NULL OR om.id <> sqlc.narg('staff_organization_id')::text)
    AND EXISTS (
      SELECT 1
      FROM users AS blocked_u
      WHERE blocked_u.id = @user_id
        AND blocked_u.deleted_at IS NULL
        AND blocked_u.admin IS FALSE
    )
    AND NOT (
      sqlc.narg('staff_organization_id')::text IS NOT NULL
      AND EXISTS (
        SELECT 1
        FROM organization_user_relationships AS blocked_staff_rel
        WHERE blocked_staff_rel.organization_id = sqlc.narg('staff_organization_id')::text
          AND blocked_staff_rel.deleted IS FALSE
          AND (
            blocked_staff_rel.user_id = @user_id
            OR (sqlc.narg('workos_user_id')::text IS NOT NULL AND blocked_staff_rel.workos_user_id = sqlc.narg('workos_user_id')::text)
          )
      )
    )
    AND EXISTS (
      SELECT 1
      FROM organization_user_relationships AS mine
      WHERE mine.organization_id = om.id
        AND mine.deleted IS FALSE
        AND (
          mine.user_id = @user_id
          OR (sqlc.narg('workos_user_id')::text IS NOT NULL AND mine.workos_user_id = sqlc.narg('workos_user_id')::text)
        )
    )
    AND NOT EXISTS (
      SELECT 1
      FROM organization_user_relationships AS other_rel
      WHERE other_rel.organization_id = om.id
        AND other_rel.deleted IS FALSE
        AND NOT (
          COALESCE(other_rel.user_id = @user_id, FALSE)
          OR (sqlc.narg('workos_user_id')::text IS NOT NULL AND COALESCE(other_rel.workos_user_id = sqlc.narg('workos_user_id')::text, FALSE))
        )
        AND NOT EXISTS (
          SELECT 1
          FROM users AS departed
          WHERE departed.deleted_at IS NOT NULL
            AND (
              departed.id = other_rel.user_id
              OR (other_rel.workos_user_id IS NOT NULL AND departed.workos_id = other_rel.workos_user_id)
            )
        )
        AND NOT EXISTS (
          SELECT 1
          FROM users AS staff_u
          WHERE staff_u.deleted_at IS NULL
            AND (
              staff_u.id = other_rel.user_id
              OR (other_rel.workos_user_id IS NOT NULL AND staff_u.workos_id = other_rel.workos_user_id)
            )
            AND (
              staff_u.admin IS TRUE
              OR (
                sqlc.narg('staff_organization_id')::text IS NOT NULL
                AND EXISTS (
                  SELECT 1
                  FROM organization_user_relationships AS staff_rel
                  WHERE staff_rel.organization_id = sqlc.narg('staff_organization_id')::text
                    AND staff_rel.deleted IS FALSE
                    AND (
                      staff_rel.user_id = staff_u.id
                      OR (staff_u.workos_id IS NOT NULL AND staff_rel.workos_user_id = staff_u.workos_id)
                    )
                )
              )
            )
        )
        AND NOT (
          sqlc.narg('staff_organization_id')::text IS NOT NULL
          AND EXISTS (
            SELECT 1
            FROM organization_user_relationships AS staff_rel
            WHERE staff_rel.organization_id = sqlc.narg('staff_organization_id')::text
              AND staff_rel.deleted IS FALSE
              AND (
                (other_rel.user_id IS NOT NULL AND staff_rel.user_id = other_rel.user_id)
                OR (other_rel.workos_user_id IS NOT NULL AND staff_rel.workos_user_id = other_rel.workos_user_id)
              )
          )
        )
    )
  ORDER BY om.id
  FOR UPDATE OF om
)
UPDATE organization_metadata AS om
SET disabled_at = clock_timestamp(),
    updated_at = clock_timestamp()
FROM target_orgs
WHERE om.id = target_orgs.id
RETURNING om.id, om.name, om.slug, om.disabled_at;

-- name: RevokeAPIKeysCreatedByUser :many
-- Soft-revokes every active API key created by the user by setting deleted_at.
-- The generated deleted column is never assigned directly.
UPDATE api_keys
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE created_by_user_id = @user_id
  AND deleted IS FALSE
RETURNING
  id,
  organization_id,
  project_id,
  name,
  scopes,
  subject_urn,
  delegated_grants,
  delegated_grants_version,
  expires_at;

-- name: DeletePrincipalGrantsForAPIKey :exec
-- Removes per-key RBAC policy grants for a revoked ordinary API key, matching
-- keys.Service.RevokeByID.
DELETE FROM principal_grants
WHERE organization_id = @organization_id
  AND principal_urn = @principal_urn;

-- name: RevokeRemainingUserSessionsForUser :many
-- Soft-deletes any active user_sessions for the blocked user (as direct
-- subject or as an agent session authorizer) not already covered by sign-in
-- revocation, matching usersessions.RevokeUserSession.
UPDATE user_sessions AS s
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
FROM user_session_issuers AS iss
LEFT JOIN projects AS p ON p.id = iss.project_id
WHERE iss.id = s.user_session_issuer_id
  AND s.deleted IS FALSE
  AND (
    s.subject_urn = @subject_urn
    OR s.authorizer_user_id = @user_id
  )
RETURNING
  s.id,
  COALESCE(s.organization_id, iss.organization_id, p.organization_id, '')::text AS resolved_organization_id,
  COALESCE(s.project_id, iss.project_id) AS resolved_project_id,
  s.subject_urn,
  s.jti;

-- name: RevokeUserSessionConsentsForUser :many
-- Withdraws all remembered consent approvals and their per-resource rows for
-- the blocked user, matching usersessions.RevokeUserSessionConsentGroup.
WITH revoked AS (
  UPDATE user_session_consents AS c
  SET deleted_at = clock_timestamp(),
      updated_at = clock_timestamp()
  WHERE c.subject_urn = @subject_urn
    AND c.deleted IS FALSE
  RETURNING c.id, c.user_session_client_id, c.project_id, c.organization_id, c.subject_urn
), resources AS (
  UPDATE user_session_consent_resources AS r
  SET deleted_at = clock_timestamp(),
      updated_at = clock_timestamp()
  FROM revoked
  WHERE r.user_session_consent_id = revoked.id
    AND r.deleted IS FALSE
)
SELECT
  revoked.id,
  COALESCE(revoked.organization_id, iss.organization_id, p.organization_id, '')::text AS resolved_organization_id,
  COALESCE(revoked.project_id, iss.project_id) AS resolved_project_id,
  revoked.subject_urn
FROM revoked
JOIN user_session_clients AS cli ON cli.id = revoked.user_session_client_id
JOIN user_session_issuers AS iss ON iss.id = cli.user_session_issuer_id
LEFT JOIN projects AS p ON p.id = iss.project_id;

-- name: RevokeOwnedUserSessionClientsForUser :many
-- Soft-deletes non-CIMD, non-assistant, non-workload OAuth clients used solely
-- by this user (and never by any other subject). In organizations that remain
-- shared, only public DCR clients (no client secret and no private_key_jwt
-- authentication) are revoked so shared confidential registrations stay intact;
-- in organizations disabled by this cascade, any sole-user non-CIMD client is
-- revoked. Matches usersessions.RevokeUserSessionClient.
WITH target_clients AS (
  SELECT
    cli.id,
    COALESCE(cli.organization_id, iss.organization_id, p.organization_id, '')::text AS resolved_organization_id,
    COALESCE(cli.project_id, iss.project_id) AS resolved_project_id
  FROM user_session_clients AS cli
  JOIN user_session_issuers AS iss ON iss.id = cli.user_session_issuer_id
  LEFT JOIN projects AS p ON p.id = iss.project_id
  WHERE cli.deleted IS FALSE
    AND cli.client_id_metadata_uri IS NULL
    AND cli.assistant_runtime_id IS NULL
    AND cli.assistant_trigger_workload_binding_id IS NULL
    AND (
      COALESCE(cli.organization_id, iss.organization_id, p.organization_id, '') = ANY(@disabled_organization_ids::text[])
      OR (
        cli.client_secret_hash IS NULL
        AND COALESCE(cli.token_endpoint_auth_method, 'none') = 'none'
      )
    )
    AND (
      EXISTS (
        SELECT 1
        FROM user_sessions AS own_s
        WHERE own_s.user_session_client_id = cli.id
          AND (own_s.subject_urn = @subject_urn OR own_s.authorizer_user_id = @user_id)
      )
      OR EXISTS (
        SELECT 1
        FROM user_session_consents AS own_c
        WHERE own_c.user_session_client_id = cli.id
          AND own_c.subject_urn = @subject_urn
      )
    )
    AND NOT EXISTS (
      SELECT 1
      FROM user_sessions AS other_s
      WHERE other_s.user_session_client_id = cli.id
        AND other_s.subject_urn <> @subject_urn
        AND COALESCE(other_s.authorizer_user_id, '') <> @user_id
    )
    AND NOT EXISTS (
      SELECT 1
      FROM user_session_consents AS other_c
      WHERE other_c.user_session_client_id = cli.id
        AND other_c.subject_urn <> @subject_urn
    )
  ORDER BY cli.id
  FOR UPDATE OF cli
)
UPDATE user_session_clients AS cli
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
FROM target_clients
WHERE cli.id = target_clients.id
RETURNING
  cli.id,
  target_clients.resolved_organization_id,
  target_clients.resolved_project_id,
  cli.client_id,
  cli.client_name;

-- name: RevokeOwnedPlatformMCPOAuthClientsForUser :many
-- Revokes DCR Platform MCP OAuth clients connected solely to the blocked user.
-- CIMD clients (client_id_metadata_uri IS NOT NULL) are shared globally and
-- are never revoked for one user. Locks clients before connections/sessions to
-- match platformmcp.PostgresOAuthStore.RevokeClient lock ordering.
WITH target_clients AS (
  SELECT c.id
  FROM platform_mcp_oauth_clients AS c
  WHERE c.revoked_at IS NULL
    AND c.client_id_metadata_uri IS NULL
    AND EXISTS (
      SELECT 1
      FROM platform_mcp_connections AS own_conn
      WHERE own_conn.oauth_client_id = c.id
        AND own_conn.subject_urn = @subject_urn
    )
    AND NOT EXISTS (
      SELECT 1
      FROM platform_mcp_connections AS other_conn
      WHERE other_conn.oauth_client_id = c.id
        AND other_conn.subject_urn <> @subject_urn
    )
  ORDER BY c.id
  FOR UPDATE OF c
)
UPDATE platform_mcp_oauth_clients AS c
SET revoked_at = clock_timestamp(),
    updated_at = clock_timestamp()
FROM target_clients
WHERE c.id = target_clients.id
RETURNING c.id, c.client_id, c.client_name;

-- name: RevokePlatformMCPConnectionsForUser :many
-- Revokes every active Platform MCP connection for the blocked user, matching
-- platformmcp.RevokePlatformMCPConnection column updates and CHECK constraints.
WITH target_connections AS (
  SELECT target.id
  FROM platform_mcp_connections AS target
  WHERE target.subject_urn = @subject_urn
    AND target.revoked_at IS NULL
  ORDER BY target.id
  FOR UPDATE OF target
)
UPDATE platform_mcp_connections AS conn
SET revoked_at = clock_timestamp(),
    reauthorization_required_at = clock_timestamp(),
    reauthorization_reason = 'connection_revoked',
    updated_at = clock_timestamp()
FROM target_connections
WHERE conn.id = target_connections.id
RETURNING conn.id, conn.organization_id, conn.oauth_client_id;

-- name: RevokePlatformMCPAuthorizationGrantsForUser :many
-- Revokes any unconsumed Platform MCP authorization codes belonging to the
-- blocked user's connections.
UPDATE platform_mcp_authorization_grants AS g
SET revoked_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE g.revoked_at IS NULL
  AND g.consumed_at IS NULL
  AND g.connection_id IN (
    SELECT conn.id
    FROM platform_mcp_connections AS conn
    WHERE conn.subject_urn = @subject_urn
  )
RETURNING g.id, g.organization_id, g.connection_id;

-- name: RevokePlatformMCPSessionsForUser :many
-- Revokes every live Platform MCP session across all generations for the
-- blocked user's connections, matching platformmcp.RevokePlatformMCPSession.
UPDATE platform_mcp_sessions AS s
SET revoked_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE s.revoked_at IS NULL
  AND s.connection_id IN (
    SELECT conn.id
    FROM platform_mcp_connections AS conn
    WHERE conn.subject_urn = @subject_urn
  )
RETURNING s.id, s.organization_id, s.connection_id, s.jti;

-- name: RevokeChannelsOAuthGrantsForUser :many
-- Revokes active Channels OAuth grants for the blocked user, matching
-- channels/authn's grant revocation columns.
UPDATE channels_oauth_grants
SET revoked_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE principal = @principal
  AND revoked_at IS NULL
RETURNING id, organization_id, project_id, client_id;

-- name: RevokeOwnedChannelsOAuthClientsForUser :many
-- Revokes Channels OAuth clients whose grants belong solely to the blocked
-- user and that have no grants or pending challenges for any other principal.
WITH target_clients AS (
  SELECT c.client_id
  FROM channels_oauth_clients AS c
  WHERE c.revoked_at IS NULL
    AND EXISTS (
      SELECT 1
      FROM channels_oauth_grants AS own_g
      WHERE own_g.client_id = c.client_id
        AND own_g.principal = @principal
    )
    AND NOT EXISTS (
      SELECT 1
      FROM channels_oauth_grants AS other_g
      WHERE other_g.client_id = c.client_id
        AND other_g.principal <> @principal
    )
    AND NOT EXISTS (
      SELECT 1
      FROM channels_oauth_challenges AS other_ch
      WHERE other_ch.client_id = c.client_id
        AND other_ch.principal <> @principal
    )
  ORDER BY c.client_id
  FOR UPDATE OF c
)
UPDATE channels_oauth_clients AS c
SET revoked_at = clock_timestamp(),
    updated_at = clock_timestamp()
FROM target_clients
WHERE c.client_id = target_clients.client_id
RETURNING c.client_id, c.client_name;
