-- name: ListLiveUserSessionCredentials :many
-- One row per live user session credential after the cursor: the lineage of
-- rows refresh rotation continues, named by COALESCE(credential_id, id). A
-- credential is live while a row of it is not deleted and can still refresh.
-- Its organization is its issuer's, which a project issuer takes from its
-- project.
SELECT DISTINCT ON (COALESCE(s.credential_id, s.id))
       COALESCE(s.credential_id, s.id)::uuid AS credential_id,
       s.user_session_issuer_id,
       COALESCE(usi.organization_id, p.organization_id, '')::text AS organization_id
FROM user_sessions AS s
JOIN user_session_issuers AS usi
  ON usi.id = s.user_session_issuer_id
LEFT JOIN projects AS p
  ON p.id = usi.project_id
WHERE s.deleted IS FALSE
  AND s.refresh_expires_at > clock_timestamp()
  AND COALESCE(s.credential_id, s.id) > @cursor::uuid
  AND (@organization_id::text = '' OR COALESCE(usi.organization_id, p.organization_id) = @organization_id::text)
ORDER BY COALESCE(s.credential_id, s.id), s.created_at DESC
LIMIT @page_size::int;

-- name: ListUpstreamClientSelectionCoverage :many
-- Every server one credential can reach, with its recorded choice and the
-- evidence RecordUpstreamClientSelections decides from. The reach and the
-- evidence must stay identical to that statement's, so a prediction made here
-- is what the write records.
WITH session_issuer AS (
    SELECT usi.id,
           usi.project_id,
           COALESCE(usi.organization_id, p.organization_id) AS organization_id
    FROM user_session_issuers AS usi
    LEFT JOIN projects AS p ON p.id = usi.project_id
    WHERE usi.id = @user_session_issuer_id
      AND usi.deleted IS FALSE
),
reachable AS (
    SELECT s.id, s.project_id, p.organization_id, s.user_session_issuer_id,
           s.remote_session_issuer_id AS derived_issuer_id,
           COALESCE(rms.url, tms.resource_identifier, '') AS upstream
    FROM mcp_servers AS s
    JOIN projects AS p
      ON p.id = s.project_id
     AND p.deleted IS FALSE
    JOIN session_issuer AS si
      ON p.organization_id = si.organization_id
    LEFT JOIN remote_mcp_servers AS rms
      ON rms.id = s.remote_mcp_server_id
     AND rms.deleted IS FALSE
    LEFT JOIN tunneled_mcp_servers AS tms
      ON tms.id = s.tunneled_mcp_server_id
     AND tms.deleted IS FALSE
    WHERE s.deleted IS FALSE
      AND (s.remote_mcp_server_id IS NOT NULL OR s.tunneled_mcp_server_id IS NOT NULL)
      AND (
        s.user_session_issuer_id = si.id
        OR si.project_id IS NULL
        OR EXISTS (
          SELECT 1
          FROM meta_mcp_server_members AS m
          JOIN meta_mcp_servers AS g
            ON g.id = m.meta_mcp_server_id
           AND g.deleted IS FALSE
          WHERE m.mcp_server_id = s.id
            AND m.deleted IS FALSE
            AND g.user_session_issuer_id = si.id
        )
      )
)
SELECT r.id AS mcp_server_id,
       EXISTS (
         SELECT 1
         FROM upstream_client_selections AS sel
         WHERE sel.credential_kind = @credential_kind::text
           AND sel.credential_id = @credential_id::uuid
           AND sel.mcp_server_id = r.id
           AND sel.deleted IS FALSE
       )::boolean AS selected,
       (
         SELECT count(*)
         FROM mcp_server_remote_session_clients AS a
         JOIN remote_session_clients AS c
           ON c.id = a.remote_session_client_id
          AND c.deleted IS FALSE
         JOIN remote_session_client_user_session_issuers AS link
           ON link.remote_session_client_id = a.remote_session_client_id
          AND link.user_session_issuer_id = @user_session_issuer_id
         WHERE a.mcp_server_id = r.id
           AND a.deleted IS FALSE
       )::bigint AS eligible_clients,
       (
         SELECT count(*)
         FROM mcp_server_remote_session_clients AS a
         JOIN remote_session_clients AS c
           ON c.id = a.remote_session_client_id
          AND c.deleted IS FALSE
         JOIN remote_session_client_user_session_issuers AS link
           ON link.remote_session_client_id = a.remote_session_client_id
          AND link.user_session_issuer_id = @user_session_issuer_id
         WHERE a.mcp_server_id = r.id
           AND a.deleted IS FALSE
           AND EXISTS (
             SELECT 1
             FROM remote_sessions AS rs
             JOIN user_sessions AS credential
               ON credential.subject_urn = rs.subject_urn
              AND credential.id = @credential_id::uuid
             WHERE rs.remote_session_client_id = a.remote_session_client_id
               AND rs.deleted IS FALSE
           )
       )::bigint AS connected_clients,
       (
         SELECT count(*)
         FROM mcp_server_remote_session_clients AS a
         WHERE a.mcp_server_id = r.id
           AND a.deleted IS FALSE
       )::bigint AS associated_clients,
       (
         SELECT count(*)
         FROM remote_session_client_user_session_issuers AS link
         JOIN remote_session_clients AS c
           ON c.id = link.remote_session_client_id
          AND c.deleted IS FALSE
         WHERE link.user_session_issuer_id = r.user_session_issuer_id
       )::bigint AS issuer_bound_clients,
       (
         SELECT count(*)
         FROM remote_session_client_user_session_issuers AS gateway_link
         JOIN remote_session_clients AS gateway_client
           ON gateway_client.id = gateway_link.remote_session_client_id
          AND gateway_client.deleted IS FALSE
          AND (gateway_client.project_id = r.project_id
               OR (gateway_client.project_id IS NULL AND gateway_client.organization_id = r.organization_id)
               OR (gateway_client.project_id IS NULL AND gateway_client.organization_id IS NULL))
         WHERE gateway_link.user_session_issuer_id = @user_session_issuer_id
           AND (
             EXISTS (
               SELECT 1
               FROM remote_session_issuers AS derived
               WHERE derived.id = r.derived_issuer_id
                 AND derived.id = gateway_client.remote_session_issuer_id
                 AND derived.deleted IS FALSE
                 AND (derived.project_id = r.project_id
                      OR (derived.project_id IS NULL AND derived.organization_id = r.organization_id)
                      OR (derived.project_id IS NULL AND derived.organization_id IS NULL))
             )
             OR EXISTS (
               SELECT 1
               FROM remote_sessions AS rs
               WHERE rs.remote_session_client_id = gateway_client.id
                 AND rs.deleted IS FALSE
                 AND r.upstream <> ''
                 AND rtrim(rs.resource, '/') = rtrim(r.upstream, '/')
                 AND EXISTS (
                   SELECT 1
                   FROM user_session_issuers AS minted
                   LEFT JOIN projects AS minted_project ON minted_project.id = minted.project_id
                   WHERE minted.id = rs.user_session_issuer_id
                     AND COALESCE(minted.organization_id, minted_project.organization_id) = r.organization_id
                 )
             )
           )
       )::bigint AS evidence_clients
FROM reachable AS r
ORDER BY r.id;

-- Test fixtures. Each seeds the one row its name says; nothing else uses them.

-- name: SeedOrganizationFixture :exec
INSERT INTO organization_metadata (id, name, slug) VALUES (@id, @id, @id);

-- name: SeedProjectFixture :exec
INSERT INTO projects (id, name, slug, organization_id) VALUES (@id, @slug, @slug, @organization_id);

-- name: SeedUserSessionIssuerFixture :exec
-- A NULL project_id makes an organization-level issuer.
INSERT INTO user_session_issuers (id, project_id, organization_id, slug, authn_challenge_mode, session_duration)
VALUES (@id, sqlc.narg('project_id'), @organization_id, @slug, 'interactive', interval '1 hour');

-- name: SeedRemoteSessionIssuerFixture :exec
INSERT INTO remote_session_issuers (id, project_id, slug, issuer) VALUES (@id, @project_id, @slug, @issuer);

-- name: SeedRemoteSessionClientFixture :exec
INSERT INTO remote_session_clients (id, project_id, organization_id, remote_session_issuer_id, client_id)
VALUES (@id, @project_id, @organization_id, @remote_session_issuer_id, @client_id);

-- name: SeedClientBindingFixture :exec
INSERT INTO remote_session_client_user_session_issuers (remote_session_client_id, user_session_issuer_id)
VALUES (@remote_session_client_id, @user_session_issuer_id);

-- name: SeedRemoteMCPServerFixture :exec
INSERT INTO remote_mcp_servers (id, project_id, transport_type, url) VALUES (@id, @project_id, 'streamable-http', 'https://upstream.example.com/mcp');

-- name: SeedMCPServerFixture :exec
INSERT INTO mcp_servers (id, project_id, slug, remote_mcp_server_id, user_session_issuer_id, visibility)
VALUES (@id, @project_id, @slug, @remote_mcp_server_id, sqlc.narg('user_session_issuer_id'), 'private');

-- name: SeedMetaMCPServerFixture :exec
INSERT INTO meta_mcp_servers (id, organization_id, project_id, name, user_session_issuer_id)
VALUES (@id, @organization_id, @project_id, @name, @user_session_issuer_id);

-- name: SeedMetaMCPServerMemberFixture :exec
INSERT INTO meta_mcp_server_members (project_id, meta_mcp_server_id, mcp_server_id) VALUES (@project_id, @meta_mcp_server_id, @mcp_server_id);

-- name: SeedAssociationFixture :exec
INSERT INTO mcp_server_remote_session_clients (project_id, organization_id, mcp_server_id, remote_session_client_id, client_attachment_scope)
VALUES (
    @project_id, @organization_id, @mcp_server_id, @remote_session_client_id,
    (SELECT c.attachment_scope FROM remote_session_clients AS c WHERE c.id = @remote_session_client_id)
);

-- name: RemoveAssociationFixture :exec
UPDATE mcp_server_remote_session_clients SET deleted_at = clock_timestamp()
WHERE mcp_server_id = @mcp_server_id AND remote_session_client_id = @remote_session_client_id AND deleted IS FALSE;

-- name: SeedUserSessionFixture :exec
INSERT INTO user_sessions (id, project_id, organization_id, user_session_issuer_id, subject_urn, jti, refresh_expires_at, expires_at, credential_id)
VALUES (@id, sqlc.narg('project_id'), @organization_id, @user_session_issuer_id, @subject_urn, @jti, @refresh_expires_at, clock_timestamp(), sqlc.narg('credential_id'));

-- name: ListSelectionsFixture :many
SELECT mcp_server_id, remote_session_client_id, no_auth, source
FROM upstream_client_selections
WHERE credential_kind = @credential_kind AND credential_id = @credential_id AND deleted IS FALSE
ORDER BY mcp_server_id;

-- name: UnbindClientFixture :exec
DELETE FROM remote_session_client_user_session_issuers
WHERE remote_session_client_id = @remote_session_client_id AND user_session_issuer_id = @user_session_issuer_id;

-- name: SeedUnproxiedMCPServerFixture :exec
INSERT INTO unproxied_mcp_servers (id, project_id, url) VALUES (@id, @project_id, 'https://unproxied.example.com/mcp');

-- name: SeedUnproxiedMCPServerRowFixture :exec
INSERT INTO mcp_servers (id, project_id, slug, unproxied_mcp_server_id, user_session_issuer_id, visibility)
VALUES (@id, @project_id, @slug, @unproxied_mcp_server_id, @user_session_issuer_id, 'private');

-- name: SeedGrantFixture :exec
INSERT INTO remote_sessions (subject_urn, user_session_issuer_id, remote_session_client_id, access_token_encrypted, resource)
VALUES (@subject_urn, @user_session_issuer_id, @remote_session_client_id, 'encrypted', @resource);
