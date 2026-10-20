-- name: ListAuthClientBackfillCandidates :many
-- One row per live MCP server upstream OAuth client association after the
-- cursor, with the client's typing evidence and the auth client and server
-- link already recorded for it, if any. Read-only: the runner decides from
-- this evidence alone. Only associations whose server, project, client and
-- client issuer are live and in the association's tenant count.
--
-- auth_client_id is the live backfilled auth client already recorded for the
-- client in the association's organization, auth_client_type its type and
-- auth_client_project_id its project; the nil UUID and empty when none is.
-- Admin-created clients are never treated as existing. link_live reports that
-- the server already offers that auth client.
SELECT a.id AS association_id,
       a.project_id,
       a.organization_id,
       a.mcp_server_id,
       c.id AS remote_session_client_id,
       c.project_id AS client_project_id,
       c.credential_owner,
       rsi.name AS issuer_name,
       COALESCE(existing.id, '00000000-0000-0000-0000-000000000000'::uuid)::uuid AS auth_client_id,
       COALESCE(existing.client_type, '')::text AS auth_client_type,
       existing.project_id AS auth_client_project_id,
       (existing.id IS NOT NULL AND EXISTS (
         SELECT 1
         FROM mcp_server_auth_clients AS link
         WHERE link.mcp_server_id = a.mcp_server_id
           AND link.auth_client_id = existing.id
           AND link.organization_id = a.organization_id
           AND link.deleted IS FALSE
       ))::boolean AS link_live
FROM mcp_server_remote_session_clients AS a
JOIN projects AS p
  ON p.id = a.project_id
 AND p.organization_id = a.organization_id
 AND p.deleted IS FALSE
JOIN mcp_servers AS s
  ON s.id = a.mcp_server_id
 AND s.project_id = a.project_id
 AND s.deleted IS FALSE
JOIN remote_session_clients AS c
  ON c.id = a.remote_session_client_id
 AND c.attachment_scope = a.client_attachment_scope
 AND c.deleted IS FALSE
JOIN remote_session_issuers AS rsi
  ON rsi.id = c.remote_session_issuer_id
 AND rsi.deleted IS FALSE
 AND (rsi.project_id = a.project_id
      OR (rsi.project_id IS NULL AND rsi.organization_id = a.organization_id)
      OR (rsi.project_id IS NULL AND rsi.organization_id IS NULL))
LEFT JOIN LATERAL (
    SELECT ac.id, ac.client_type, ac.project_id
    FROM mcp_auth_clients AS ac
    WHERE ac.organization_id = a.organization_id
      AND ac.remote_session_client_id = c.id
      AND ac.client_type IN ('remote_session', 'service_account')
      AND ac.source = 'backfill'
      AND ac.deleted IS FALSE
    ORDER BY ac.id
    LIMIT 1
) AS existing ON TRUE
WHERE a.deleted IS FALSE
  AND a.id > @cursor::uuid
  AND a.organization_id = @organization_id::text
ORDER BY a.id
LIMIT @page_size::int;

-- name: CreateAuthClient :one
-- Records the auth client for an upstream OAuth client in one organization,
-- typed by its credential owner, conditional on the client and its issuer
-- still being live and in that organization's reach, the client owned as
-- decided and associated with a live server of a live project there. A
-- self-owned client is never platform-global. A concurrent run recording the
-- same client fails the type's unique key.
INSERT INTO mcp_auth_clients (organization_id, project_id, client_type, source, name, remote_session_client_id, client_attachment_scope)
SELECT @organization_id::text, sqlc.narg('project_id')::uuid, @client_type::text, 'backfill', @name::text, c.id, c.attachment_scope
FROM remote_session_clients AS c
JOIN remote_session_issuers AS rsi
  ON rsi.id = c.remote_session_issuer_id
 AND rsi.deleted IS FALSE
 AND (rsi.organization_id IS NULL OR rsi.organization_id = @organization_id::text)
WHERE c.id = @remote_session_client_id::uuid
  AND c.deleted IS FALSE
  AND c.credential_owner = CASE @client_type::text WHEN 'service_account' THEN 'self' ELSE 'subject' END
  AND @client_type::text IN ('remote_session', 'service_account')
  AND (c.project_id = sqlc.narg('project_id')::uuid
       OR (c.project_id IS NULL AND c.organization_id = @organization_id::text)
       OR (c.project_id IS NULL AND c.organization_id IS NULL AND @client_type::text = 'remote_session'))
  AND EXISTS (
    SELECT 1
    FROM mcp_server_remote_session_clients AS a
    JOIN projects AS p
      ON p.id = a.project_id
     AND p.organization_id = a.organization_id
     AND p.deleted IS FALSE
    JOIN mcp_servers AS s
      ON s.id = a.mcp_server_id
     AND s.project_id = a.project_id
     AND s.deleted IS FALSE
    WHERE a.remote_session_client_id = c.id
      AND a.client_attachment_scope = c.attachment_scope
      AND a.organization_id = @organization_id::text
      AND a.deleted IS FALSE
      AND (rsi.project_id IS NULL OR rsi.project_id = a.project_id)
  )
RETURNING id;

-- name: LinkAuthClientToServer :execrows
-- Records that a server offers an auth client, conditional on the evidence the
-- candidate was decided from still holding: the association, its server,
-- project, client and the client's issuer live and in the association's
-- tenant, and the auth client still the live backfilled one for that client.
-- Zero rows means the evidence changed or the link already exists.
INSERT INTO mcp_server_auth_clients (project_id, organization_id, mcp_server_id, auth_client_id, client_attachment_scope, source)
SELECT a.project_id, a.organization_id, a.mcp_server_id, ac.id, ac.attachment_scope, 'backfill'
FROM mcp_server_remote_session_clients AS a
JOIN projects AS p
  ON p.id = a.project_id
 AND p.organization_id = a.organization_id
 AND p.deleted IS FALSE
JOIN mcp_servers AS s
  ON s.id = a.mcp_server_id
 AND s.project_id = a.project_id
 AND s.deleted IS FALSE
JOIN remote_session_clients AS c
  ON c.id = a.remote_session_client_id
 AND c.attachment_scope = a.client_attachment_scope
 AND c.deleted IS FALSE
JOIN remote_session_issuers AS rsi
  ON rsi.id = c.remote_session_issuer_id
 AND rsi.deleted IS FALSE
 AND (rsi.project_id = a.project_id
      OR (rsi.project_id IS NULL AND rsi.organization_id = a.organization_id)
      OR (rsi.project_id IS NULL AND rsi.organization_id IS NULL))
JOIN mcp_auth_clients AS ac
  ON ac.id = @auth_client_id::uuid
 AND ac.organization_id = a.organization_id
 AND ac.source = 'backfill'
 AND ac.deleted IS FALSE
 AND (ac.project_id IS NULL OR ac.project_id = a.project_id)
WHERE a.id = @association_id::uuid
  AND a.organization_id = @organization_id::text
  AND a.deleted IS FALSE
  AND ac.remote_session_client_id = a.remote_session_client_id
  AND ac.client_type IN ('remote_session', 'service_account')
  AND NOT EXISTS (
    SELECT 1
    FROM mcp_server_auth_clients AS link
    WHERE link.mcp_server_id = a.mcp_server_id
      AND link.auth_client_id = ac.id
      AND link.deleted IS FALSE
  )
ON CONFLICT (mcp_server_id, auth_client_id) WHERE deleted IS FALSE DO NOTHING;

-- name: ListStaleBackfillAuthClients :many
-- Live backfilled OAuth-backed auth clients in the organization that no live
-- association of a live server in a live project still reaches, or whose
-- upstream client is deleted. Read-only: a dry run counts these.
SELECT ac.id
FROM mcp_auth_clients AS ac
WHERE ac.organization_id = @organization_id::text
  AND ac.source = 'backfill'
  AND ac.client_type IN ('remote_session', 'service_account')
  AND ac.deleted IS FALSE
  AND NOT EXISTS (
    SELECT 1
    FROM mcp_server_remote_session_clients AS a
    JOIN projects AS p
      ON p.id = a.project_id
     AND p.organization_id = a.organization_id
     AND p.deleted IS FALSE
    JOIN mcp_servers AS s
      ON s.id = a.mcp_server_id
     AND s.project_id = a.project_id
     AND s.deleted IS FALSE
    JOIN remote_session_clients AS c
      ON c.id = a.remote_session_client_id
     AND c.attachment_scope = a.client_attachment_scope
     AND c.deleted IS FALSE
    JOIN remote_session_issuers AS rsi
      ON rsi.id = c.remote_session_issuer_id
     AND rsi.deleted IS FALSE
     AND (rsi.project_id = a.project_id
          OR (rsi.project_id IS NULL AND rsi.organization_id = a.organization_id)
          OR (rsi.project_id IS NULL AND rsi.organization_id IS NULL))
    WHERE a.organization_id = ac.organization_id
      AND a.remote_session_client_id = ac.remote_session_client_id
      AND a.deleted IS FALSE
      AND (ac.project_id IS NULL OR a.project_id = ac.project_id)
  )
ORDER BY ac.id;

-- name: RetireStaleBackfillAuthClients :many
-- Soft-deletes the auth clients ListStaleBackfillAuthClients reports,
-- rechecking the same evidence.
UPDATE mcp_auth_clients AS target
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE target.id IN (
  SELECT ac.id
FROM mcp_auth_clients AS ac
WHERE ac.organization_id = @organization_id::text
  AND ac.source = 'backfill'
  AND ac.client_type IN ('remote_session', 'service_account')
  AND ac.deleted IS FALSE
  AND NOT EXISTS (
    SELECT 1
    FROM mcp_server_remote_session_clients AS a
    JOIN projects AS p
      ON p.id = a.project_id
     AND p.organization_id = a.organization_id
     AND p.deleted IS FALSE
    JOIN mcp_servers AS s
      ON s.id = a.mcp_server_id
     AND s.project_id = a.project_id
     AND s.deleted IS FALSE
    JOIN remote_session_clients AS c
      ON c.id = a.remote_session_client_id
     AND c.attachment_scope = a.client_attachment_scope
     AND c.deleted IS FALSE
    JOIN remote_session_issuers AS rsi
      ON rsi.id = c.remote_session_issuer_id
     AND rsi.deleted IS FALSE
     AND (rsi.project_id = a.project_id
          OR (rsi.project_id IS NULL AND rsi.organization_id = a.organization_id)
          OR (rsi.project_id IS NULL AND rsi.organization_id IS NULL))
    WHERE a.organization_id = ac.organization_id
      AND a.remote_session_client_id = ac.remote_session_client_id
      AND a.deleted IS FALSE
      AND (ac.project_id IS NULL OR a.project_id = ac.project_id)
  )
)
RETURNING target.id;

-- name: ListStaleBackfillLinks :many
-- Live backfilled server links in the organization whose auth client is
-- retired, or whose server no longer has a live association with that client
-- (associations may be soft or hard deleted). Read-only.
SELECT l.id
FROM mcp_server_auth_clients AS l
JOIN mcp_auth_clients AS ac
  ON ac.id = l.auth_client_id
 AND ac.organization_id = l.organization_id
WHERE l.organization_id = @organization_id::text
  AND l.source = 'backfill'
  AND l.deleted IS FALSE
  AND ac.source = 'backfill'
  AND ac.client_type IN ('remote_session', 'service_account')
  AND (ac.deleted IS TRUE OR NOT EXISTS (
    SELECT 1
    FROM mcp_server_remote_session_clients AS a
    JOIN projects AS p
      ON p.id = a.project_id
     AND p.organization_id = a.organization_id
     AND p.deleted IS FALSE
    JOIN mcp_servers AS s
      ON s.id = a.mcp_server_id
     AND s.project_id = a.project_id
     AND s.deleted IS FALSE
    JOIN remote_session_clients AS c
      ON c.id = a.remote_session_client_id
     AND c.attachment_scope = a.client_attachment_scope
     AND c.deleted IS FALSE
    JOIN remote_session_issuers AS rsi
      ON rsi.id = c.remote_session_issuer_id
     AND rsi.deleted IS FALSE
     AND (rsi.project_id = a.project_id
          OR (rsi.project_id IS NULL AND rsi.organization_id = a.organization_id)
          OR (rsi.project_id IS NULL AND rsi.organization_id IS NULL))
    WHERE a.organization_id = l.organization_id
      AND a.mcp_server_id = l.mcp_server_id
      AND a.remote_session_client_id = ac.remote_session_client_id
      AND a.deleted IS FALSE
  ))
ORDER BY l.id;

-- name: RetireStaleBackfillLinks :many
-- Soft-deletes the links ListStaleBackfillLinks reports, rechecking the same
-- evidence.
UPDATE mcp_server_auth_clients AS target
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE target.id IN (
  SELECT l.id
FROM mcp_server_auth_clients AS l
JOIN mcp_auth_clients AS ac
  ON ac.id = l.auth_client_id
 AND ac.organization_id = l.organization_id
WHERE l.organization_id = @organization_id::text
  AND l.source = 'backfill'
  AND l.deleted IS FALSE
  AND ac.source = 'backfill'
  AND ac.client_type IN ('remote_session', 'service_account')
  AND (ac.deleted IS TRUE OR NOT EXISTS (
    SELECT 1
    FROM mcp_server_remote_session_clients AS a
    JOIN projects AS p
      ON p.id = a.project_id
     AND p.organization_id = a.organization_id
     AND p.deleted IS FALSE
    JOIN mcp_servers AS s
      ON s.id = a.mcp_server_id
     AND s.project_id = a.project_id
     AND s.deleted IS FALSE
    JOIN remote_session_clients AS c
      ON c.id = a.remote_session_client_id
     AND c.attachment_scope = a.client_attachment_scope
     AND c.deleted IS FALSE
    JOIN remote_session_issuers AS rsi
      ON rsi.id = c.remote_session_issuer_id
     AND rsi.deleted IS FALSE
     AND (rsi.project_id = a.project_id
          OR (rsi.project_id IS NULL AND rsi.organization_id = a.organization_id)
          OR (rsi.project_id IS NULL AND rsi.organization_id IS NULL))
    WHERE a.organization_id = l.organization_id
      AND a.mcp_server_id = l.mcp_server_id
      AND a.remote_session_client_id = ac.remote_session_client_id
      AND a.deleted IS FALSE
  ))
)
RETURNING target.id;

-- name: ListUndecidedUpstreamAuthServers :many
-- The servers among those given, in the organization, whose need for upstream
-- authentication is still undecided and that still have a live association
-- with a live client in reach: the evidence MarkServersRequiringUpstreamAuth
-- requires, so a dry run counts what an apply marks.
SELECT s.id
FROM mcp_servers AS s
JOIN projects AS p
  ON p.id = s.project_id
 AND p.organization_id = @organization_id::text
 AND p.deleted IS FALSE
WHERE s.id = ANY(@mcp_server_ids::uuid[])
  AND s.deleted IS FALSE
  AND s.requires_upstream_auth IS NULL
  AND EXISTS (
    SELECT 1
    FROM mcp_server_remote_session_clients AS a
    JOIN remote_session_clients AS c
      ON c.id = a.remote_session_client_id
     AND c.attachment_scope = a.client_attachment_scope
     AND c.deleted IS FALSE
    JOIN remote_session_issuers AS rsi
      ON rsi.id = c.remote_session_issuer_id
     AND rsi.deleted IS FALSE
     AND (rsi.project_id = a.project_id
          OR (rsi.project_id IS NULL AND rsi.organization_id = a.organization_id)
          OR (rsi.project_id IS NULL AND rsi.organization_id IS NULL))
    WHERE a.mcp_server_id = s.id
      AND a.project_id = s.project_id
      AND a.organization_id = @organization_id::text
      AND a.deleted IS FALSE
  )
ORDER BY s.id;

-- name: MarkServersRequiringUpstreamAuth :many
-- Records that the given servers need upstream authentication, only where it
-- is still undecided and the server offers a live backfilled auth client whose
-- upstream client it still has a live association with. Never overwrites a
-- decision.
UPDATE mcp_servers AS s
SET requires_upstream_auth = TRUE, updated_at = clock_timestamp()
FROM projects AS p
WHERE p.id = s.project_id
  AND p.organization_id = @organization_id::text
  AND p.deleted IS FALSE
  AND s.id = ANY(@mcp_server_ids::uuid[])
  AND s.deleted IS FALSE
  AND s.requires_upstream_auth IS NULL
  AND EXISTS (
    SELECT 1
    FROM mcp_server_auth_clients AS link
    JOIN mcp_auth_clients AS ac
      ON ac.id = link.auth_client_id
     AND ac.organization_id = link.organization_id
     AND ac.source = 'backfill'
     AND ac.deleted IS FALSE
    WHERE link.mcp_server_id = s.id
      AND link.organization_id = @organization_id::text
      AND link.source = 'backfill'
      AND link.deleted IS FALSE
      AND EXISTS (
        SELECT 1
        FROM mcp_server_remote_session_clients AS a
        JOIN remote_session_clients AS c
          ON c.id = a.remote_session_client_id
         AND c.attachment_scope = a.client_attachment_scope
         AND c.deleted IS FALSE
        JOIN remote_session_issuers AS rsi
          ON rsi.id = c.remote_session_issuer_id
         AND rsi.deleted IS FALSE
         AND (rsi.project_id = a.project_id
              OR (rsi.project_id IS NULL AND rsi.organization_id = a.organization_id)
              OR (rsi.project_id IS NULL AND rsi.organization_id IS NULL))
        WHERE a.mcp_server_id = s.id
          AND a.project_id = s.project_id
          AND a.organization_id = @organization_id::text
          AND a.remote_session_client_id = ac.remote_session_client_id
          AND a.deleted IS FALSE
      )
  )
RETURNING s.id;

-- name: SeedOrganizationFixture :exec
INSERT INTO organization_metadata (id, name, slug) VALUES (@id, @id, @id);

-- name: SeedProjectFixture :exec
INSERT INTO projects (id, name, slug, organization_id) VALUES (@id, @slug, @slug, @organization_id);

-- name: SeedRemoteSessionIssuerFixture :exec
INSERT INTO remote_session_issuers (id, project_id, slug, issuer, name) VALUES (@id, @project_id, @slug, @issuer, sqlc.narg('name'));

-- name: SeedRemoteSessionClientFixture :exec
INSERT INTO remote_session_clients (id, project_id, organization_id, remote_session_issuer_id, client_id, credential_owner, token_endpoint_auth_method)
VALUES (@id, @project_id, @organization_id, @remote_session_issuer_id, @client_id, @credential_owner, sqlc.narg('token_endpoint_auth_method'));

-- name: SeedRemoteMCPServerFixture :exec
INSERT INTO remote_mcp_servers (id, project_id, transport_type, url) VALUES (@id, @project_id, 'streamable-http', @url);

-- name: SeedMCPServerFixture :exec
INSERT INTO mcp_servers (id, project_id, slug, remote_mcp_server_id, visibility) VALUES (@id, @project_id, @slug, @remote_mcp_server_id, 'private');

-- name: SeedAssociationFixture :one
INSERT INTO mcp_server_remote_session_clients (project_id, organization_id, mcp_server_id, remote_session_client_id, client_attachment_scope)
VALUES (
    @project_id, @organization_id, @mcp_server_id, @remote_session_client_id,
    (SELECT c.attachment_scope FROM remote_session_clients AS c WHERE c.id = @remote_session_client_id)
)
RETURNING id;

-- name: HardDeleteAssociationFixture :exec
DELETE FROM mcp_server_remote_session_clients WHERE id = @id;

-- name: SoftDeleteRemoteSessionClientFixture :exec
UPDATE remote_session_clients SET deleted_at = clock_timestamp() WHERE id = @id;

-- name: ListLiveAuthClientIDsFixture :many
SELECT id FROM mcp_auth_clients WHERE organization_id = @organization_id AND deleted IS FALSE ORDER BY id;

-- name: ListLiveLinkIDsFixture :many
SELECT id FROM mcp_server_auth_clients WHERE organization_id = @organization_id AND deleted IS FALSE ORDER BY id;

-- name: SoftDeleteAssociationFixture :exec
UPDATE mcp_server_remote_session_clients SET deleted_at = clock_timestamp() WHERE id = @id;

-- name: ListAuthClientsFixture :many
SELECT id, client_type, project_id, name, remote_session_client_id::uuid AS remote_session_client_id
FROM mcp_auth_clients
WHERE organization_id = @organization_id
ORDER BY id;

-- name: ListServerAuthClientsFixture :many
SELECT auth_client_id
FROM mcp_server_auth_clients
WHERE mcp_server_id = @mcp_server_id AND deleted IS FALSE
ORDER BY auth_client_id;

-- name: SoftDeleteServerAuthClientLinksFixture :exec
UPDATE mcp_server_auth_clients SET deleted_at = clock_timestamp() WHERE mcp_server_id = @mcp_server_id;

-- name: SeedAuthClientFixture :one
INSERT INTO mcp_auth_clients (organization_id, project_id, client_type, source, name, remote_session_client_id, client_attachment_scope)
VALUES (
    @organization_id, @project_id, @client_type, @source, 'seeded', @remote_session_client_id,
    (SELECT c.attachment_scope FROM remote_session_clients AS c WHERE c.id = @remote_session_client_id)
)
RETURNING id;

-- name: SetRequiresUpstreamAuthFixture :exec
UPDATE mcp_servers SET requires_upstream_auth = sqlc.narg('requires_upstream_auth') WHERE id = @id;

-- name: GetRequiresUpstreamAuthFixture :one
SELECT requires_upstream_auth FROM mcp_servers WHERE id = @id;

-- name: SoftDeleteIssuerOfClientFixture :exec
UPDATE remote_session_issuers SET deleted_at = clock_timestamp()
WHERE id = (SELECT remote_session_issuer_id FROM remote_session_clients WHERE remote_session_clients.id = @client_id);
