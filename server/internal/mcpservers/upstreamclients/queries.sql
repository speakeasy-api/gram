-- name: AssociateMCPServerRemoteSessionClient :execrows
-- Records that one MCP server uses one upstream OAuth client, returning one
-- row whether the association is new or already live, and none when the
-- server or client no longer qualifies. Both ids are re-checked against the
-- caller's tenant here because the client id may come from an untenanted join
-- table: the server must be live in the project, and the client live and owned
-- by that project, its organization, or the platform.
INSERT INTO mcp_server_remote_session_clients (project_id, organization_id, mcp_server_id, remote_session_client_id, client_attachment_scope)
SELECT s.project_id, p.organization_id, s.id, c.id, c.attachment_scope
FROM mcp_servers AS s
JOIN projects AS p
  ON p.id = s.project_id
 AND p.organization_id = @organization_id::text
 AND p.deleted IS FALSE
JOIN remote_session_clients AS c
  ON c.id = @remote_session_client_id::uuid
 AND c.deleted IS FALSE
 AND (c.project_id = p.id
      OR (c.project_id IS NULL AND c.organization_id = p.organization_id)
      OR (c.project_id IS NULL AND c.organization_id IS NULL))
WHERE s.id = @mcp_server_id::uuid
  AND s.project_id = @project_id::uuid
  AND s.deleted IS FALSE
ON CONFLICT (mcp_server_id, remote_session_client_id) WHERE deleted IS FALSE
DO UPDATE SET updated_at = clock_timestamp();

-- name: CaptureSoleConsumerAssociations :execrows
-- Associates every client bound to a user session issuer with the issuer's
-- MCP server when that server is the issuer's only consumer in the
-- organization. With one consumer the binding can only be meant for that
-- server, so the association is proven rather than guessed. Issuers shared by
-- several servers, or also fronting a gateway, prove nothing and are left for
-- explicit configuration.
--
-- Tenancy: the ids arrive from callers that already hold the issuer, but are
-- re-pinned to the caller's project and organization so a foreign id writes
-- nothing, and only the organization's own servers and gateways count as
-- other consumers.
INSERT INTO mcp_server_remote_session_clients (project_id, organization_id, mcp_server_id, remote_session_client_id, client_attachment_scope)
SELECT s.project_id, p.organization_id, s.id, c.id, c.attachment_scope
FROM mcp_servers AS s
JOIN projects AS p
  ON p.id = s.project_id
 AND p.organization_id = @organization_id::text
 AND p.deleted IS FALSE
JOIN user_session_issuers AS usi
  ON usi.id = s.user_session_issuer_id
 AND usi.deleted IS FALSE
 AND (usi.project_id = p.id
      OR (usi.project_id IS NULL AND usi.organization_id = p.organization_id))
JOIN remote_session_client_user_session_issuers AS link
  ON link.user_session_issuer_id = usi.id
JOIN remote_session_clients AS c
  ON c.id = link.remote_session_client_id
 AND c.deleted IS FALSE
 AND (c.project_id = p.id
      OR (c.project_id IS NULL AND c.organization_id = p.organization_id)
      OR (c.project_id IS NULL AND c.organization_id IS NULL))
WHERE s.project_id = @project_id::uuid
  AND s.deleted IS FALSE
  AND s.user_session_issuer_id = ANY(@user_session_issuer_ids::uuid[])
  AND NOT EXISTS (
    SELECT 1
    FROM mcp_servers AS other
    JOIN projects AS other_project
      ON other_project.id = other.project_id
     AND other_project.organization_id = p.organization_id
     AND other_project.deleted IS FALSE
    WHERE other.user_session_issuer_id = s.user_session_issuer_id
      AND other.id <> s.id
      AND other.deleted IS FALSE
  )
  AND NOT EXISTS (
    SELECT 1
    FROM meta_mcp_servers AS gateway
    JOIN projects AS gateway_project
      ON gateway_project.id = gateway.project_id
     AND gateway_project.deleted IS FALSE
    WHERE gateway.user_session_issuer_id = s.user_session_issuer_id
      AND gateway.organization_id = p.organization_id
      AND gateway.deleted IS FALSE
  )
ON CONFLICT (mcp_server_id, remote_session_client_id) WHERE deleted IS FALSE DO NOTHING;

-- name: CaptureGatewayDerivedAssociations :execrows
-- Associates a gateway-fronted server in the project with the one client its
-- gateway issuer binds for the server's derived authorization server, which
-- is how consent claims a member for a gateway client. Runs for a gateway
-- issuer named in user_session_issuer_ids, or for a server whose own issuer
-- is named there, since that issuer derives the server's authorization
-- server. It writes nothing when several of the issuer's clients match, when
-- a grant the issuer offers for the server's remote upstream routes it to
-- another client (a grant minted through the issuer, or one for a client the
-- issuer binds, whichever issuer minted it; a tunnel's claimed resource
-- proves nothing), or when an explicit detach was recorded for the pair.
WITH fronted AS (
    SELECT gateway.user_session_issuer_id AS issuer_id, member.mcp_server_id AS server_id
    FROM meta_mcp_servers AS gateway
    JOIN user_session_issuers AS gateway_issuer
      ON gateway_issuer.id = gateway.user_session_issuer_id
     AND gateway_issuer.deleted IS FALSE
     AND (gateway_issuer.project_id = gateway.project_id
          OR (gateway_issuer.project_id IS NULL AND gateway_issuer.organization_id = gateway.organization_id))
    JOIN meta_mcp_server_members AS member
      ON member.meta_mcp_server_id = gateway.id
     AND member.project_id = gateway.project_id
     AND member.deleted IS FALSE
    WHERE gateway.project_id = @project_id::uuid
      AND gateway.organization_id = @organization_id::text
      AND gateway.deleted IS FALSE
    UNION
    SELECT organization_issuer.id AS issuer_id, s.id AS server_id
    FROM user_session_issuers AS organization_issuer
    JOIN mcp_servers AS s
      ON s.project_id = @project_id::uuid
    WHERE organization_issuer.project_id IS NULL
      AND organization_issuer.organization_id = @organization_id::text
      AND organization_issuer.deleted IS FALSE
      AND (
        SELECT count(*)
        FROM user_session_issuers AS sibling
        WHERE sibling.organization_id = organization_issuer.organization_id
          AND sibling.project_id IS NULL
          AND sibling.deleted IS FALSE
      ) = 1
)
INSERT INTO mcp_server_remote_session_clients (project_id, organization_id, mcp_server_id, remote_session_client_id, client_attachment_scope)
SELECT s.project_id, p.organization_id, s.id, c.id, c.attachment_scope
FROM fronted AS f
JOIN mcp_servers AS s
  ON s.id = f.server_id
 AND s.project_id = @project_id::uuid
 AND s.deleted IS FALSE
 AND s.remote_session_issuer_id IS NOT NULL
 AND (s.remote_mcp_server_id IS NOT NULL OR s.tunneled_mcp_server_id IS NOT NULL)
JOIN projects AS p
  ON p.id = s.project_id
 AND p.organization_id = @organization_id::text
 AND p.deleted IS FALSE
JOIN remote_session_client_user_session_issuers AS link
  ON link.user_session_issuer_id = f.issuer_id
JOIN remote_session_issuers AS derived
  ON derived.id = s.remote_session_issuer_id
 AND derived.deleted IS FALSE
 AND (derived.project_id = p.id
      OR (derived.project_id IS NULL AND derived.organization_id = p.organization_id)
      OR (derived.project_id IS NULL AND derived.organization_id IS NULL))
JOIN remote_session_clients AS c
  ON c.id = link.remote_session_client_id
 AND c.deleted IS FALSE
 AND c.remote_session_issuer_id = derived.id
 AND (c.project_id = p.id
      OR (c.project_id IS NULL AND c.organization_id = p.organization_id)
      OR (c.project_id IS NULL AND c.organization_id IS NULL))
LEFT JOIN remote_mcp_servers AS r
  ON r.id = s.remote_mcp_server_id
 AND r.project_id = s.project_id
 AND r.deleted IS FALSE
WHERE (f.issuer_id = ANY(@user_session_issuer_ids::uuid[]) OR s.user_session_issuer_id = ANY(@user_session_issuer_ids::uuid[]))
  AND NOT EXISTS (
    SELECT 1
    FROM remote_session_client_user_session_issuers AS other_link
    JOIN remote_session_clients AS other
      ON other.id = other_link.remote_session_client_id
     AND other.deleted IS FALSE
    WHERE other_link.user_session_issuer_id = f.issuer_id
      AND other.id <> c.id
      AND other.remote_session_issuer_id = s.remote_session_issuer_id
  )
  AND NOT EXISTS (
    SELECT 1
    FROM remote_sessions AS rs
    WHERE rs.remote_session_client_id <> c.id
      AND rs.deleted IS FALSE
      AND r.url IS NOT NULL
      AND rtrim(rs.resource, '/') = rtrim(r.url, '/')
      AND EXISTS (
        SELECT 1
        FROM user_session_issuers AS minted
        LEFT JOIN projects AS minted_project ON minted_project.id = minted.project_id
        WHERE minted.id = rs.user_session_issuer_id
          AND COALESCE(minted.organization_id, minted_project.organization_id) = p.organization_id
      )
      AND (
        rs.user_session_issuer_id = f.issuer_id
        OR EXISTS (
          SELECT 1
          FROM remote_session_client_user_session_issuers AS grant_link
          WHERE grant_link.remote_session_client_id = rs.remote_session_client_id
            AND grant_link.user_session_issuer_id = f.issuer_id
        )
      )
  )
  AND NOT EXISTS (
    SELECT 1
    FROM mcp_server_remote_session_clients AS recorded
    WHERE recorded.mcp_server_id = s.id
      AND recorded.remote_session_client_id = c.id
  )
ON CONFLICT (mcp_server_id, remote_session_client_id) WHERE deleted IS FALSE DO NOTHING;

-- name: PruneUnservableAssociations :execrows
-- Removes the project's associations no issuer can serve: the server is gone,
-- the client is deleted, or the client is bound neither to the server's own
-- user session issuer nor to the live issuer of a gateway fronting the server
-- (a gateway it is a member of, or its organization's only organization-level
-- issuer for the organization gateway). Rows are deleted outright rather
-- than soft-deleted: a soft-deleted row records an explicit detach, which
-- capture and the backfill keep, and automatic cleanup must not read as one,
-- so a server that regains its binding or rejoins a gateway is captured again.
DELETE FROM mcp_server_remote_session_clients AS a
USING mcp_servers AS s
WHERE a.mcp_server_id = s.id
  AND a.project_id = @project_id::uuid
  AND a.organization_id = @organization_id::text
  AND a.deleted IS FALSE
  AND (
    s.deleted IS TRUE
    OR NOT EXISTS (
      SELECT 1
      FROM remote_session_client_user_session_issuers AS link
      JOIN remote_session_clients AS c
        ON c.id = link.remote_session_client_id
       AND c.deleted IS FALSE
      WHERE link.remote_session_client_id = a.remote_session_client_id
        AND (
          link.user_session_issuer_id = s.user_session_issuer_id
          OR EXISTS (
            SELECT 1
            FROM meta_mcp_servers AS gateway
            JOIN projects AS gateway_project
              ON gateway_project.id = gateway.project_id
             AND gateway_project.deleted IS FALSE
            JOIN user_session_issuers AS gateway_issuer
              ON gateway_issuer.id = gateway.user_session_issuer_id
             AND gateway_issuer.deleted IS FALSE
             AND (gateway_issuer.project_id = s.project_id
                  OR (gateway_issuer.project_id IS NULL AND gateway_issuer.organization_id = a.organization_id))
            JOIN meta_mcp_server_members AS member
              ON member.meta_mcp_server_id = gateway.id
             AND member.project_id = gateway.project_id
             AND member.deleted IS FALSE
            WHERE gateway.user_session_issuer_id = link.user_session_issuer_id
              AND gateway.deleted IS FALSE
              AND member.mcp_server_id = s.id
          )
          OR EXISTS (
            SELECT 1
            FROM user_session_issuers AS organization_issuer
            WHERE organization_issuer.id = link.user_session_issuer_id
              AND organization_issuer.project_id IS NULL
              AND organization_issuer.organization_id = a.organization_id
              AND organization_issuer.deleted IS FALSE
              AND (
              SELECT count(*)
              FROM user_session_issuers AS sibling
              WHERE sibling.organization_id = organization_issuer.organization_id
                AND sibling.project_id IS NULL
                AND sibling.deleted IS FALSE
            ) = 1
          )
        )
    )
  );

-- name: PruneRemoteSessionClientAssociations :execrows
-- The client-keyed form of PruneUnservableAssociations, for a change to one
-- client's bindings that reaches servers in several of the organization's
-- projects: an organization issuer losing the client, or the client being
-- deleted. Deletes outright for the same reason.
DELETE FROM mcp_server_remote_session_clients AS a
USING mcp_servers AS s
WHERE a.mcp_server_id = s.id
  AND a.remote_session_client_id = @remote_session_client_id::uuid
  AND a.organization_id = @organization_id::text
  AND a.deleted IS FALSE
  AND (
    s.deleted IS TRUE
    OR NOT EXISTS (
      SELECT 1
      FROM remote_session_client_user_session_issuers AS link
      JOIN remote_session_clients AS c
        ON c.id = link.remote_session_client_id
       AND c.deleted IS FALSE
      WHERE link.remote_session_client_id = a.remote_session_client_id
        AND (
          link.user_session_issuer_id = s.user_session_issuer_id
          OR EXISTS (
            SELECT 1
            FROM meta_mcp_servers AS gateway
            JOIN projects AS gateway_project
              ON gateway_project.id = gateway.project_id
             AND gateway_project.deleted IS FALSE
            JOIN user_session_issuers AS gateway_issuer
              ON gateway_issuer.id = gateway.user_session_issuer_id
             AND gateway_issuer.deleted IS FALSE
             AND (gateway_issuer.project_id = s.project_id
                  OR (gateway_issuer.project_id IS NULL AND gateway_issuer.organization_id = a.organization_id))
            JOIN meta_mcp_server_members AS member
              ON member.meta_mcp_server_id = gateway.id
             AND member.project_id = gateway.project_id
             AND member.deleted IS FALSE
            WHERE gateway.user_session_issuer_id = link.user_session_issuer_id
              AND gateway.deleted IS FALSE
              AND member.mcp_server_id = s.id
          )
          OR EXISTS (
            SELECT 1
            FROM user_session_issuers AS organization_issuer
            WHERE organization_issuer.id = link.user_session_issuer_id
              AND organization_issuer.project_id IS NULL
              AND organization_issuer.organization_id = a.organization_id
              AND organization_issuer.deleted IS FALSE
              AND (
              SELECT count(*)
              FROM user_session_issuers AS sibling
              WHERE sibling.organization_id = organization_issuer.organization_id
                AND sibling.project_id IS NULL
                AND sibling.deleted IS FALSE
            ) = 1
          )
        )
    )
  );

-- name: RemoveGlobalClientAssociations :execrows
-- Removes every association of a deleted platform-global client, in every
-- organization. Only the platform-administered global client deletion calls
-- it, and it touches nothing but that one client's rows. Deletes outright,
-- like the prunes.
DELETE FROM mcp_server_remote_session_clients AS a
USING remote_session_clients AS c
WHERE c.id = a.remote_session_client_id
  AND c.project_id IS NULL
  AND c.organization_id IS NULL
  AND c.deleted IS TRUE
  AND a.remote_session_client_id = @remote_session_client_id::uuid
  AND a.deleted IS FALSE;

-- name: ListMCPServerRemoteSessionClients :many
-- The upstream OAuth clients one MCP server is associated with, and how many
-- of the organization's live servers share each one, so a caller can warn
-- that editing a shared client changes every server using it. A platform
-- client's use by other organizations is never counted.
SELECT
    a.remote_session_client_id,
    a.created_at                           AS associated_at,
    c.client_id                            AS external_client_id,
    c.project_id                           AS client_project_id,
    c.organization_id                      AS client_organization_id,
    c.remote_session_issuer_id,
    i.slug                                 AS issuer_slug,
    i.name                                 AS issuer_name,
    i.issuer                               AS issuer_url,
    (
      SELECT count(*)
      FROM mcp_server_remote_session_clients AS shared
      JOIN mcp_servers AS shared_server
        ON shared_server.id = shared.mcp_server_id
       AND shared_server.deleted IS FALSE
      WHERE shared.remote_session_client_id = a.remote_session_client_id
        AND shared.organization_id = a.organization_id
        AND shared.deleted IS FALSE
    )::bigint                              AS server_count
FROM mcp_server_remote_session_clients AS a
JOIN remote_session_clients AS c
  ON c.id = a.remote_session_client_id
 AND c.deleted IS FALSE
JOIN remote_session_issuers AS i
  ON i.id = c.remote_session_issuer_id
WHERE a.mcp_server_id = @mcp_server_id::uuid
  AND a.project_id = @project_id::uuid
  AND a.deleted IS FALSE
ORDER BY a.created_at, a.remote_session_client_id;

-- name: DetachMCPServerRemoteSessionClient :execrows
-- Removes one server's association. The client and its other associations
-- are untouched. The row is soft-deleted so capture and the backfill keep the
-- explicit detach.
UPDATE mcp_server_remote_session_clients
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE mcp_server_id = @mcp_server_id::uuid
  AND remote_session_client_id = @remote_session_client_id::uuid
  AND project_id = @project_id::uuid
  AND deleted IS FALSE;

-- name: IsSoleConsumerBinding :one
-- Whether the client is bound to the server's user session issuer and the
-- server is that issuer's only consumer in its organization, in which case an
-- association is re-derived from the binding and cannot be detached on its
-- own.
SELECT EXISTS (
  SELECT 1
  FROM mcp_servers AS s
  JOIN projects AS p
    ON p.id = s.project_id
  JOIN remote_session_client_user_session_issuers AS link
    ON link.user_session_issuer_id = s.user_session_issuer_id
   AND link.remote_session_client_id = @remote_session_client_id::uuid
  WHERE s.id = @mcp_server_id::uuid
    AND s.project_id = @project_id::uuid
    AND s.deleted IS FALSE
    AND NOT EXISTS (
      SELECT 1
      FROM mcp_servers AS other
      JOIN projects AS other_project
        ON other_project.id = other.project_id
       AND other_project.organization_id = p.organization_id
       AND other_project.deleted IS FALSE
      WHERE other.user_session_issuer_id = s.user_session_issuer_id
        AND other.id <> s.id
        AND other.deleted IS FALSE
    )
    AND NOT EXISTS (
      SELECT 1
      FROM meta_mcp_servers AS gateway
      JOIN projects AS gateway_project
        ON gateway_project.id = gateway.project_id
       AND gateway_project.deleted IS FALSE
      WHERE gateway.user_session_issuer_id = s.user_session_issuer_id
        AND gateway.organization_id = p.organization_id
        AND gateway.deleted IS FALSE
    )
) AS sole_consumer;

-- name: ListServerAssociatedClients :many
-- The live clients associated with each of the given servers, for consent to
-- offer a server only its own clients.
SELECT a.mcp_server_id, a.remote_session_client_id
FROM mcp_server_remote_session_clients AS a
JOIN remote_session_clients AS c
  ON c.id = a.remote_session_client_id
 AND c.deleted IS FALSE
WHERE a.mcp_server_id = ANY(@mcp_server_ids::uuid[])
  AND a.organization_id = @organization_id::text
  AND a.deleted IS FALSE;

-- name: GetAuthClientSelection :many
-- One credential's choices for MCP servers, read the way the typed auth
-- client resolver reads it: the auth client the choice names, whether that
-- client is still usable for the server, and what the server says about
-- needing upstream authentication. A client outside the server's tenant reads
-- as absent, never as another tenant's client.
-- A client the backfill copied from a server's settings, offered through a
-- backfilled association, is checked against the settings it mirrors, which
-- server edits still write: its OAuth registration must still be attached to
-- the server, its header copies must still match the server's headers, and the
-- server's credential headers must be carried by it. A client or association
-- an administrator created is authoritative and mirrors nothing, so those
-- checks do not apply to it.
WITH selected AS (
  SELECT no_auth, auth_client_id, remote_session_client_id, mcp_server_id, project_id, organization_id,
         FALSE AS ambiguous
  FROM upstream_client_selections
  WHERE NOT @proposed::boolean
    AND credential_kind = @credential_kind::text
    AND credential_id = @credential_id::uuid
    AND mcp_server_id = ANY(@mcp_server_ids::uuid[])
    AND organization_id = @organization_id::text
    AND deleted IS FALSE
  UNION ALL
  SELECT sqlc.narg('proposed_registration_id')::uuid IS NULL AND sqlc.narg('proposed_auth_client_id')::uuid IS NULL,
         candidates.auth_client_id,
         sqlc.narg('proposed_registration_id')::uuid, m.id, m.project_id, p.organization_id,
         candidates.ambiguous
  FROM mcp_servers AS m
  JOIN projects AS p ON p.id = m.project_id
   AND p.organization_id = @organization_id::text AND p.deleted IS FALSE
  CROSS JOIN LATERAL (
    SELECT CASE WHEN count(*) = 1 THEN min(c.id::text)::uuid END AS auth_client_id,
           count(*) > 1 AS ambiguous
    FROM mcp_server_auth_clients AS link
    JOIN mcp_auth_clients AS c ON c.id = link.auth_client_id
     AND c.organization_id = link.organization_id
     AND (c.project_id IS NULL OR c.project_id = m.project_id)
     AND c.deleted IS FALSE
    WHERE link.mcp_server_id = m.id AND link.organization_id = p.organization_id
      AND link.deleted IS FALSE
      AND (
        -- A named client is the only candidate, whatever its type.
        (sqlc.narg('proposed_auth_client_id')::uuid IS NOT NULL
         AND c.id = sqlc.narg('proposed_auth_client_id')::uuid)
        OR (sqlc.narg('proposed_auth_client_id')::uuid IS NULL
            AND ((sqlc.narg('proposed_registration_id')::uuid IS NOT NULL
                  AND c.remote_session_client_id = sqlc.narg('proposed_registration_id')::uuid
                  AND c.client_type IN ('remote_session', 'service_account'))
                 OR (sqlc.narg('proposed_registration_id')::uuid IS NULL
                     AND c.client_type IN ('static_credential', 'pass_through'))))
      )
  ) AS candidates
  WHERE @proposed::boolean AND m.id = ANY(@mcp_server_ids::uuid[]) AND m.deleted IS FALSE
)
SELECT sel.mcp_server_id,
       sel.ambiguous,
       sel.no_auth,
       sel.auth_client_id,
       m.name AS server_name,
       (m.tunneled_mcp_server_id IS NOT NULL)::boolean AS tunneled,
       ac.id AS client_id,
       ac.client_type,
       ac.enabled AS client_enabled,
       ac.deleted AS client_deleted,
       reg.id AS registration_id,
       reg.remote_session_issuer_id AS registration_issuer_id,
       (
         reg.id IS NOT NULL
         AND reg.deleted IS FALSE
         AND rsi.id IS NOT NULL
         AND rsi.deleted IS FALSE
       )::boolean AS registration_live,
       (
         NOT copy.mirrored
         OR EXISTS (
           SELECT 1
           FROM mcp_server_remote_session_clients AS a
           WHERE a.mcp_server_id = sel.mcp_server_id
             AND a.remote_session_client_id = ac.remote_session_client_id
             AND a.organization_id = sel.organization_id
             AND a.deleted IS FALSE
         )
       )::boolean AS registration_attached,
       EXISTS (
         SELECT 1
         FROM mcp_server_auth_clients AS link
         WHERE link.mcp_server_id = sel.mcp_server_id
           AND link.auth_client_id = ac.id
           AND link.organization_id = sel.organization_id
           AND link.deleted IS FALSE
       )::boolean AS associated,
       m.requires_upstream_auth,
       (
         EXISTS (
           SELECT 1
           FROM mcp_server_auth_clients AS link
           JOIN mcp_auth_clients AS other
             ON other.id = link.auth_client_id
            AND other.organization_id = link.organization_id
            AND other.deleted IS FALSE
           WHERE link.mcp_server_id = sel.mcp_server_id
             AND link.organization_id = sel.organization_id
             AND link.deleted IS FALSE
         )
         OR EXISTS (
           SELECT 1
           FROM mcp_server_remote_session_clients AS a
           WHERE a.mcp_server_id = sel.mcp_server_id
             AND a.organization_id = sel.organization_id
             AND a.deleted IS FALSE
         )
       )::boolean AS server_has_clients,
       EXISTS (
         SELECT 1
         FROM mcp_server_auth_clients AS link
         JOIN mcp_auth_clients AS other
           ON other.id = link.auth_client_id
          AND other.organization_id = link.organization_id
          AND other.deleted IS FALSE
          AND other.client_type IN ('static_credential', 'pass_through')
         WHERE link.mcp_server_id = sel.mcp_server_id
           AND link.organization_id = sel.organization_id
           AND link.deleted IS FALSE
       )::boolean AS server_has_header_clients,
       (
         EXISTS (
           SELECT 1
           FROM mcp_server_auth_clients AS link
           JOIN mcp_auth_clients AS other
             ON other.id = link.auth_client_id
            AND other.organization_id = link.organization_id
            AND other.deleted IS FALSE
            AND other.client_type NOT IN ('static_credential', 'pass_through')
           WHERE link.mcp_server_id = sel.mcp_server_id
             AND link.organization_id = sel.organization_id
             AND link.deleted IS FALSE
         )
         OR EXISTS (
           SELECT 1
           FROM mcp_server_remote_session_clients AS a
           WHERE a.mcp_server_id = sel.mcp_server_id
             AND a.organization_id = sel.organization_id
             AND a.deleted IS FALSE
         )
       )::boolean AS server_has_oauth_clients,
       (copy.mirrored AND EXISTS (
         SELECT 1
         FROM mcp_auth_client_headers AS c
         WHERE c.auth_client_id = ac.id
           AND c.organization_id = sel.organization_id
           AND c.deleted IS FALSE
           AND NOT EXISTS (
             SELECT 1
             FROM remote_mcp_server_headers AS h
             WHERE h.remote_mcp_server_id = m.remote_mcp_server_id
               AND h.deleted IS FALSE
               AND h.name = c.name
               AND h.value IS NOT DISTINCT FROM c.value
               AND h.value_from_request_header IS NOT DISTINCT FROM c.value_from_request_header
               AND h.is_required = c.is_required
               AND h.is_secret = c.is_secret
           )
       ))::boolean AS headers_stale,
       (copy.mirrored AND EXISTS (
         SELECT 1
         FROM remote_mcp_server_headers AS h
         WHERE h.remote_mcp_server_id = m.remote_mcp_server_id
           AND h.deleted IS FALSE
           AND (lower(h.name) = 'authorization' OR h.value_from_request_header IS NOT NULL OR h.is_secret)
           -- An OAuth client's bearer replaces the server's Authorization.
           AND NOT (lower(h.name) = 'authorization' AND COALESCE(ac.client_type IN ('remote_session', 'service_account'), FALSE))
           AND NOT EXISTS (
             SELECT 1
             FROM mcp_auth_client_headers AS c
             WHERE c.auth_client_id = ac.id
               AND c.organization_id = sel.organization_id
               AND c.deleted IS FALSE
               AND lower(c.name) = lower(h.name)
           )
       ))::boolean AS credential_headers_uncovered,
       EXISTS (
         SELECT 1
         FROM mcp_auth_client_headers AS c
         WHERE c.auth_client_id = ac.id
           AND c.organization_id = sel.organization_id
           AND c.deleted IS FALSE
           AND c.is_required
           -- Mirrors proxy.IsDeniedPassThroughSource.
           AND replace(lower(btrim(c.value_from_request_header)), '_', '-')
               IN ('authorization', 'cookie', 'set-cookie', 'proxy-authorization')
       )::boolean AS requires_denied_source,
       EXISTS (
         SELECT 1
         FROM mcp_server_auth_clients AS link
         JOIN mcp_auth_clients AS e
           ON e.id = link.auth_client_id
          AND e.organization_id = link.organization_id
          AND e.client_type = 'ema'
          AND e.enabled IS TRUE
          AND e.deleted IS FALSE
         JOIN mcp_auth_client_emas AS x
           ON x.auth_client_id = e.id
          AND x.deleted IS FALSE
         WHERE link.mcp_server_id = sel.mcp_server_id
           AND link.organization_id = sel.organization_id
           AND link.deleted IS FALSE
           AND e.remote_session_client_id = ac.remote_session_client_id
       )::boolean AS registration_has_ema
FROM selected AS sel
JOIN mcp_servers AS m
  ON m.id = sel.mcp_server_id
 AND m.project_id = sel.project_id
 AND m.deleted IS FALSE
LEFT JOIN mcp_auth_clients AS ac
  ON ac.id = sel.auth_client_id
 AND ac.organization_id = sel.organization_id
 AND (ac.project_id IS NULL OR ac.project_id = sel.project_id)
CROSS JOIN LATERAL (
  -- No client reads as mirrored, so a no-auth choice keeps being checked
  -- against the server's credential headers.
  SELECT (ac.id IS NULL OR (ac.source = 'backfill' AND NOT EXISTS (
    SELECT 1
    FROM mcp_server_auth_clients AS link
    WHERE link.mcp_server_id = sel.mcp_server_id
      AND link.auth_client_id = ac.id
      AND link.organization_id = sel.organization_id
      AND link.deleted IS FALSE
      AND link.source <> 'backfill'
  )))::boolean AS mirrored
) AS copy
LEFT JOIN remote_session_clients AS reg
  ON reg.id = COALESCE(ac.remote_session_client_id, CASE WHEN @proposed::boolean THEN sel.remote_session_client_id END)
LEFT JOIN remote_session_issuers AS rsi
  ON rsi.id = reg.remote_session_issuer_id;

-- name: ListAuthClientHeaders :many
-- A live auth client's own headers, for the proxy to send after the server's
-- non-credential headers. Values stay as stored; secret ones are encrypted.
SELECT h.auth_client_id, h.name, h.is_required, h.is_secret, h.value, h.value_from_request_header
FROM mcp_auth_client_headers AS h
JOIN mcp_auth_clients AS ac
  ON ac.id = h.auth_client_id
 AND ac.organization_id = h.organization_id
 AND ac.deleted IS FALSE
WHERE h.auth_client_id = ANY(@auth_client_ids::uuid[])
  AND h.organization_id = @organization_id::text
  AND h.deleted IS FALSE
ORDER BY h.name, h.id;

-- name: ListServerConsentAuthClients :many
-- The enabled auth clients each MCP server offers, for the consent screen to
-- choose among, with whether the server needs upstream authentication. A
-- server with no enabled client still has a row, with no client, so the
-- screen can tell no-auth from incomplete setup.
SELECT m.id AS mcp_server_id,
       m.requires_upstream_auth,
       c.id AS auth_client_id,
       c.name AS auth_client_name,
       c.client_type,
       c.remote_session_client_id
FROM mcp_servers AS m
JOIN projects AS p
  ON p.id = m.project_id
 AND p.organization_id = @organization_id::text
 AND p.deleted IS FALSE
LEFT JOIN mcp_server_auth_clients AS link
  ON link.mcp_server_id = m.id
 AND link.organization_id = p.organization_id
 AND link.deleted IS FALSE
LEFT JOIN mcp_auth_clients AS c
  ON c.id = link.auth_client_id
 AND c.organization_id = link.organization_id
 AND (c.project_id IS NULL OR c.project_id = m.project_id)
 AND c.deleted IS FALSE
 AND c.enabled
WHERE m.id = ANY(@mcp_server_ids::uuid[])
  AND m.deleted IS FALSE
ORDER BY m.id, c.name, c.id;

-- name: ListRememberedAuthClients :many
-- The auth client a subject most recently approved for each MCP server
-- through any of their user sessions. It only prefills a new consent and is
-- checked again before use. A latest approval with no client remembers
-- nothing.
SELECT latest.mcp_server_id,
       latest.auth_client_id::uuid AS auth_client_id
FROM (
  SELECT DISTINCT ON (sel.mcp_server_id)
         sel.mcp_server_id,
         sel.auth_client_id
  FROM upstream_client_selections AS sel
  JOIN user_sessions AS us
    ON COALESCE(us.credential_id, us.id) = sel.credential_id
   AND us.subject_urn = @subject_urn::text
  WHERE sel.credential_kind = 'user_session'
    AND sel.organization_id = @organization_id::text
    AND sel.mcp_server_id = ANY(@mcp_server_ids::uuid[])
    AND sel.deleted IS FALSE
  ORDER BY sel.mcp_server_id, sel.created_at DESC
) AS latest
WHERE latest.auth_client_id IS NOT NULL;

-- name: ListGatewayMembersAddedAfterIssuance :many
-- The members, of those given, that joined the gateway after the credential
-- was first issued: a server added to a gateway's member list since, or, for
-- the organization gateway, which has no list, a server created since. A
-- member present at issuance was considered then, whatever its choice.
SELECT s.id
FROM mcp_servers AS s
JOIN projects AS p
  ON p.id = s.project_id
 AND p.organization_id = @organization_id::text
LEFT JOIN meta_mcp_server_members AS m
  ON m.mcp_server_id = s.id
 AND m.meta_mcp_server_id = sqlc.narg('meta_mcp_server_id')::uuid
 AND m.deleted IS FALSE
WHERE s.id = ANY(@mcp_server_ids::uuid[])
  AND s.deleted IS FALSE
  -- The subject and sign-in bound the lineage lookup to one person's
  -- sessions, which an index serves.
  AND COALESCE(m.created_at, s.created_at) > (
    SELECT min(us.created_at)
    FROM user_sessions AS us
    WHERE us.subject_urn = @subject_urn::text
      AND us.user_session_issuer_id = @user_session_issuer_id::uuid
      AND COALESCE(us.credential_id, us.id) = @credential_id::uuid
  );

-- name: RecordAuthClientChoice :execrows
-- Records one credential's choice for one MCP server: the auth client named,
-- which must still be enabled and offered by the server, or no-auth when none
-- is. A choice already recorded, by a concurrent request or otherwise, is
-- kept. Callers validate the choice first; this only refuses one the server
-- no longer offers.
INSERT INTO upstream_client_selections (
    project_id,
    organization_id,
    credential_kind,
    credential_id,
    mcp_server_id,
    remote_session_client_id,
    client_attachment_scope,
    no_auth,
    auth_client_id,
    auth_client_attachment_scope,
    source
)
SELECT s.project_id,
       p.organization_id,
       @credential_kind::text,
       @credential_id::uuid,
       s.id,
       ac.remote_session_client_id,
       reg.attachment_scope,
       ac.id IS NULL,
       ac.id,
       ac.attachment_scope,
       @source::text
FROM mcp_servers AS s
JOIN projects AS p
  ON p.id = s.project_id
 AND p.organization_id = @organization_id::text
 AND p.deleted IS FALSE
LEFT JOIN mcp_auth_clients AS ac
  ON ac.id = sqlc.narg('auth_client_id')::uuid
 AND ac.organization_id = p.organization_id
 AND (ac.project_id IS NULL OR ac.project_id = s.project_id)
 AND ac.deleted IS FALSE
 AND ac.enabled
 AND EXISTS (
   SELECT 1
   FROM mcp_server_auth_clients AS link
   WHERE link.mcp_server_id = s.id
     AND link.auth_client_id = ac.id
     AND link.organization_id = p.organization_id
     AND link.deleted IS FALSE
 )
LEFT JOIN remote_session_clients AS reg
  ON reg.id = ac.remote_session_client_id
 AND reg.deleted IS FALSE
WHERE s.id = @mcp_server_id::uuid
  AND s.deleted IS FALSE
  AND (sqlc.narg('auth_client_id')::uuid IS NULL OR ac.id IS NOT NULL)
  AND (ac.remote_session_client_id IS NULL OR reg.id IS NOT NULL)
ON CONFLICT (credential_kind, credential_id, mcp_server_id) WHERE deleted IS FALSE DO NOTHING;

-- name: CopyCredentialChoices :execrows
-- Gives a replacement credential the choices of the one it replaces.
INSERT INTO upstream_client_selections (
    project_id, organization_id, credential_kind, credential_id, mcp_server_id,
    remote_session_client_id, client_attachment_scope, no_auth, auth_client_id,
    auth_client_attachment_scope, source
)
SELECT project_id, organization_id, credential_kind, @to_credential_id::uuid, mcp_server_id,
       remote_session_client_id, client_attachment_scope, no_auth, auth_client_id,
       auth_client_attachment_scope, source
FROM upstream_client_selections
WHERE credential_kind = @credential_kind::text
  AND credential_id = @from_credential_id::uuid
  AND organization_id = @organization_id::text
  AND deleted IS FALSE
ON CONFLICT (credential_kind, credential_id, mcp_server_id) WHERE deleted IS FALSE DO NOTHING;

-- name: HasCredentialChoice :one
SELECT EXISTS (
  SELECT 1
  FROM upstream_client_selections
  WHERE credential_kind = @credential_kind::text
    AND credential_id = @credential_id::uuid
    AND mcp_server_id = @mcp_server_id::uuid
    AND organization_id = @organization_id::text
    AND deleted IS FALSE
)::boolean AS has_choice;

-- name: GetIssuanceProfileForCaller :one
-- The profile configured for one headless caller signing in through one user
-- session issuer: a client, or a workload issuer and subject.
SELECT p.id
FROM issuance_selection_profiles AS p
WHERE p.user_session_issuer_id = @user_session_issuer_id::uuid
  AND p.organization_id = @organization_id::text
  AND p.deleted IS FALSE
  AND (
    (sqlc.narg('user_session_client_id')::uuid IS NOT NULL AND p.user_session_client_id = sqlc.narg('user_session_client_id')::uuid)
    OR (sqlc.narg('workload_issuer_id')::uuid IS NOT NULL
        AND p.workload_issuer_id = sqlc.narg('workload_issuer_id')::uuid
        AND p.workload_subject = sqlc.narg('workload_subject')::text)
  );

-- name: ListCredentialChoices :many
-- One credential's (or profile's) recorded choices for the given servers.
SELECT sel.mcp_server_id,
       sel.auth_client_id,
       sel.no_auth
FROM upstream_client_selections AS sel
WHERE sel.credential_kind = @credential_kind::text
  AND sel.credential_id = @credential_id::uuid
  AND sel.organization_id = @organization_id::text
  AND sel.mcp_server_id = ANY(@mcp_server_ids::uuid[])
  AND sel.deleted IS FALSE;
