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

-- name: PruneUnservableAssociations :execrows
-- Removes the project's associations the current serving path can no longer
-- honor: the server is gone or has no user session issuer, the client is
-- deleted, or the client is no longer bound to the server's issuer. Until
-- serving reads associations, an association is only kept while that binding
-- would also serve it.
UPDATE mcp_server_remote_session_clients AS a
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
FROM mcp_servers AS s
WHERE a.mcp_server_id = s.id
  AND a.project_id = @project_id::uuid
  AND a.organization_id = @organization_id::text
  AND a.deleted IS FALSE
  AND (
    s.deleted IS TRUE
    OR s.user_session_issuer_id IS NULL
    OR NOT EXISTS (
      SELECT 1
      FROM remote_session_client_user_session_issuers AS link
      JOIN remote_session_clients AS c
        ON c.id = link.remote_session_client_id
       AND c.deleted IS FALSE
      WHERE link.user_session_issuer_id = s.user_session_issuer_id
        AND link.remote_session_client_id = a.remote_session_client_id
    )
  );

-- name: PruneRemoteSessionClientAssociations :execrows
-- The client-keyed form of PruneUnservableAssociations, for a change to one
-- client's bindings that reaches servers in several of the organization's
-- projects: an organization issuer losing the client, or the client being
-- deleted.
UPDATE mcp_server_remote_session_clients AS a
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
FROM mcp_servers AS s
WHERE a.mcp_server_id = s.id
  AND a.remote_session_client_id = @remote_session_client_id::uuid
  AND a.organization_id = @organization_id::text
  AND a.deleted IS FALSE
  AND (
    s.deleted IS TRUE
    OR s.user_session_issuer_id IS NULL
    OR NOT EXISTS (
      SELECT 1
      FROM remote_session_client_user_session_issuers AS link
      JOIN remote_session_clients AS c
        ON c.id = link.remote_session_client_id
       AND c.deleted IS FALSE
      WHERE link.user_session_issuer_id = s.user_session_issuer_id
        AND link.remote_session_client_id = a.remote_session_client_id
    )
  );

-- name: RemoveGlobalClientAssociations :execrows
-- Removes every association of a deleted platform-global client, in every
-- organization. Only the platform-administered global client deletion calls
-- it, and it touches nothing but that one client's rows.
UPDATE mcp_server_remote_session_clients AS a
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
FROM remote_session_clients AS c
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
-- are untouched.
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
