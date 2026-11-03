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
-- the server already offers that auth client; link_detached that an
-- administrator detached the client's backfilled auth client from the server,
-- so the backfill must not link it again.
--
-- only_mcp_server_ids narrows the rows to those servers' associations, for an
-- association write that records its own servers' clients; NULL reads every
-- server in the organization.
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
       ))::boolean AS link_live,
       EXISTS (
         SELECT 1
         FROM mcp_server_auth_clients AS detached
         JOIN mcp_auth_clients AS dac
           ON dac.id = detached.auth_client_id
          AND dac.organization_id = detached.organization_id
          AND dac.remote_session_client_id = c.id
          AND dac.client_type IN ('remote_session', 'service_account')
          AND dac.source = 'backfill'
         WHERE detached.mcp_server_id = a.mcp_server_id
           AND detached.organization_id = a.organization_id
           AND detached.source = 'admin'
           AND detached.deleted IS TRUE
       )::boolean AS link_detached
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
  AND (sqlc.narg('only_mcp_server_ids')::uuid[] IS NULL
       OR a.mcp_server_id = ANY(sqlc.narg('only_mcp_server_ids')::uuid[]))
ORDER BY a.id
LIMIT @page_size::int;

-- name: CreateAuthClient :one
-- Records the auth client for an upstream OAuth client in one organization,
-- typed by its credential owner, conditional on the client and its issuer
-- still being live and in that organization's reach, the client owned as
-- decided and associated with a live server of a live project there. A
-- self-owned client is never platform-global. A concurrent run recording the
-- same client either fails the type's unique key or, under the same id, finds
-- it live and returns no row. The id is stable for the
-- organization, registration and type, so a client retired when its last
-- association went away is revived under the same id when one returns; only
-- its deletion is undone.
INSERT INTO mcp_auth_clients (id, organization_id, project_id, client_type, source, name, remote_session_client_id, client_attachment_scope)
SELECT @id::uuid, @organization_id::text, sqlc.narg('project_id')::uuid, @client_type::text, 'backfill', @name::text, c.id, c.attachment_scope
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
ON CONFLICT (id) DO UPDATE
SET deleted_at = NULL, updated_at = clock_timestamp()
WHERE mcp_auth_clients.deleted
  AND mcp_auth_clients.source = 'backfill'
  AND mcp_auth_clients.organization_id = EXCLUDED.organization_id
  AND mcp_auth_clients.client_type = EXCLUDED.client_type
  AND mcp_auth_clients.remote_session_client_id = EXCLUDED.remote_session_client_id
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
  AND NOT EXISTS (
    -- An administrator detached this client from this server: the backfill
    -- never links it again.
    SELECT 1
    FROM mcp_server_auth_clients AS detached
    WHERE detached.mcp_server_id = a.mcp_server_id
      AND detached.auth_client_id = ac.id
      AND detached.source = 'admin'
      AND detached.deleted IS TRUE
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
-- rechecking the same evidence, with their header copies.
WITH retired AS (
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
RETURNING target.id
), headers AS (
  UPDATE mcp_auth_client_headers AS h
  SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
  FROM retired
  WHERE h.auth_client_id = retired.id
    AND h.organization_id = @organization_id::text
    AND h.deleted IS FALSE
  RETURNING h.id
)
SELECT id FROM retired;

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

-- name: LockOrganizationBackfill :exec
-- Serializes runs for one organization, dry runs included, for the
-- transaction's life.
SELECT pg_advisory_xact_lock(hashtext('authclientbackfill:' || @organization_id::text));

-- name: TryLockOrganizationBackfill :one
-- Takes the organization's backfill lock for the session's life without
-- waiting, so one run holds it across all its phases; false means another run
-- holds it. started_at is the database's clock as the run begins, against
-- which retirement checks for configuration changed since.
SELECT pg_try_advisory_lock(hashtext('authclientbackfill:' || @organization_id::text))::boolean AS locked,
       clock_timestamp()::timestamptz AS started_at;

-- name: UnlockOrganizationBackfill :exec
-- Releases the organization's session backfill lock.
SELECT pg_advisory_unlock(hashtext('authclientbackfill:' || @organization_id::text));

-- name: ListCredentialSources :many
-- Live remote and tunneled MCP sources in live projects of the organization
-- that carry credential headers, after the cursor. A credential header is an
-- Authorization header, a static header marked secret, or a header forwarded
-- from the request; other static headers stay server headers. Read-only and
-- value-free.
WITH creds AS (
  SELECT 'remote'::text AS source_kind, r.id AS source_id, r.project_id, rh.id AS header_id, rh.name,
         (rh.value IS NOT NULL) AS is_static,
         rh.value_from_request_header AS forwarded_from
  FROM remote_mcp_server_headers AS rh
  JOIN remote_mcp_servers AS r ON r.id = rh.remote_mcp_server_id AND r.deleted IS FALSE
  JOIN projects AS p ON p.id = r.project_id AND p.organization_id = @organization_id::text AND p.deleted IS FALSE
  WHERE rh.deleted IS FALSE
    AND (lower(rh.name) = 'authorization' OR rh.value_from_request_header IS NOT NULL OR rh.is_secret)
  UNION ALL
  SELECT 'tunneled'::text, t.id, t.project_id, th.id, th.name, (th.value IS NOT NULL),
         th.value_from_request_header
  FROM tunneled_mcp_server_headers AS th
  JOIN tunneled_mcp_servers AS t ON t.id = th.tunneled_mcp_server_id AND t.deleted IS FALSE
  JOIN projects AS p ON p.id = t.project_id AND p.organization_id = @organization_id::text AND p.deleted IS FALSE
  WHERE th.deleted IS FALSE
    AND (lower(th.name) = 'authorization' OR th.value_from_request_header IS NOT NULL OR th.is_secret)
)
SELECT c.source_kind,
       c.source_id,
       c.project_id,
       COUNT(*) FILTER (WHERE lower(c.name) = 'authorization')::bigint AS authorization_headers,
       (array_agg(c.header_id ORDER BY c.header_id) FILTER (WHERE lower(c.name) = 'authorization'))[1]::uuid AS authorization_header_id,
       COALESCE(bool_and(c.is_static) FILTER (WHERE lower(c.name) = 'authorization'), FALSE)::boolean AS authorization_is_static,
       bool_and(c.is_static)::boolean AS all_static,
       bool_and(NOT c.is_static)::boolean AS all_forwarded,
       (COUNT(DISTINCT lower(c.name)) < COUNT(*))::boolean AS has_duplicate_names,
       -- The proxy decides which of these it refuses (proxy.CheckRemoteHeader).
       COALESCE(array_agg(DISTINCT c.forwarded_from) FILTER (WHERE c.forwarded_from IS NOT NULL), ARRAY[]::text[])::text[] AS forwarded_sources,
       array_agg(c.header_id ORDER BY c.header_id)::uuid[] AS header_ids,
       COALESCE(array_agg(c.header_id ORDER BY c.header_id) FILTER (WHERE lower(c.name) <> 'authorization'), ARRAY[]::uuid[])::uuid[] AS other_header_ids
FROM creds AS c
WHERE c.source_id > @cursor::uuid
  -- Set to decide one source's headers as they are written.
  AND (sqlc.narg('only_source_id')::uuid IS NULL OR c.source_id = sqlc.narg('only_source_id')::uuid)
GROUP BY c.source_kind, c.source_id, c.project_id
ORDER BY c.source_id
LIMIT @page_size::int;

-- name: ListRemoteSourceServers :many
-- The live servers of a live project in the organization backed by one remote
-- source, and whether each has a live upstream OAuth client association with
-- a client and issuer in reach, as the OAuth phase decides them.
SELECT s.id AS mcp_server_id,
       s.project_id,
       EXISTS (
         SELECT 1 FROM mcp_server_remote_session_clients AS a
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
         WHERE a.mcp_server_id = s.id AND a.organization_id = p.organization_id AND a.deleted IS FALSE
       )::boolean AS has_oauth
FROM mcp_servers AS s
JOIN projects AS p ON p.id = s.project_id AND p.organization_id = @organization_id::text AND p.deleted IS FALSE
WHERE s.remote_mcp_server_id = @source_id::uuid
  AND s.deleted IS FALSE
ORDER BY s.id;

-- name: ListTunneledSourceServers :many
-- The live servers of a live project in the organization backed by one
-- tunneled source, and whether each has a live upstream OAuth client
-- association with a client and issuer in reach.
SELECT s.id AS mcp_server_id,
       s.project_id,
       EXISTS (
         SELECT 1 FROM mcp_server_remote_session_clients AS a
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
         WHERE a.mcp_server_id = s.id AND a.organization_id = p.organization_id AND a.deleted IS FALSE
       )::boolean AS has_oauth
FROM mcp_servers AS s
JOIN projects AS p ON p.id = s.project_id AND p.organization_id = @organization_id::text AND p.deleted IS FALSE
WHERE s.tunneled_mcp_server_id = @source_id::uuid
  AND s.deleted IS FALSE
ORDER BY s.id;

-- name: GetAuthClientState :one
-- A backfilled auth client of the organization by id, deleted or not.
SELECT ac.id, ac.client_type, ac.deleted
FROM mcp_auth_clients AS ac
WHERE ac.id = @id::uuid
  AND ac.organization_id = @organization_id::text
  AND ac.source = 'backfill';

-- name: CreateSourceAuthClient :execrows
-- Records the static_credential or pass_through auth client for one source's
-- credential headers under a stable id, conditional on the source and its
-- project still being live in the organization and the source still carrying
-- a credential header of the type: the anchoring Authorization header when one
-- is named, otherwise credential headers all of that type. A client retired
-- earlier is revived. Zero rows
-- means the evidence changed or the client is already live.
INSERT INTO mcp_auth_clients (id, organization_id, project_id, client_type, source, name)
SELECT @id::uuid, @organization_id::text, src.project_id, @client_type::text, 'backfill', @name::text
FROM (
  SELECT r.project_id
  FROM remote_mcp_servers AS r
  WHERE @source_kind::text = 'remote' AND r.id = @source_id::uuid AND r.deleted IS FALSE
    AND EXISTS (
      SELECT 1 FROM remote_mcp_server_headers AS h
      WHERE h.remote_mcp_server_id = r.id AND h.deleted IS FALSE
        AND (sqlc.narg('anchor_header_id')::uuid IS NULL OR (h.id = sqlc.narg('anchor_header_id')::uuid AND lower(h.name) = 'authorization'))
        AND (lower(h.name) = 'authorization' OR h.value_from_request_header IS NOT NULL OR h.is_secret)
        AND (h.value IS NOT NULL) = (@client_type::text = 'static_credential')
    )
    AND (sqlc.narg('anchor_header_id')::uuid IS NOT NULL OR NOT EXISTS (
      SELECT 1 FROM remote_mcp_server_headers AS h
      WHERE h.remote_mcp_server_id = r.id AND h.deleted IS FALSE
        AND (lower(h.name) = 'authorization' OR h.value_from_request_header IS NOT NULL OR h.is_secret)
        AND (h.value IS NOT NULL) <> (@client_type::text = 'static_credential')
    ))
  UNION ALL
  SELECT t.project_id
  FROM tunneled_mcp_servers AS t
  WHERE @source_kind::text = 'tunneled' AND t.id = @source_id::uuid AND t.deleted IS FALSE
    AND EXISTS (
      SELECT 1 FROM tunneled_mcp_server_headers AS h
      WHERE h.tunneled_mcp_server_id = t.id AND h.deleted IS FALSE
        AND (sqlc.narg('anchor_header_id')::uuid IS NULL OR (h.id = sqlc.narg('anchor_header_id')::uuid AND lower(h.name) = 'authorization'))
        AND (lower(h.name) = 'authorization' OR h.value_from_request_header IS NOT NULL OR h.is_secret)
        AND (h.value IS NOT NULL) = (@client_type::text = 'static_credential')
    )
    AND (sqlc.narg('anchor_header_id')::uuid IS NOT NULL OR NOT EXISTS (
      SELECT 1 FROM tunneled_mcp_server_headers AS h
      WHERE h.tunneled_mcp_server_id = t.id AND h.deleted IS FALSE
        AND (lower(h.name) = 'authorization' OR h.value_from_request_header IS NOT NULL OR h.is_secret)
        AND (h.value IS NOT NULL) <> (@client_type::text = 'static_credential')
    ))
) AS src
JOIN projects AS p
  ON p.id = src.project_id
 AND p.organization_id = @organization_id::text
 AND p.deleted IS FALSE
WHERE @client_type::text IN ('static_credential', 'pass_through')
ON CONFLICT (id) DO UPDATE
SET deleted_at = NULL, updated_at = clock_timestamp()
WHERE mcp_auth_clients.deleted
  AND mcp_auth_clients.source = 'backfill'
  AND mcp_auth_clients.organization_id = EXCLUDED.organization_id
  AND mcp_auth_clients.project_id IS NOT DISTINCT FROM EXCLUDED.project_id
  AND mcp_auth_clients.client_type = EXCLUDED.client_type;

-- name: PlanClientHeaders :many
-- Compares an auth client's live header copies with the source headers it
-- should carry, by name in any letter case. Each desired header returns the
-- copy it matches, if any, and whether any field differs: values compare as
-- stored, so ciphertext is never decrypted. Copies matching no desired header,
-- and case-variant extras, return with no source to be retired.
WITH desired AS (
  SELECT h.id AS src_id, 'remote'::text AS src_kind, h.name, lower(h.name) AS lname,
         h.description, h.is_required, h.is_secret, h.value, h.value_from_request_header
  FROM remote_mcp_server_headers AS h
  JOIN remote_mcp_servers AS r ON r.id = h.remote_mcp_server_id AND r.deleted IS FALSE
  JOIN projects AS p ON p.id = r.project_id AND p.organization_id = @organization_id::text AND p.deleted IS FALSE
  WHERE h.id = ANY(@remote_header_ids::uuid[]) AND h.deleted IS FALSE
  UNION ALL
  SELECT h.id, 'tunneled'::text, h.name, lower(h.name),
         h.description, h.is_required, h.is_secret, h.value, h.value_from_request_header
  FROM tunneled_mcp_server_headers AS h
  JOIN tunneled_mcp_servers AS t ON t.id = h.tunneled_mcp_server_id AND t.deleted IS FALSE
  JOIN projects AS p ON p.id = t.project_id AND p.organization_id = @organization_id::text AND p.deleted IS FALSE
  WHERE h.id = ANY(@tunneled_header_ids::uuid[]) AND h.deleted IS FALSE
), copies AS (
  SELECT c.id, c.name, lower(c.name) AS lname, c.description, c.is_required, c.is_secret, c.value, c.value_from_request_header,
         row_number() OVER (PARTITION BY lower(c.name) ORDER BY (c.name = ANY(SELECT d.name FROM desired AS d)) DESC, c.id) AS rank
  FROM mcp_auth_client_headers AS c
  WHERE c.auth_client_id = @auth_client_id::uuid
    AND c.organization_id = @organization_id::text
    AND c.deleted IS FALSE
)
SELECT COALESCE(d.src_id, '00000000-0000-0000-0000-000000000000'::uuid)::uuid AS src_id,
       COALESCE(d.src_kind, '')::text AS src_kind,
       COALESCE(c.id, '00000000-0000-0000-0000-000000000000'::uuid)::uuid AS copy_id,
       (c.id IS NOT NULL AND (
          c.name IS DISTINCT FROM d.name
          OR c.value IS DISTINCT FROM d.value
          OR c.value_from_request_header IS DISTINCT FROM d.value_from_request_header
          OR c.is_secret IS DISTINCT FROM d.is_secret
          OR c.is_required IS DISTINCT FROM d.is_required
          OR c.description IS DISTINCT FROM d.description
       ))::boolean AS differs
FROM desired AS d
LEFT JOIN copies AS c ON c.lname = d.lname AND c.rank = 1
UNION ALL
SELECT '00000000-0000-0000-0000-000000000000'::uuid, ''::text, c.id, FALSE
FROM copies AS c
WHERE c.rank > 1 OR NOT EXISTS (SELECT 1 FROM desired AS d WHERE d.lname = c.lname);

-- name: InsertHeaderCopy :execrows
-- Copies one source credential header onto a live backfilled auth client of
-- the organization as stored, ciphertext included, conditional on the header
-- and its source being live in a live project of the organization, in the
-- auth client's project when the client has one, and, for an upstream OAuth
-- client's auth client or a required auth client, that client still being
-- used on the header's source and only there. Zero rows means the evidence changed or a copy with
-- that name already exists.
INSERT INTO mcp_auth_client_headers (auth_client_id, organization_id, name, description, is_required, is_secret, value, value_from_request_header)
SELECT ac.id, ac.organization_id, src.name, src.description, src.is_required, src.is_secret, src.value, src.value_from_request_header
FROM (
  SELECT h.name, h.description, h.is_required, h.is_secret, h.value, h.value_from_request_header, r.project_id,
         'remote'::text AS source_kind, r.id AS source_id
  FROM remote_mcp_server_headers AS h
  JOIN remote_mcp_servers AS r ON r.id = h.remote_mcp_server_id AND r.deleted IS FALSE
  WHERE @src_kind::text = 'remote' AND h.id = @src_id::uuid AND h.deleted IS FALSE
  UNION ALL
  SELECT h.name, h.description, h.is_required, h.is_secret, h.value, h.value_from_request_header, t.project_id,
         'tunneled'::text, t.id
  FROM tunneled_mcp_server_headers AS h
  JOIN tunneled_mcp_servers AS t ON t.id = h.tunneled_mcp_server_id AND t.deleted IS FALSE
  WHERE @src_kind::text = 'tunneled' AND h.id = @src_id::uuid AND h.deleted IS FALSE
) AS src
JOIN projects AS p ON p.id = src.project_id AND p.organization_id = @organization_id::text AND p.deleted IS FALSE
JOIN mcp_auth_clients AS ac
  ON ac.id = @auth_client_id::uuid
 AND ac.organization_id = p.organization_id
 AND ac.source = 'backfill'
 AND ac.deleted IS FALSE
 AND (ac.project_id IS NULL OR ac.project_id = src.project_id)
WHERE TRUE
  AND (sqlc.narg('required_remote_session_client_id')::uuid IS NULL OR NOT EXISTS (
    SELECT 1
    FROM mcp_server_remote_session_clients AS a
    JOIN mcp_servers AS s2 ON s2.id = a.mcp_server_id AND s2.project_id = a.project_id AND s2.deleted IS FALSE
    WHERE a.organization_id = @organization_id::text
      AND a.remote_session_client_id = sqlc.narg('required_remote_session_client_id')::uuid
      AND a.deleted IS FALSE
      AND NOT COALESCE(
        (src.source_kind = 'remote' AND s2.remote_mcp_server_id = src.source_id)
        OR (src.source_kind = 'tunneled' AND s2.tunneled_mcp_server_id = src.source_id),
        FALSE
      )
  ))
  AND (sqlc.narg('required_auth_client_id')::uuid IS NULL OR (
    EXISTS (
      SELECT 1
      FROM mcp_server_auth_clients AS l
      JOIN mcp_servers AS s3 ON s3.id = l.mcp_server_id AND s3.project_id = l.project_id AND s3.deleted IS FALSE
      WHERE l.auth_client_id = sqlc.narg('required_auth_client_id')::uuid
        AND l.organization_id = @organization_id::text
        AND l.deleted IS FALSE
        AND s3.project_id = src.project_id
        AND COALESCE(
          (src.source_kind = 'remote' AND s3.remote_mcp_server_id = src.source_id)
          OR (src.source_kind = 'tunneled' AND s3.tunneled_mcp_server_id = src.source_id),
          FALSE
        )
    )
    AND NOT EXISTS (
      SELECT 1
      FROM mcp_server_auth_clients AS l
      JOIN mcp_servers AS s3 ON s3.id = l.mcp_server_id AND s3.project_id = l.project_id AND s3.deleted IS FALSE
      WHERE l.auth_client_id = sqlc.narg('required_auth_client_id')::uuid
        AND l.organization_id = @organization_id::text
        AND l.deleted IS FALSE
        AND NOT COALESCE(
          s3.project_id = src.project_id
          AND ((src.source_kind = 'remote' AND s3.remote_mcp_server_id = src.source_id)
               OR (src.source_kind = 'tunneled' AND s3.tunneled_mcp_server_id = src.source_id)),
          FALSE
        )
    )
  ))
ON CONFLICT (auth_client_id, name) WHERE deleted IS FALSE DO NOTHING;

-- name: UpdateHeaderCopy :execrows
-- Brings one live header copy in line with its source header, conditional on
-- the source header still being live and named the same in any letter case,
-- and in the auth client's project when the client has one; with a required
-- upstream OAuth client or auth client, that client still being used on the
-- header's source and only there.
UPDATE mcp_auth_client_headers AS c
SET name = src.name,
    description = src.description,
    is_required = src.is_required,
    is_secret = src.is_secret,
    value = src.value,
    value_from_request_header = src.value_from_request_header,
    updated_at = clock_timestamp()
FROM (
  SELECT h.name, h.description, h.is_required, h.is_secret, h.value, h.value_from_request_header,
         'remote'::text AS source_kind, r.id AS source_id, r.project_id
  FROM remote_mcp_server_headers AS h
  JOIN remote_mcp_servers AS r ON r.id = h.remote_mcp_server_id AND r.deleted IS FALSE
  JOIN projects AS sp ON sp.id = r.project_id AND sp.organization_id = @organization_id::text AND sp.deleted IS FALSE
  WHERE @src_kind::text = 'remote' AND h.id = @src_id::uuid AND h.deleted IS FALSE
  UNION ALL
  SELECT h.name, h.description, h.is_required, h.is_secret, h.value, h.value_from_request_header,
         'tunneled'::text, t.id, t.project_id
  FROM tunneled_mcp_server_headers AS h
  JOIN tunneled_mcp_servers AS t ON t.id = h.tunneled_mcp_server_id AND t.deleted IS FALSE
  JOIN projects AS sp ON sp.id = t.project_id AND sp.organization_id = @organization_id::text AND sp.deleted IS FALSE
  WHERE @src_kind::text = 'tunneled' AND h.id = @src_id::uuid AND h.deleted IS FALSE
) AS src
WHERE c.id = @copy_id::uuid
  AND c.organization_id = @organization_id::text
  AND c.deleted IS FALSE
  AND lower(c.name) = lower(src.name)
  AND EXISTS (
    SELECT 1 FROM mcp_auth_clients AS ac
    WHERE ac.id = c.auth_client_id
      AND ac.organization_id = c.organization_id
      AND (ac.project_id IS NULL OR ac.project_id = src.project_id)
  )
  AND (sqlc.narg('required_remote_session_client_id')::uuid IS NULL OR NOT EXISTS (
    SELECT 1
    FROM mcp_server_remote_session_clients AS a
    JOIN mcp_servers AS s2 ON s2.id = a.mcp_server_id AND s2.project_id = a.project_id AND s2.deleted IS FALSE
    WHERE a.organization_id = @organization_id::text
      AND a.remote_session_client_id = sqlc.narg('required_remote_session_client_id')::uuid
      AND a.deleted IS FALSE
      AND NOT COALESCE(
        (src.source_kind = 'remote' AND s2.remote_mcp_server_id = src.source_id)
        OR (src.source_kind = 'tunneled' AND s2.tunneled_mcp_server_id = src.source_id),
        FALSE
      )
  ))
  AND (sqlc.narg('required_auth_client_id')::uuid IS NULL OR (
    EXISTS (
      SELECT 1
      FROM mcp_server_auth_clients AS l
      JOIN mcp_servers AS s3 ON s3.id = l.mcp_server_id AND s3.project_id = l.project_id AND s3.deleted IS FALSE
      WHERE l.auth_client_id = sqlc.narg('required_auth_client_id')::uuid
        AND l.organization_id = @organization_id::text
        AND l.deleted IS FALSE
        AND s3.project_id = src.project_id
        AND COALESCE(
          (src.source_kind = 'remote' AND s3.remote_mcp_server_id = src.source_id)
          OR (src.source_kind = 'tunneled' AND s3.tunneled_mcp_server_id = src.source_id),
          FALSE
        )
    )
    AND NOT EXISTS (
      SELECT 1
      FROM mcp_server_auth_clients AS l
      JOIN mcp_servers AS s3 ON s3.id = l.mcp_server_id AND s3.project_id = l.project_id AND s3.deleted IS FALSE
      WHERE l.auth_client_id = sqlc.narg('required_auth_client_id')::uuid
        AND l.organization_id = @organization_id::text
        AND l.deleted IS FALSE
        AND NOT COALESCE(
          s3.project_id = src.project_id
          AND ((src.source_kind = 'remote' AND s3.remote_mcp_server_id = src.source_id)
               OR (src.source_kind = 'tunneled' AND s3.tunneled_mcp_server_id = src.source_id)),
          FALSE
        )
    )
  ));

-- name: RetireHeaderCopy :execrows
-- Soft-deletes one live header copy of the organization.
UPDATE mcp_auth_client_headers
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE id = @copy_id::uuid
  AND organization_id = @organization_id::text
  AND deleted IS FALSE;

-- name: LinkSourceServer :execrows
-- Records that a live server offers a live backfilled auth client,
-- conditional on the server still being backed by the source the decision
-- read and, when required_has_oauth is set, its upstream identity state being
-- the one the decision used: an upstream OAuth association or a live
-- backfilled EMA client.
INSERT INTO mcp_server_auth_clients (project_id, organization_id, mcp_server_id, auth_client_id, client_attachment_scope, source)
SELECT s.project_id, p.organization_id, s.id, ac.id, ac.attachment_scope, 'backfill'
FROM mcp_servers AS s
JOIN projects AS p ON p.id = s.project_id AND p.organization_id = @organization_id::text AND p.deleted IS FALSE
JOIN mcp_auth_clients AS ac
  ON ac.id = @auth_client_id::uuid
 AND ac.organization_id = p.organization_id
 AND ac.source = 'backfill'
 AND ac.deleted IS FALSE
 AND (ac.project_id IS NULL OR ac.project_id = s.project_id)
WHERE s.id = @mcp_server_id::uuid
  AND s.deleted IS FALSE
  AND CASE @source_kind::text
        WHEN 'remote' THEN s.remote_mcp_server_id = @source_id::uuid
        WHEN 'tunneled' THEN s.tunneled_mcp_server_id = @source_id::uuid
        ELSE FALSE
      END
  AND (sqlc.narg('required_has_oauth')::boolean IS NULL
       OR sqlc.narg('required_has_oauth')::boolean = (EXISTS (
         SELECT 1 FROM mcp_server_remote_session_clients AS a
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
         WHERE a.mcp_server_id = s.id AND a.organization_id = p.organization_id AND a.deleted IS FALSE
       ) OR EXISTS (
         SELECT 1 FROM mcp_server_auth_clients AS el
         JOIN mcp_auth_clients AS ec
           ON ec.id = el.auth_client_id
          AND ec.organization_id = el.organization_id
          AND ec.client_type = 'ema'
          AND ec.source = 'backfill'
          AND ec.deleted IS FALSE
         WHERE el.mcp_server_id = s.id AND el.organization_id = p.organization_id AND el.deleted IS FALSE
           AND NOT EXISTS (
             -- Reached only through the gateways: the server's own issuer has no
             -- ready binding for the client's upstream, so the client cannot serve
             -- its direct route and leaves that route's configuration alone.
             SELECT 1
             FROM mcp_auth_client_emas AS gx
             JOIN mcp_servers AS gs ON gs.id = el.mcp_server_id
             WHERE gx.auth_client_id = ec.id
               AND gx.deleted IS FALSE
               AND NOT EXISTS (
                 SELECT 1 FROM remote_session_ema_bindings AS db
                 WHERE db.organization_id = gx.organization_id
                   AND db.project_id = gs.project_id
                   AND db.remote_session_issuer_id = gx.remote_session_issuer_id
                   AND db.remote_session_client_id = gx.remote_session_client_id
                   AND db.resource = gx.resource
                   AND db.state = 'ready'
                   AND db.user_session_issuer_id = gs.user_session_issuer_id
               )
               AND EXISTS (
                 SELECT 1 FROM remote_session_ema_bindings AS gb
                 WHERE gb.organization_id = gx.organization_id
                   AND gb.project_id = gs.project_id
                   AND gb.remote_session_issuer_id = gx.remote_session_issuer_id
                   AND gb.remote_session_client_id = gx.remote_session_client_id
                   AND gb.resource = gx.resource
                   AND gb.state = 'ready'
                   AND gb.user_session_issuer_id = (
                     -- The gateways' issuer, as ListEMABindingServers reads it.
                     SELECT (array_agg(gi.id))[1]
                     FROM user_session_issuers AS gi
                     WHERE gi.organization_id = gb.organization_id
                       AND gi.project_id IS NULL
                       AND gi.deleted IS FALSE
                       AND EXISTS (
                         SELECT 1 FROM organization_features AS gf
                         WHERE gf.organization_id = gb.organization_id
                           AND gf.feature_name = 'organization_gateway_disabled'
                       )
                     HAVING count(*) = 1
                   )
               )
           )
       )))
  AND NOT EXISTS (
    -- An administrator detached this client from this server: the backfill
    -- never links it again.
    SELECT 1
    FROM mcp_server_auth_clients AS detached
    WHERE detached.mcp_server_id = s.id
      AND detached.auth_client_id = ac.id
      AND detached.source = 'admin'
      AND detached.deleted IS TRUE
  )
ON CONFLICT (mcp_server_id, auth_client_id) WHERE deleted IS FALSE DO NOTHING;

-- name: ListEMAClientLiveServers :many
-- The live servers a backfilled ema client is linked to.
SELECT s.id
FROM mcp_server_auth_clients AS l
JOIN mcp_auth_clients AS ac
  ON ac.id = l.auth_client_id
 AND ac.organization_id = l.organization_id
 AND ac.client_type = 'ema'
 AND ac.source = 'backfill'
JOIN mcp_servers AS s ON s.id = l.mcp_server_id AND s.project_id = l.project_id AND s.deleted IS FALSE
JOIN projects AS p ON p.id = s.project_id AND p.organization_id = l.organization_id AND p.deleted IS FALSE
WHERE l.auth_client_id = @auth_client_id::uuid
  AND l.organization_id = @organization_id::text
  AND l.deleted IS FALSE
ORDER BY s.id;

-- name: ListServerCredentialSources :many
-- The sources of the given live servers in the organization, each with those
-- servers, its non-Authorization credential headers, and whether two of them
-- differ only in letter case. A server with no remote or tunneled source is a
-- source of its own with no headers.
WITH used AS (
  SELECT CASE WHEN s.remote_mcp_server_id IS NOT NULL THEN 'remote'
              WHEN s.tunneled_mcp_server_id IS NOT NULL THEN 'tunneled'
              ELSE 'none' END AS source_kind,
         COALESCE(s.remote_mcp_server_id, s.tunneled_mcp_server_id, s.id) AS source_id,
         s.id AS server_id
  FROM mcp_servers AS s
  JOIN projects AS p ON p.id = s.project_id AND p.organization_id = @organization_id::text AND p.deleted IS FALSE
  WHERE s.id = ANY(@mcp_server_ids::uuid[])
    AND s.deleted IS FALSE
), grouped AS (
  SELECT source_kind, source_id, array_agg(DISTINCT server_id ORDER BY server_id) AS server_ids
  FROM used
  GROUP BY source_kind, source_id
)
SELECT g.source_kind::text AS source_kind,
       g.source_id::uuid AS source_id,
       g.server_ids::uuid[] AS server_ids,
       COALESCE(h.ids, ARRAY[]::uuid[])::uuid[] AS header_ids,
       COALESCE(h.duplicate, FALSE)::boolean AS has_duplicate_names,
       COALESCE(h.forwarded, ARRAY[]::text[])::text[] AS forwarded_sources
FROM grouped AS g
LEFT JOIN LATERAL (
  SELECT array_agg(x.id ORDER BY x.id) AS ids,
         COUNT(DISTINCT lower(x.name)) < COUNT(*) AS duplicate,
         array_agg(DISTINCT x.forwarded) FILTER (WHERE x.forwarded IS NOT NULL) AS forwarded
  FROM (
    SELECT h.id, h.name, h.value_from_request_header AS forwarded FROM remote_mcp_server_headers AS h
    JOIN remote_mcp_servers AS r ON r.id = h.remote_mcp_server_id AND r.deleted IS FALSE
    JOIN projects AS sp ON sp.id = r.project_id AND sp.organization_id = @organization_id::text AND sp.deleted IS FALSE
    WHERE g.source_kind = 'remote' AND h.remote_mcp_server_id = g.source_id AND h.deleted IS FALSE
      AND lower(h.name) <> 'authorization' AND (h.value_from_request_header IS NOT NULL OR h.is_secret)
    UNION ALL
    SELECT h.id, h.name, h.value_from_request_header FROM tunneled_mcp_server_headers AS h
    JOIN tunneled_mcp_servers AS t ON t.id = h.tunneled_mcp_server_id AND t.deleted IS FALSE
    JOIN projects AS sp ON sp.id = t.project_id AND sp.organization_id = @organization_id::text AND sp.deleted IS FALSE
    WHERE g.source_kind = 'tunneled' AND h.tunneled_mcp_server_id = g.source_id AND h.deleted IS FALSE
      AND lower(h.name) <> 'authorization' AND (h.value_from_request_header IS NOT NULL OR h.is_secret)
  ) AS x
) AS h ON TRUE
ORDER BY g.source_id;

-- name: ListOAuthClientSources :many
-- Each upstream OAuth client the organization's live associations name, per
-- source of its associated live servers: those servers, the source's
-- non-Authorization credential headers, and whether two of them differ only
-- in letter case. A server with no remote or tunneled source (hosted,
-- unproxied) is a source of its own with no headers, so a client it shares
-- with another source's servers is never copied onto.
WITH used AS (
  SELECT a.remote_session_client_id,
         c.credential_owner,
         CASE WHEN s.remote_mcp_server_id IS NOT NULL THEN 'remote'
              WHEN s.tunneled_mcp_server_id IS NOT NULL THEN 'tunneled'
              ELSE 'none' END AS source_kind,
         COALESCE(s.remote_mcp_server_id, s.tunneled_mcp_server_id, s.id) AS source_id,
         s.id AS server_id
  FROM mcp_server_remote_session_clients AS a
  JOIN projects AS p ON p.id = a.project_id AND p.organization_id = a.organization_id AND p.deleted IS FALSE
  JOIN mcp_servers AS s ON s.id = a.mcp_server_id AND s.project_id = a.project_id AND s.deleted IS FALSE
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
  WHERE a.organization_id = @organization_id::text
    AND a.deleted IS FALSE
    -- Set to re-decide named clients as a source or its servers are written.
    -- Every source of a named client is still returned, so the rule that
    -- refuses a client shared across sources sees the same evidence.
    AND (sqlc.narg('only_client_ids')::uuid[] IS NULL
         OR a.remote_session_client_id = ANY(sqlc.narg('only_client_ids')::uuid[]))
), grouped AS (
  SELECT remote_session_client_id, credential_owner, source_kind, source_id,
         array_agg(DISTINCT server_id ORDER BY server_id) AS server_ids
  FROM used
  GROUP BY remote_session_client_id, credential_owner, source_kind, source_id
)
SELECT g.remote_session_client_id::uuid AS remote_session_client_id,
       g.credential_owner::text AS credential_owner,
       g.source_kind::text AS source_kind,
       g.source_id::uuid AS source_id,
       g.server_ids::uuid[] AS server_ids,
       COALESCE(h.ids, ARRAY[]::uuid[])::uuid[] AS header_ids,
       COALESCE(h.duplicate, FALSE)::boolean AS has_duplicate_names,
       COALESCE(h.forwarded, ARRAY[]::text[])::text[] AS forwarded_sources
FROM grouped AS g
LEFT JOIN LATERAL (
  SELECT array_agg(x.id ORDER BY x.id) AS ids,
         COUNT(DISTINCT lower(x.name)) < COUNT(*) AS duplicate,
         array_agg(DISTINCT x.forwarded) FILTER (WHERE x.forwarded IS NOT NULL) AS forwarded
  FROM (
    SELECT h.id, h.name, h.value_from_request_header AS forwarded FROM remote_mcp_server_headers AS h
    JOIN remote_mcp_servers AS r ON r.id = h.remote_mcp_server_id AND r.deleted IS FALSE
    JOIN projects AS sp ON sp.id = r.project_id AND sp.organization_id = @organization_id::text AND sp.deleted IS FALSE
    WHERE g.source_kind = 'remote' AND h.remote_mcp_server_id = g.source_id AND h.deleted IS FALSE
      AND lower(h.name) <> 'authorization' AND (h.value_from_request_header IS NOT NULL OR h.is_secret)
    UNION ALL
    SELECT h.id, h.name, h.value_from_request_header FROM tunneled_mcp_server_headers AS h
    JOIN tunneled_mcp_servers AS t ON t.id = h.tunneled_mcp_server_id AND t.deleted IS FALSE
    JOIN projects AS sp ON sp.id = t.project_id AND sp.organization_id = @organization_id::text AND sp.deleted IS FALSE
    WHERE g.source_kind = 'tunneled' AND h.tunneled_mcp_server_id = g.source_id AND h.deleted IS FALSE
      AND lower(h.name) <> 'authorization' AND (h.value_from_request_header IS NOT NULL OR h.is_secret)
  ) AS x
) AS h ON TRUE
ORDER BY g.remote_session_client_id, g.source_id;

-- name: FindOAuthAuthClients :many
-- The organization's live backfilled OAuth-backed auth clients for one
-- upstream OAuth client.
SELECT ac.id
FROM mcp_auth_clients AS ac
WHERE ac.organization_id = @organization_id::text
  AND ac.remote_session_client_id = @remote_session_client_id::uuid
  AND ac.client_type IN ('remote_session', 'service_account')
  AND ac.source = 'backfill'
  AND ac.deleted IS FALSE
ORDER BY ac.id;

-- name: LockEMARegistrationForSync :one
-- Serialize EMA binding and credential-copy decisions on their registration.
-- The caller must read bindings and links again after taking this lock. Lock
-- strength allows concurrent binding inserts' foreign-key checks to finish.
-- Existing typed clients can lock registrations in deleted projects so stale
-- credential copies can be retired; live-owner queries decide serving reach.
SELECT c.id
FROM remote_session_clients AS c
WHERE (EXISTS (
  SELECT 1 FROM remote_session_ema_bindings AS b
  JOIN projects AS p ON p.id = b.project_id
    AND p.organization_id = b.organization_id AND p.deleted IS FALSE
  WHERE b.id = sqlc.narg('binding_id')::uuid
    AND b.organization_id = @organization_id::text
    AND b.remote_session_client_id = c.id
    AND (c.project_id = b.project_id
         OR (c.project_id IS NULL AND c.organization_id = b.organization_id)
         OR (c.project_id IS NULL AND c.organization_id IS NULL))
) OR EXISTS (
  SELECT 1 FROM mcp_auth_clients AS ac
  WHERE ac.id = sqlc.narg('auth_client_id')::uuid
    AND ac.organization_id = @organization_id::text
    AND ac.client_type = 'ema'
    AND ac.remote_session_client_id = c.id
    AND (c.project_id IS NULL OR EXISTS (
      SELECT 1 FROM projects AS p WHERE p.id = c.project_id
        AND p.organization_id = ac.organization_id
    ))
    AND (c.project_id IS NOT NULL OR c.organization_id IS NULL
         OR c.organization_id = ac.organization_id)
))
FOR NO KEY UPDATE OF c;

-- name: ListEMABackfillBindings :many
-- The organization's ready, client-bound identity chaining bindings in live
-- projects, with the exact resource used for grouping and stable typed keys.
-- in_reach reports that the binding's registration and its issuer are live
-- and belong to the binding's project, its organization or the platform.
SELECT b.id AS binding_id,
       b.project_id,
       b.user_session_issuer_id,
       b.remote_session_issuer_id,
       b.resource AS exact_resource,
       b.resource::text AS resource,
       b.requested_scopes,
       b.remote_session_client_id::uuid AS remote_session_client_id,
       rsi.name AS issuer_name,
       (rsi.id IS NOT NULL AND EXISTS (
         SELECT 1 FROM remote_session_clients AS c
         WHERE c.id = b.remote_session_client_id
           AND c.remote_session_issuer_id = b.remote_session_issuer_id
           AND c.deleted IS FALSE
           AND (c.project_id = b.project_id
                OR (c.project_id IS NULL AND c.organization_id = b.organization_id)
                OR (c.project_id IS NULL AND c.organization_id IS NULL))
       ))::boolean AS in_reach
FROM remote_session_ema_bindings AS b
JOIN projects AS p
  ON p.id = b.project_id
 AND p.organization_id = b.organization_id
 AND p.deleted IS FALSE
LEFT JOIN remote_session_issuers AS rsi
  ON rsi.id = b.remote_session_issuer_id
 AND rsi.deleted IS FALSE
 AND (rsi.project_id = b.project_id
      OR (rsi.project_id IS NULL AND rsi.organization_id = b.organization_id)
      OR (rsi.project_id IS NULL AND rsi.organization_id IS NULL))
WHERE b.organization_id = @organization_id::text
  AND b.state = 'ready'
  AND b.remote_session_client_id IS NOT NULL
  AND (sqlc.narg('only_binding_id')::uuid IS NULL OR EXISTS (
    SELECT 1 FROM remote_session_ema_bindings AS selected
    WHERE selected.id = sqlc.narg('only_binding_id')::uuid
      AND selected.organization_id = b.organization_id
      AND selected.remote_session_issuer_id = b.remote_session_issuer_id
      AND selected.remote_session_client_id = b.remote_session_client_id
      AND selected.resource = b.resource
  ))
  AND (sqlc.narg('only_project_id')::uuid IS NULL OR EXISTS (
    SELECT 1 FROM remote_session_ema_bindings AS selected
    WHERE selected.project_id = sqlc.narg('only_project_id')::uuid
      AND selected.organization_id = b.organization_id
      AND selected.state = 'ready'
      AND selected.remote_session_issuer_id = b.remote_session_issuer_id
      AND selected.remote_session_client_id = b.remote_session_client_id
      AND selected.resource = b.resource
  ))
ORDER BY b.id;

-- name: ListEMABindingServers :many
-- Resource identity is byte-exact. Metadata location normalization may locate
-- a distinct parent resource, but must not alias slash-distinct resources.
-- The live servers a ready binding reaches, as the runtime matches them: in
-- the binding's project and signed in through its user session issuer, either
-- directly or as the issuer of the organization and project gateways, which
-- serve the server as a member and chain it in its own project. A link offers
-- the client on the server whichever way it is reached; the runtime still
-- chains only a request whose own inbound issuer the binding names. It is a
-- remote server whose valid persisted protected resource names the binding's
-- resource, or, with no valid persisted record, whose URL names it; or a
-- tunneled server whose resource identifier names it under its own derived
-- issuer. A remote server whose valid record lists authorization servers
-- without the binding's issuer is returned as issuer_not_allowed, since the
-- runtime would refuse it. direct reports that the server's own issuer is the
-- binding's.
SELECT s.id AS mcp_server_id,
       s.project_id,
       COALESCE(s.user_session_issuer_id = b.user_session_issuer_id, FALSE)::boolean AS direct,
       (CASE WHEN rp.id IS NOT NULL AND rp.authorization_servers IS NOT NULL AND NOT EXISTS (
               SELECT 1 FROM unnest(rp.authorization_servers) AS allowed(server)
               WHERE rtrim(allowed.server, '/') = rtrim(rsi.issuer, '/')
             ) THEN 'issuer_not_allowed'
             ELSE 'match' END)::text AS match
FROM remote_session_ema_bindings AS b
JOIN remote_session_issuers AS rsi
  ON rsi.id = b.remote_session_issuer_id
 AND rsi.deleted IS FALSE
 AND (rsi.project_id = b.project_id
      OR (rsi.project_id IS NULL AND rsi.organization_id = b.organization_id)
      OR (rsi.project_id IS NULL AND rsi.organization_id IS NULL))
JOIN remote_session_clients AS rc
  ON rc.id = b.remote_session_client_id
 AND rc.remote_session_issuer_id = b.remote_session_issuer_id
 AND rc.deleted IS FALSE
 AND (rc.project_id = b.project_id
      OR (rc.project_id IS NULL AND rc.organization_id = b.organization_id)
      OR (rc.project_id IS NULL AND rc.organization_id IS NULL))
JOIN mcp_servers AS s
  ON s.project_id = b.project_id
 AND (s.user_session_issuer_id = b.user_session_issuer_id
      OR b.user_session_issuer_id = (
        -- The organization and project gateways' issuer, as orggateway.Resolve
        -- chooses it: the organization's sole live organization-level user
        -- session issuer. They chain a member in its own project through it.
        -- Configured reach, not current availability: an organization that has
        -- never made a gateway decision is not reached, while turning the
        -- gateway off, disabling the organization or restricting its custom
        -- domain leaves it reached, so no toggle retires a client. Every
        -- request is still matched to its own inbound issuer at runtime.
        SELECT (array_agg(gi.id))[1]
        FROM user_session_issuers AS gi
        WHERE gi.organization_id = b.organization_id
          AND gi.project_id IS NULL
          AND gi.deleted IS FALSE
          AND EXISTS (
            SELECT 1 FROM organization_features AS gf
            WHERE gf.organization_id = b.organization_id
              AND gf.feature_name = 'organization_gateway_disabled'
          )
        HAVING count(*) = 1
      ))
 AND s.deleted IS FALSE
LEFT JOIN remote_mcp_servers AS r ON r.id = s.remote_mcp_server_id AND r.deleted IS FALSE
LEFT JOIN tunneled_mcp_servers AS t ON t.id = s.tunneled_mcp_server_id AND t.deleted IS FALSE
LEFT JOIN LATERAL (
  SELECT rp.id, rp.resource_identifier, rp.authorization_servers
  FROM remote_protected_resources AS rp
  WHERE r.id IS NOT NULL
    AND rp.project_id = s.project_id
    AND rp.organization_id = b.organization_id
    AND rp.deleted IS FALSE
    AND rp.metadata_fetched_at IS NOT NULL
    AND rp.metadata_last_error IS NULL
    AND (rp.resource_identifier = r.url
         OR (rtrim(rp.resource_identifier, '/') <> rtrim(r.url, '/')
             AND rtrim(rp.metadata_url, '/') = rtrim(regexp_replace(r.url, '^(https?://[^/?#]+)(.*)$', '\1/.well-known/oauth-protected-resource\2'), '/')))
  ORDER BY rp.metadata_fetched_at DESC, rp.id
  LIMIT 1
) AS rp ON TRUE
WHERE b.id = @binding_id::uuid
  AND b.organization_id = @organization_id::text
  AND b.state = 'ready'
  AND b.remote_session_client_id IS NOT NULL
  AND (
    (r.id IS NOT NULL AND (
      (rp.id IS NOT NULL AND rp.resource_identifier = b.resource)
      OR (rp.id IS NULL AND r.url = b.resource)
    ))
    OR (t.id IS NOT NULL
        AND s.remote_session_issuer_id = b.remote_session_issuer_id
        AND t.resource_identifier = b.resource)
  )
ORDER BY s.id;

-- name: ListEMASameOriginServers :many
-- The live remote servers a ready binding could plausibly cover without a
-- match: in the binding's project, signed in through its user session issuer
-- directly or through the gateways as ListEMABindingServers reads them,
-- at the same origin as its resource, and with no valid persisted protected
-- resource naming another resource. Whether their metadata names the
-- binding's resource only live discovery can confirm, so these are reported
-- as warnings rather than linked.
SELECT s.id AS mcp_server_id, s.project_id
FROM remote_session_ema_bindings AS b
JOIN mcp_servers AS s
  ON s.project_id = b.project_id
 AND (s.user_session_issuer_id = b.user_session_issuer_id
      OR b.user_session_issuer_id = (
        -- The gateways' issuer, as ListEMABindingServers reads it.
        SELECT (array_agg(gi.id))[1]
        FROM user_session_issuers AS gi
        WHERE gi.organization_id = b.organization_id
          AND gi.project_id IS NULL
          AND gi.deleted IS FALSE
          AND EXISTS (
            SELECT 1 FROM organization_features AS gf
            WHERE gf.organization_id = b.organization_id
              AND gf.feature_name = 'organization_gateway_disabled'
          )
        HAVING count(*) = 1
      ))
 AND s.deleted IS FALSE
JOIN remote_mcp_servers AS r ON r.id = s.remote_mcp_server_id AND r.deleted IS FALSE
LEFT JOIN LATERAL (
  SELECT rp.id, rp.resource_identifier, rp.authorization_servers
  FROM remote_protected_resources AS rp
  WHERE r.id IS NOT NULL
    AND rp.project_id = s.project_id
    AND rp.organization_id = b.organization_id
    AND rp.deleted IS FALSE
    AND rp.metadata_fetched_at IS NOT NULL
    AND rp.metadata_last_error IS NULL
    AND (rp.resource_identifier = r.url
         OR (rtrim(rp.resource_identifier, '/') <> rtrim(r.url, '/')
             AND rtrim(rp.metadata_url, '/') = rtrim(regexp_replace(r.url, '^(https?://[^/?#]+)(.*)$', '\1/.well-known/oauth-protected-resource\2'), '/')))
  ORDER BY rp.metadata_fetched_at DESC, rp.id
  LIMIT 1
) AS rp ON TRUE
WHERE b.id = @binding_id::uuid
  AND b.organization_id = @organization_id::text
  AND b.state = 'ready'
  AND b.remote_session_client_id IS NOT NULL
  AND lower(substring(r.url from '^https://[^/?#]+')) = lower(substring(b.resource from '^https://[^/?#]+'))
  AND (rp.id IS NULL OR rp.resource_identifier = b.resource)
ORDER BY s.id;

-- name: FindEMAAuthClient :one
-- The organization's live backfilled EMA auth client for one upstream, with
-- its stored scopes.
SELECT ac.id, e.requested_scopes
FROM mcp_auth_client_emas AS e
JOIN mcp_auth_clients AS ac
  ON ac.id = e.auth_client_id
 AND ac.organization_id = e.organization_id
WHERE e.organization_id = @organization_id::text
  AND e.remote_session_issuer_id = @remote_session_issuer_id::uuid
  AND e.remote_session_client_id = @remote_session_client_id::uuid
  AND e.resource = @resource::text
  AND e.deleted IS FALSE
  AND ac.deleted IS FALSE
  AND ac.source = 'backfill'
ORDER BY ac.id
LIMIT 1;

-- name: UpdateEMAScopes :execrows
-- Updates a live backfilled EMA client's scopes, conditional on a ready
-- binding of the organization still naming that upstream with those scopes.
UPDATE mcp_auth_client_emas AS e
SET requested_scopes = @requested_scopes::text[], updated_at = clock_timestamp()
WHERE e.auth_client_id = @auth_client_id::uuid
  AND e.organization_id = @organization_id::text
  AND e.deleted IS FALSE
  AND EXISTS (
    SELECT 1 FROM remote_session_ema_bindings AS b
    WHERE b.organization_id = e.organization_id
      AND b.remote_session_issuer_id = e.remote_session_issuer_id
      AND b.remote_session_client_id = e.remote_session_client_id
      AND b.resource = e.resource
      AND b.state = 'ready'
      AND ARRAY(SELECT DISTINCT btrim(x) COLLATE "C" FROM unnest(b.requested_scopes) AS x WHERE btrim(x) <> '' ORDER BY 1) = @requested_scopes::text[]
  );

-- name: CreateEMAAuthClient :one
-- Records the EMA auth client for one upstream with its EMA details in one
-- statement, conditional on the registration still being live, naming a live
-- issuer and in the organization's reach, and a ready binding of the
-- organization still naming that upstream with those scopes. A project
-- registration's client belongs to that project. The id is stable for the
-- organization, issuer, registration and resource, so a client retired while
-- its bindings were gone is revived under the same id, EMA details included.
WITH base AS (
  INSERT INTO mcp_auth_clients (id, organization_id, project_id, client_type, source, name, remote_session_client_id, client_attachment_scope)
  SELECT @id::uuid, @organization_id::text, c.project_id, 'ema', 'backfill', @name::text, c.id, c.attachment_scope
  FROM remote_session_clients AS c
  JOIN remote_session_issuers AS rsi
    ON rsi.id = c.remote_session_issuer_id
   AND rsi.deleted IS FALSE
  WHERE c.id = @remote_session_client_id::uuid
    AND c.remote_session_issuer_id = @remote_session_issuer_id::uuid
    AND c.deleted IS FALSE
    AND ((c.project_id IS NOT NULL AND EXISTS (
           SELECT 1 FROM projects AS p
           WHERE p.id = c.project_id AND p.organization_id = @organization_id::text AND p.deleted IS FALSE
         ))
         OR (c.project_id IS NULL AND c.organization_id = @organization_id::text)
         OR (c.project_id IS NULL AND c.organization_id IS NULL))
    AND EXISTS (
      SELECT 1 FROM remote_session_ema_bindings AS b
      JOIN projects AS bp ON bp.id = b.project_id AND bp.organization_id = b.organization_id AND bp.deleted IS FALSE
      WHERE b.organization_id = @organization_id::text
        AND b.remote_session_issuer_id = c.remote_session_issuer_id
        AND b.remote_session_client_id = c.id
        AND b.resource = @resource::text
        AND b.state = 'ready'
        AND (c.project_id = b.project_id
             OR (c.project_id IS NULL AND c.organization_id = b.organization_id)
             OR (c.project_id IS NULL AND c.organization_id IS NULL))
        AND (rsi.project_id = b.project_id
             OR (rsi.project_id IS NULL AND rsi.organization_id = b.organization_id)
             OR (rsi.project_id IS NULL AND rsi.organization_id IS NULL))
        AND ARRAY(SELECT DISTINCT btrim(x) COLLATE "C" FROM unnest(b.requested_scopes) AS x WHERE btrim(x) <> '' ORDER BY 1) = @requested_scopes::text[]
    )
  ON CONFLICT (id) DO UPDATE
  SET deleted_at = NULL, updated_at = clock_timestamp()
  WHERE mcp_auth_clients.deleted
    AND mcp_auth_clients.source = 'backfill'
    AND mcp_auth_clients.organization_id = EXCLUDED.organization_id
    AND mcp_auth_clients.client_type = 'ema'
    AND mcp_auth_clients.remote_session_client_id = EXCLUDED.remote_session_client_id
  RETURNING id, organization_id, remote_session_client_id
)
INSERT INTO mcp_auth_client_emas (auth_client_id, organization_id, remote_session_client_id, remote_session_issuer_id, resource, requested_scopes)
SELECT base.id, base.organization_id, base.remote_session_client_id, @remote_session_issuer_id::uuid, @resource::text, @requested_scopes::text[]
FROM base
ON CONFLICT (auth_client_id) DO UPDATE
SET deleted_at = NULL, requested_scopes = EXCLUDED.requested_scopes, updated_at = clock_timestamp()
WHERE mcp_auth_client_emas.deleted
  AND mcp_auth_client_emas.remote_session_issuer_id = EXCLUDED.remote_session_issuer_id
  AND mcp_auth_client_emas.resource = EXCLUDED.resource
RETURNING auth_client_id;

-- name: LinkEMAServer :execrows
-- Records that a server offers a live backfilled EMA client, conditional on a
-- ready binding of the organization for that client's upstream still reaching
-- the server the way ListEMABindingServers matches it, with the binding's
-- issuer allowed by the server's valid persisted protected resource.
INSERT INTO mcp_server_auth_clients (project_id, organization_id, mcp_server_id, auth_client_id, client_attachment_scope, source)
SELECT s.project_id, p.organization_id, s.id, ac.id, ac.attachment_scope, 'backfill'
FROM mcp_servers AS s
JOIN projects AS p ON p.id = s.project_id AND p.organization_id = @organization_id::text AND p.deleted IS FALSE
JOIN mcp_auth_clients AS ac
  ON ac.id = @auth_client_id::uuid
 AND ac.organization_id = p.organization_id
 AND ac.client_type = 'ema'
 AND ac.source = 'backfill'
 AND ac.deleted IS FALSE
 AND (ac.project_id IS NULL OR ac.project_id = s.project_id)
JOIN mcp_auth_client_emas AS e ON e.auth_client_id = ac.id AND e.deleted IS FALSE
JOIN remote_session_issuers AS rsi
  ON rsi.id = e.remote_session_issuer_id
 AND rsi.deleted IS FALSE
 AND (rsi.project_id = s.project_id
      OR (rsi.project_id IS NULL AND rsi.organization_id = p.organization_id)
      OR (rsi.project_id IS NULL AND rsi.organization_id IS NULL))
JOIN remote_session_clients AS rc
  ON rc.id = e.remote_session_client_id
 AND rc.remote_session_issuer_id = e.remote_session_issuer_id
 AND rc.deleted IS FALSE
 AND (rc.project_id = s.project_id
      OR (rc.project_id IS NULL AND rc.organization_id = p.organization_id)
      OR (rc.project_id IS NULL AND rc.organization_id IS NULL))
LEFT JOIN remote_mcp_servers AS r ON r.id = s.remote_mcp_server_id AND r.deleted IS FALSE
LEFT JOIN tunneled_mcp_servers AS t ON t.id = s.tunneled_mcp_server_id AND t.deleted IS FALSE
LEFT JOIN LATERAL (
  SELECT rp.id, rp.resource_identifier, rp.authorization_servers
  FROM remote_protected_resources AS rp
  WHERE r.id IS NOT NULL
    AND rp.project_id = s.project_id
    AND rp.organization_id = p.organization_id
    AND rp.deleted IS FALSE
    AND rp.metadata_fetched_at IS NOT NULL
    AND rp.metadata_last_error IS NULL
    AND (rp.resource_identifier = r.url
         OR (rtrim(rp.resource_identifier, '/') <> rtrim(r.url, '/')
             AND rtrim(rp.metadata_url, '/') = rtrim(regexp_replace(r.url, '^(https?://[^/?#]+)(.*)$', '\1/.well-known/oauth-protected-resource\2'), '/')))
  ORDER BY rp.metadata_fetched_at DESC, rp.id
  LIMIT 1
) AS rp ON TRUE
WHERE s.id = @mcp_server_id::uuid
  AND s.deleted IS FALSE
  AND (rp.id IS NULL OR rp.authorization_servers IS NULL OR EXISTS (
    SELECT 1 FROM unnest(rp.authorization_servers) AS allowed(server)
    WHERE rtrim(allowed.server, '/') = rtrim(rsi.issuer, '/')
  ))
  AND EXISTS (
    SELECT 1 FROM remote_session_ema_bindings AS b
    WHERE b.organization_id = p.organization_id
      AND b.project_id = s.project_id
      AND (b.user_session_issuer_id = s.user_session_issuer_id
           OR b.user_session_issuer_id = (
             -- The gateways' issuer, as ListEMABindingServers reads it.
             SELECT (array_agg(gi.id))[1]
             FROM user_session_issuers AS gi
             WHERE gi.organization_id = b.organization_id
               AND gi.project_id IS NULL
               AND gi.deleted IS FALSE
               AND EXISTS (
                 SELECT 1 FROM organization_features AS gf
                 WHERE gf.organization_id = b.organization_id
                   AND gf.feature_name = 'organization_gateway_disabled'
               )
             HAVING count(*) = 1
           ))
      AND b.remote_session_issuer_id = e.remote_session_issuer_id
      AND b.remote_session_client_id = e.remote_session_client_id
      AND b.resource = e.resource
      AND b.state = 'ready'
      AND (
        (r.id IS NOT NULL AND (
          (rp.id IS NOT NULL AND rp.resource_identifier = b.resource)
          OR (rp.id IS NULL AND r.url = b.resource)
        ))
        OR (t.id IS NOT NULL
            AND s.remote_session_issuer_id = b.remote_session_issuer_id
            AND t.resource_identifier = b.resource)
      )
  )
  AND NOT EXISTS (
    -- An administrator detached this client from this server: the backfill
    -- never links it again.
    SELECT 1
    FROM mcp_server_auth_clients AS detached
    WHERE detached.mcp_server_id = s.id
      AND detached.auth_client_id = ac.id
      AND detached.source = 'admin'
      AND detached.deleted IS TRUE
  )
ON CONFLICT (mcp_server_id, auth_client_id) WHERE deleted IS FALSE DO NOTHING;

-- name: IsServerLinked :one
-- Whether a server already offers an auth client, or an administrator
-- detached it from the server so the backfill must not link it again.
SELECT EXISTS (
  SELECT 1
  FROM mcp_server_auth_clients AS link
  WHERE link.mcp_server_id = @mcp_server_id::uuid
    AND link.auth_client_id = @auth_client_id::uuid
    AND link.organization_id = @organization_id::text
    AND ((link.source = 'backfill' AND link.deleted IS FALSE)
         OR (link.source = 'admin' AND link.deleted IS TRUE))
)::boolean AS linked;

-- name: ListBackfillClientsOfTypes :many
-- Live backfilled auth clients of the given types in the organization, for
-- the runner to retire those the configuration no longer yields.
SELECT ac.id, ac.client_type
FROM mcp_auth_clients AS ac
WHERE ac.organization_id = @organization_id::text
  AND ac.source = 'backfill'
  AND ac.client_type = ANY(@client_types::text[])
  AND ac.deleted IS FALSE
ORDER BY ac.id;

-- name: RetireBackfillClient :execrows
-- Soft-deletes one live backfilled auth client of the given types, with its
-- EMA details and header copies, unless the configuration it was derived from
-- changed since the run began: a header of a source one of its servers is on,
-- or a binding naming its EMA upstream. The run decided from evidence read
-- earlier, so a change since then is left for the next run.
WITH retired AS (
  UPDATE mcp_auth_clients AS ac
  SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
  WHERE ac.id = @id::uuid
    AND ac.organization_id = @organization_id::text
    AND ac.source = 'backfill'
    AND ac.client_type = ANY(@client_types::text[])
    AND ac.deleted IS FALSE
    AND NOT EXISTS (
      SELECT 1
      FROM mcp_server_auth_clients AS l
      JOIN mcp_servers AS s ON s.id = l.mcp_server_id AND s.project_id = l.project_id AND s.deleted IS FALSE
      WHERE l.auth_client_id = ac.id
        AND l.organization_id = ac.organization_id
        AND l.deleted IS FALSE
        AND (
          EXISTS (
            SELECT 1 FROM remote_mcp_server_headers AS h
            JOIN remote_mcp_servers AS r ON r.id = h.remote_mcp_server_id AND r.project_id = s.project_id
            WHERE h.remote_mcp_server_id = s.remote_mcp_server_id
              AND GREATEST(h.created_at, h.updated_at, h.deleted_at) >= @evidence_since::timestamptz
          )
          OR EXISTS (
            SELECT 1 FROM tunneled_mcp_server_headers AS h
            JOIN tunneled_mcp_servers AS t ON t.id = h.tunneled_mcp_server_id AND t.project_id = s.project_id
            WHERE h.tunneled_mcp_server_id = s.tunneled_mcp_server_id
              AND GREATEST(h.created_at, h.updated_at, h.deleted_at) >= @evidence_since::timestamptz
          )
        )
    )
    AND NOT EXISTS (
      SELECT 1
      FROM mcp_auth_client_emas AS e
      JOIN remote_session_ema_bindings AS b
        ON b.organization_id = e.organization_id
       AND b.remote_session_issuer_id = e.remote_session_issuer_id
       AND b.remote_session_client_id = e.remote_session_client_id
       AND b.resource = e.resource
      WHERE e.auth_client_id = ac.id
        AND GREATEST(b.created_at, b.updated_at) >= @evidence_since::timestamptz
    )
  RETURNING ac.id
), details AS (
  UPDATE mcp_auth_client_emas AS e
  SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
  FROM retired
  WHERE e.auth_client_id = retired.id
    AND e.organization_id = @organization_id::text
    AND e.deleted IS FALSE
  RETURNING e.auth_client_id
), headers AS (
  UPDATE mcp_auth_client_headers AS h
  SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
  FROM retired
  WHERE h.auth_client_id = retired.id
    AND h.organization_id = @organization_id::text
    AND h.deleted IS FALSE
  RETURNING h.id
)
SELECT id FROM retired;

-- name: ListBackfillLinksOfTypes :many
-- Live backfilled server links in the organization to backfilled auth
-- clients of the given types, with whether the client is retired.
SELECT l.id, l.mcp_server_id, l.auth_client_id, ac.client_type, ac.deleted AS client_deleted
FROM mcp_server_auth_clients AS l
JOIN mcp_auth_clients AS ac
  ON ac.id = l.auth_client_id
 AND ac.organization_id = l.organization_id
WHERE l.organization_id = @organization_id::text
  AND l.source = 'backfill'
  AND l.deleted IS FALSE
  AND ac.source = 'backfill'
  AND ac.client_type = ANY(@client_types::text[])
ORDER BY l.id;

-- name: RetireBackfillLink :execrows
-- Soft-deletes one live backfilled server link.
UPDATE mcp_server_auth_clients
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE id = @id::uuid
  AND organization_id = @organization_id::text
  AND source = 'backfill'
  AND deleted IS FALSE;

-- name: LockRemoteCredentialSource :one
-- Locks one remote source of the organization, live or deleted, so header
-- syncs for it run one at a time and see each other's writes. The source's
-- own writers take this lock before touching its headers, so the two never
-- wait on each other in opposite order. No row means the source is not the
-- organization's.
SELECT r.id
FROM remote_mcp_servers AS r
JOIN projects AS p ON p.id = r.project_id AND p.organization_id = @organization_id::text
WHERE r.id = @source_id::uuid
FOR UPDATE OF r;

-- name: ListRemoteSourceHeaderIDs :many
-- Every header ever recorded on one remote source of the organization,
-- deleted ones included, so the auth clients derived from any of them can be
-- found by their stable ids.
SELECT h.id
FROM remote_mcp_server_headers AS h
JOIN remote_mcp_servers AS r ON r.id = h.remote_mcp_server_id
JOIN projects AS p ON p.id = r.project_id AND p.organization_id = @organization_id::text
WHERE h.remote_mcp_server_id = @source_id::uuid
ORDER BY h.id;

-- name: ListOAuthClientsOfServers :many
-- Every upstream OAuth client the given servers are associated with, deleted
-- associations and tombstoned servers included, so a server that has just
-- left a source or been deleted still names the clients whose copies have to
-- be re-decided.
SELECT DISTINCT a.remote_session_client_id
FROM mcp_server_remote_session_clients AS a
WHERE a.organization_id = @organization_id::text
  AND a.mcp_server_id = ANY(@mcp_server_ids::uuid[])
UNION
SELECT ac.remote_session_client_id
FROM mcp_server_auth_clients AS l
JOIN mcp_auth_clients AS ac ON ac.id = l.auth_client_id AND ac.organization_id = l.organization_id
WHERE l.organization_id = @organization_id::text
  AND l.mcp_server_id = ANY(@mcp_server_ids::uuid[])
  AND ac.client_type IN ('remote_session', 'service_account')
  AND ac.source = 'backfill'
  AND ac.remote_session_client_id IS NOT NULL
ORDER BY remote_session_client_id;

-- name: ListEMAClientsOfServers :many
-- Every live backfilled EMA client the given servers are linked to, with the
-- live servers that offer it, so the rule that refuses a client shared across
-- sources sees the same evidence as the backfill. The naming link is read
-- deleted ones included, so a server that has just left a source or been
-- deleted still names its clients; an empty server list then means the client
-- serves nothing and carries no source's credentials.
SELECT ac.id AS auth_client_id,
       COALESCE(array_agg(DISTINCT ls.id) FILTER (WHERE ls.id IS NOT NULL), ARRAY[]::uuid[])::uuid[] AS mcp_server_ids
FROM mcp_auth_clients AS ac
JOIN mcp_server_auth_clients AS named
  ON named.auth_client_id = ac.id
 AND named.organization_id = ac.organization_id
 AND named.mcp_server_id = ANY(@mcp_server_ids::uuid[])
LEFT JOIN mcp_server_auth_clients AS live
  ON live.auth_client_id = ac.id
 AND live.organization_id = ac.organization_id
 AND live.deleted IS FALSE
LEFT JOIN mcp_servers AS ls
  ON ls.id = live.mcp_server_id
 AND ls.project_id = live.project_id
 AND ls.deleted IS FALSE
 AND EXISTS (SELECT 1 FROM projects AS p WHERE p.id = ls.project_id AND p.organization_id = ac.organization_id AND p.deleted IS FALSE)
WHERE ac.organization_id = @organization_id::text
  AND ac.client_type = 'ema'
  AND ac.source = 'backfill'
  AND ac.deleted IS FALSE
GROUP BY ac.id
ORDER BY ac.remote_session_client_id, ac.id;

-- name: LockTunneledCredentialSource :one
-- LockRemoteCredentialSource for a tunneled source.
SELECT t.id
FROM tunneled_mcp_servers AS t
JOIN projects AS p ON p.id = t.project_id AND p.organization_id = @organization_id::text
WHERE t.id = @source_id::uuid
FOR UPDATE OF t;

-- name: ListTunneledSourceHeaderIDs :many
-- ListRemoteSourceHeaderIDs for a tunneled source.
SELECT h.id
FROM tunneled_mcp_server_headers AS h
JOIN tunneled_mcp_servers AS t ON t.id = h.tunneled_mcp_server_id
JOIN projects AS p ON p.id = t.project_id AND p.organization_id = @organization_id::text
WHERE h.tunneled_mcp_server_id = @source_id::uuid
ORDER BY h.id;

-- name: ListTunneledSourceServerIDs :many
-- The servers backed by one tunneled source, as ListTunneledSourceServers
-- reads them but without the upstream OAuth state, for the copy pass.
SELECT s.id
FROM mcp_servers AS s
JOIN projects AS p ON p.id = s.project_id AND p.organization_id = @organization_id::text AND p.deleted IS FALSE
WHERE s.tunneled_mcp_server_id = @source_id::uuid
ORDER BY s.id;

-- name: ListRemoteSourceServerIDs :many
-- The servers backed by one remote source, tombstoned ones included, so a
-- source whose last server was deleted still names it for the copy pass.
SELECT s.id
FROM mcp_servers AS s
JOIN projects AS p ON p.id = s.project_id AND p.organization_id = @organization_id::text AND p.deleted IS FALSE
WHERE s.remote_mcp_server_id = @source_id::uuid
ORDER BY s.id;

-- name: ListLiveBackfillClients :many
-- The clients among those given, in the organization, that are live
-- backfilled clients of one of the given types. Only those can be retired, so
-- a sync reads the live ones once instead of attempting every derived id.
SELECT ac.id
FROM mcp_auth_clients AS ac
WHERE ac.organization_id = @organization_id::text
  AND ac.id = ANY(@auth_client_ids::uuid[])
  AND ac.source = 'backfill'
  AND ac.client_type = ANY(@client_types::text[])
  AND ac.deleted IS FALSE
ORDER BY ac.id;

-- name: ListBackfillLinksOfClients :many
-- Live backfilled server links in the organization to any of the given auth
-- clients.
SELECT l.id, l.mcp_server_id, l.auth_client_id
FROM mcp_server_auth_clients AS l
WHERE l.organization_id = @organization_id::text
  AND l.auth_client_id = ANY(@auth_client_ids::uuid[])
  AND l.source = 'backfill'
  AND l.deleted IS FALSE
ORDER BY l.id;

-- name: ListLiveCredentialClasses :many
-- One row per live user session credential in the organization after the
-- cursor, with the class coverage is reported by and the user session issuer
-- it was issued by. A credential is live while a row of it is not deleted and
-- can still refresh; its class is read from its latest row. The subject kind
-- names agent and workload sessions; the refresh token hash prefixes are the
-- ones issuance writes: id-jag: for an inbound ID-JAG exchange and
-- dashboard-mint for a session the dashboard minted.
SELECT DISTINCT ON (COALESCE(s.credential_id, s.id))
       COALESCE(s.credential_id, s.id)::uuid AS credential_id,
       s.user_session_issuer_id,
       (CASE
         WHEN s.subject_urn LIKE 'agent:%' THEN 'agent'
         WHEN s.subject_urn LIKE 'workload:%' THEN 'workload'
         WHEN s.refresh_token_hash LIKE 'id-jag:%' THEN 'id_jag'
         WHEN s.refresh_token_hash LIKE 'dashboard-mint%' THEN 'dashboard_mint'
         ELSE 'interactive'
       END)::text AS credential_class
FROM user_sessions AS s
JOIN user_session_issuers AS usi
  ON usi.id = s.user_session_issuer_id
LEFT JOIN projects AS p
  ON p.id = usi.project_id
WHERE s.deleted IS FALSE
  AND s.refresh_expires_at > clock_timestamp()
  AND COALESCE(s.credential_id, s.id) > @cursor::uuid
  AND COALESCE(usi.organization_id, p.organization_id) = @organization_id::text
ORDER BY COALESCE(s.credential_id, s.id), s.created_at DESC
LIMIT @page_size::int;

-- name: ListSelectionMappings :many
-- Every live user session choice for a live server in the organization after
-- the cursor, or the one named by only_id, with what mapping it onto a typed
-- auth client decides from.
--
-- What the upstream client choice reads today (GetUpstreamClientSelection):
-- whether the server has any upstream OAuth client association, whether the
-- chosen registration is live and associated with the server, the user
-- session issuers the registration is bound to, and whether the choice names
-- the server's headers, which reads as no-auth.
--
-- The typed auth clients: an OAuth choice can name the live, enabled
-- remote_session or service_account clients of its registration linked to
-- the server. A no-auth choice can name the server's linked, enabled
-- static_credential or pass_through clients that carry exactly the server
-- source's current credential headers, while the server has no OAuth
-- association and links no enabled OAuth-backed client: those are the
-- server's own headers, which the no-auth choice was served with. When the
-- source has no credential headers, a client an administrator created or
-- linked is the server's own configuration and is a candidate too. A mapped
-- choice's client is usable while it is live, enabled, linked to the server
-- and, for an OAuth choice, still the registration's.
SELECT sel.id,
       sel.project_id,
       sel.credential_id,
       sel.mcp_server_id,
       sel.remote_session_client_id,
       sel.no_auth,
       sel.auth_client_id,
       (sel.auth_client_id IS NOT NULL AND EXISTS (
         SELECT 1
         FROM mcp_auth_clients AS ac
         JOIN mcp_server_auth_clients AS l
           ON l.auth_client_id = ac.id
          AND l.mcp_server_id = sel.mcp_server_id
          AND l.deleted IS FALSE
         WHERE ac.id = sel.auth_client_id
           AND ac.organization_id = sel.organization_id
           AND ac.deleted IS FALSE
           AND ac.enabled
           AND (sel.remote_session_client_id IS NULL OR ac.remote_session_client_id = sel.remote_session_client_id)
       ))::boolean AS mapped_usable,
       EXISTS (
         SELECT 1
         FROM mcp_auth_clients AS headers_only
         WHERE headers_only.id = sel.auth_client_id
           AND sel.remote_session_client_id IS NULL
           AND headers_only.organization_id = sel.organization_id
           AND (headers_only.project_id IS NULL OR headers_only.project_id = sel.project_id)
           AND headers_only.client_type IN ('static_credential', 'pass_through')
       )::boolean AS names_headers,
       EXISTS (
         SELECT 1
         FROM mcp_server_remote_session_clients AS a
         WHERE a.mcp_server_id = sel.mcp_server_id
           AND a.deleted IS FALSE
       )::boolean AS server_has_associations,
       EXISTS (
         SELECT 1
         FROM mcp_server_remote_session_clients AS a
         JOIN remote_session_clients AS c
           ON c.id = a.remote_session_client_id
          AND c.deleted IS FALSE
         WHERE a.mcp_server_id = sel.mcp_server_id
           AND a.remote_session_client_id = sel.remote_session_client_id
           AND a.deleted IS FALSE
       )::boolean AS registration_associated,
       ARRAY(
         SELECT link.user_session_issuer_id
         FROM remote_session_client_user_session_issuers AS link
         WHERE link.remote_session_client_id = sel.remote_session_client_id
         ORDER BY link.user_session_issuer_id
       )::uuid[] AS registration_issuer_ids,
       ARRAY(
         SELECT ac.id
         FROM mcp_auth_clients AS ac
         JOIN mcp_server_auth_clients AS l
           ON l.auth_client_id = ac.id
          AND l.mcp_server_id = sel.mcp_server_id
          AND l.deleted IS FALSE
         WHERE ac.organization_id = sel.organization_id
           AND ac.deleted IS FALSE
           AND ac.enabled
           AND (
             (sel.remote_session_client_id IS NOT NULL
              AND ac.remote_session_client_id = sel.remote_session_client_id
              AND ac.client_type IN ('remote_session', 'service_account'))
             OR (sel.no_auth
                 AND ac.client_type IN ('static_credential', 'pass_through')
                 -- The client carries the source's current credential
                 -- headers, compared as stored. RecordUpstreamClientSelections
                 -- repeats this comparison; the two must stay identical.
                 AND (
                   SELECT COALESCE(
                            cmp.client_headers = cmp.source_headers,
                            -- A source with no credential headers has none to carry. A
                            -- client or association an administrator created there is
                            -- the server's own configuration and mirrors nothing, as
                            -- GetAuthClientSelection reads it.
                            cmp.source_headers IS NULL
                            AND NOT (ac.source = 'backfill' AND NOT EXISTS (
                              SELECT 1
                              FROM mcp_server_auth_clients AS admin_link
                              WHERE admin_link.mcp_server_id = sel.mcp_server_id
                                AND admin_link.auth_client_id = ac.id
                                AND admin_link.organization_id = ac.organization_id
                                AND admin_link.deleted IS FALSE
                                AND admin_link.source <> 'backfill'
                            ))
                          )
                   FROM (
                     SELECT (
                       SELECT string_agg(format('%L|%L|%L', lower(h.name), h.value, h.value_from_request_header), ',' ORDER BY lower(h.name), h.value, h.value_from_request_header)
                       FROM mcp_auth_client_headers AS h
                       WHERE h.auth_client_id = ac.id
                         AND h.deleted IS FALSE
                     ) AS client_headers, (
                       SELECT string_agg(format('%L|%L|%L', lower(src.name), src.value, src.value_from_request_header), ',' ORDER BY lower(src.name), src.value, src.value_from_request_header)
                       FROM (
                         SELECT rh.name, rh.value, rh.value_from_request_header, rh.is_secret
                         FROM mcp_servers AS s
                         JOIN remote_mcp_servers AS r
                           ON r.id = s.remote_mcp_server_id
                          AND r.project_id = s.project_id
                          AND r.deleted IS FALSE
                         JOIN remote_mcp_server_headers AS rh
                           ON rh.remote_mcp_server_id = r.id
                          AND rh.deleted IS FALSE
                         WHERE s.id = sel.mcp_server_id
                         UNION ALL
                         SELECT th.name, th.value, th.value_from_request_header, th.is_secret
                         FROM mcp_servers AS s
                         JOIN tunneled_mcp_servers AS t
                           ON t.id = s.tunneled_mcp_server_id
                          AND t.project_id = s.project_id
                          AND t.deleted IS FALSE
                         JOIN tunneled_mcp_server_headers AS th
                           ON th.tunneled_mcp_server_id = t.id
                          AND th.deleted IS FALSE
                         WHERE s.id = sel.mcp_server_id
                       ) AS src
                       WHERE lower(src.name) = 'authorization' OR src.value_from_request_header IS NOT NULL OR src.is_secret
                     ) AS source_headers
                   ) AS cmp
                 ))
           )
         ORDER BY ac.id
       )::uuid[] AS candidate_ids,
       EXISTS (
         SELECT 1
         FROM mcp_auth_clients AS ac
         JOIN mcp_server_auth_clients AS l
           ON l.auth_client_id = ac.id
          AND l.mcp_server_id = sel.mcp_server_id
          AND l.deleted IS FALSE
         WHERE ac.organization_id = sel.organization_id
           AND ac.deleted IS FALSE
           AND ac.enabled
           AND ac.client_type IN ('remote_session', 'service_account', 'ema')
           AND NOT (ac.client_type = 'ema' AND EXISTS (
             -- Reached only through the gateways: the server's own issuer has no
             -- ready binding for the client's upstream, so the client cannot serve
             -- its direct route and leaves that route's configuration alone.
             SELECT 1
             FROM mcp_auth_client_emas AS gx
             JOIN mcp_servers AS gs ON gs.id = l.mcp_server_id
             WHERE gx.auth_client_id = ac.id
               AND gx.deleted IS FALSE
               AND NOT EXISTS (
                 SELECT 1 FROM remote_session_ema_bindings AS db
                 WHERE db.organization_id = gx.organization_id
                   AND db.project_id = gs.project_id
                   AND db.remote_session_issuer_id = gx.remote_session_issuer_id
                   AND db.remote_session_client_id = gx.remote_session_client_id
                   AND db.resource = gx.resource
                   AND db.state = 'ready'
                   AND db.user_session_issuer_id = gs.user_session_issuer_id
               )
               AND EXISTS (
                 SELECT 1 FROM remote_session_ema_bindings AS gb
                 WHERE gb.organization_id = gx.organization_id
                   AND gb.project_id = gs.project_id
                   AND gb.remote_session_issuer_id = gx.remote_session_issuer_id
                   AND gb.remote_session_client_id = gx.remote_session_client_id
                   AND gb.resource = gx.resource
                   AND gb.state = 'ready'
                   AND gb.user_session_issuer_id = (
                     -- The gateways' issuer, as ListEMABindingServers reads it.
                     SELECT (array_agg(gi.id))[1]
                     FROM user_session_issuers AS gi
                     WHERE gi.organization_id = gb.organization_id
                       AND gi.project_id IS NULL
                       AND gi.deleted IS FALSE
                       AND EXISTS (
                         SELECT 1 FROM organization_features AS gf
                         WHERE gf.organization_id = gb.organization_id
                           AND gf.feature_name = 'organization_gateway_disabled'
                       )
                     HAVING count(*) = 1
                   )
               )
           ))
       )::boolean AS server_has_oauth_clients,
       -- The gateways' issuer when an EMA client reaches the server only through
       -- them: a choice whose credential it issued reads the client as
       -- OAuth-backed, a choice made on the direct route does not.
       ARRAY(
         SELECT DISTINCT gb2.user_session_issuer_id
         FROM mcp_auth_clients AS ac
         JOIN mcp_server_auth_clients AS l
           ON l.auth_client_id = ac.id
          AND l.mcp_server_id = sel.mcp_server_id
          AND l.deleted IS FALSE
         JOIN mcp_auth_client_emas AS x ON x.auth_client_id = ac.id AND x.deleted IS FALSE
         JOIN remote_session_ema_bindings AS gb2
           ON gb2.organization_id = x.organization_id
          AND gb2.project_id = sel.project_id
          AND gb2.remote_session_issuer_id = x.remote_session_issuer_id
          AND gb2.remote_session_client_id = x.remote_session_client_id
          AND gb2.resource = x.resource
          AND gb2.state = 'ready'
          AND gb2.user_session_issuer_id = (
            -- The gateways' issuer, as ListEMABindingServers reads it.
            SELECT (array_agg(gi.id))[1]
            FROM user_session_issuers AS gi
            WHERE gi.organization_id = gb2.organization_id
              AND gi.project_id IS NULL
              AND gi.deleted IS FALSE
              AND EXISTS (
                SELECT 1 FROM organization_features AS gf
                WHERE gf.organization_id = gb2.organization_id
                  AND gf.feature_name = 'organization_gateway_disabled'
              )
            HAVING count(*) = 1
          )
         WHERE ac.organization_id = sel.organization_id
           AND ac.deleted IS FALSE
           AND ac.enabled
           AND ac.client_type = 'ema'
           AND EXISTS (
             -- Reached only through the gateways: the server's own issuer has no
             -- ready binding for the client's upstream, so the client cannot serve
             -- its direct route and leaves that route's configuration alone.
             SELECT 1
             FROM mcp_auth_client_emas AS gx
             JOIN mcp_servers AS gs ON gs.id = l.mcp_server_id
             WHERE gx.auth_client_id = ac.id
               AND gx.deleted IS FALSE
               AND NOT EXISTS (
                 SELECT 1 FROM remote_session_ema_bindings AS db
                 WHERE db.organization_id = gx.organization_id
                   AND db.project_id = gs.project_id
                   AND db.remote_session_issuer_id = gx.remote_session_issuer_id
                   AND db.remote_session_client_id = gx.remote_session_client_id
                   AND db.resource = gx.resource
                   AND db.state = 'ready'
                   AND db.user_session_issuer_id = gs.user_session_issuer_id
               )
               AND EXISTS (
                 SELECT 1 FROM remote_session_ema_bindings AS gb
                 WHERE gb.organization_id = gx.organization_id
                   AND gb.project_id = gs.project_id
                   AND gb.remote_session_issuer_id = gx.remote_session_issuer_id
                   AND gb.remote_session_client_id = gx.remote_session_client_id
                   AND gb.resource = gx.resource
                   AND gb.state = 'ready'
                   AND gb.user_session_issuer_id = (
                     -- The gateways' issuer, as ListEMABindingServers reads it.
                     SELECT (array_agg(gi.id))[1]
                     FROM user_session_issuers AS gi
                     WHERE gi.organization_id = gb.organization_id
                       AND gi.project_id IS NULL
                       AND gi.deleted IS FALSE
                       AND EXISTS (
                         SELECT 1 FROM organization_features AS gf
                         WHERE gf.organization_id = gb.organization_id
                           AND gf.feature_name = 'organization_gateway_disabled'
                       )
                     HAVING count(*) = 1
                   )
               )
           )
       )::uuid[] AS ema_gateway_issuer_ids
FROM upstream_client_selections AS sel
WHERE sel.organization_id = @organization_id::text
  AND sel.credential_kind = 'user_session'
  AND sel.deleted IS FALSE
  -- A choice for a deleted server is kept as history but is never served
  -- again, so it is not live coverage.
  AND EXISTS (
    SELECT 1
    FROM mcp_servers AS live_server
    WHERE live_server.id = sel.mcp_server_id
      AND live_server.deleted IS FALSE
  )
  AND sel.id > @cursor::uuid
  AND (sqlc.narg('only_id')::uuid IS NULL OR sel.id = sqlc.narg('only_id')::uuid)
ORDER BY sel.id
LIMIT @page_size::int;

-- name: LockSelectionRemoteSource :exec
-- Holds the remote source of a choice's server against header writers, which
-- lock it FOR UPDATE, until the transaction ends. Taken before
-- LockSelectionServer.
SELECT r.id
FROM remote_mcp_servers AS r
JOIN mcp_servers AS s ON s.remote_mcp_server_id = r.id
WHERE s.id = @mcp_server_id::uuid
FOR SHARE OF r;

-- name: LockSelectionTunneledSource :exec
-- LockSelectionRemoteSource for a tunneled source.
SELECT t.id
FROM tunneled_mcp_servers AS t
JOIN mcp_servers AS s ON s.tunneled_mcp_server_id = t.id
WHERE s.id = @mcp_server_id::uuid
FOR SHARE OF t;

-- name: LockSelectionServer :exec
-- Holds a choice's server against configuration writers, which lock it FOR
-- UPDATE before changing its upstream OAuth client associations, until the
-- transaction ends.
SELECT s.id
FROM mcp_servers AS s
WHERE s.id = @mcp_server_id::uuid
FOR SHARE;

-- name: LockSelectionServerAuthClients :exec
-- Holds every auth client linked to a choice's server, enabled or not,
-- against auth client writers, which lock a client FOR UPDATE before enabling,
-- disabling or detaching it, until the transaction ends. Taken after
-- LockSelectionServer.
SELECT ac.id
FROM mcp_auth_clients AS ac
JOIN mcp_server_auth_clients AS l
  ON l.auth_client_id = ac.id
 AND l.mcp_server_id = @mcp_server_id::uuid
 AND l.deleted IS FALSE
WHERE ac.organization_id = @organization_id::text
  AND ac.deleted IS FALSE
ORDER BY ac.id
FOR SHARE OF ac;

-- name: LockSelectionIssuerBinding :exec
-- Holds the binding of an unmapped OAuth choice's registration to its
-- credential's user session issuer, which is what makes the upstream client
-- choice usable, against unbinding until the transaction ends, so a choice is
-- never named after the binding it was decided from is gone. Taken after
-- LockSelectionServerAuthClients. A binding that is already gone locks
-- nothing, and the fresh decision then rejects the choice.
SELECT link.remote_session_client_id
FROM remote_session_client_user_session_issuers AS link
WHERE link.remote_session_client_id = @remote_session_client_id::uuid
  AND link.user_session_issuer_id = @user_session_issuer_id::uuid
FOR SHARE OF link;

-- name: MapSelectionToAuthClient :execrows
-- Names a typed auth client on one choice, conditional on the evidence it was
-- decided from: the choice is still unmapped and unchanged, and the client is
-- still live, enabled and linked to the server. The caller holds the
-- LockSelection* locks and decided from a read taken after them, so the
-- decision as a whole, not only this client, still holds. Naming a client is
-- never no-auth, so a no-auth choice served by the server's headers stops
-- being one; the upstream client choice reads a choice naming a header client
-- as no-auth, as before.
UPDATE upstream_client_selections AS sel
SET auth_client_id = ac.id,
    auth_client_attachment_scope = ac.attachment_scope,
    no_auth = FALSE,
    updated_at = clock_timestamp()
FROM mcp_auth_clients AS ac
WHERE sel.id = @id::uuid
  AND sel.organization_id = @organization_id::text
  AND sel.deleted IS FALSE
  AND sel.auth_client_id IS NULL
  AND sel.no_auth = @no_auth::boolean
  AND sel.remote_session_client_id IS NOT DISTINCT FROM sqlc.narg('remote_session_client_id')::uuid
  AND ac.id = @auth_client_id::uuid
  AND ac.organization_id = sel.organization_id
  AND ac.deleted IS FALSE
  AND ac.enabled
  AND EXISTS (
    SELECT 1
    FROM mcp_server_auth_clients AS l
    WHERE l.auth_client_id = ac.id
      AND l.mcp_server_id = sel.mcp_server_id
      AND l.deleted IS FALSE
  );

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

-- name: MoveServerSourceFixture :exec
UPDATE mcp_servers SET remote_mcp_server_id = @remote_mcp_server_id WHERE id = @id;

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

-- name: SoftDeleteAssociationOfClientFixture :exec
UPDATE mcp_server_remote_session_clients SET deleted_at = clock_timestamp()
WHERE mcp_server_id = @mcp_server_id AND remote_session_client_id = @remote_session_client_id AND deleted IS FALSE;

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

-- name: GetAuthClientFixture :one
SELECT client_type, deleted FROM mcp_auth_clients WHERE id = @id;

-- name: ListBackfillLinkedServersFixture :many
SELECT mcp_server_id
FROM mcp_server_auth_clients
WHERE auth_client_id = @auth_client_id AND source = 'backfill' AND deleted IS FALSE
ORDER BY mcp_server_id;

-- name: SoftDeleteServerAuthClientLinksFixture :exec
UPDATE mcp_server_auth_clients SET deleted_at = clock_timestamp() WHERE mcp_server_id = @mcp_server_id;

-- name: SeedAuthClientFixture :one
INSERT INTO mcp_auth_clients (organization_id, project_id, client_type, source, name, remote_session_client_id, client_attachment_scope)
VALUES (
    @organization_id, @project_id, @client_type, @source, 'seeded', @remote_session_client_id,
    (SELECT c.attachment_scope FROM remote_session_clients AS c WHERE c.id = @remote_session_client_id)
)
RETURNING id;

-- name: SeedRemoteHeaderFixture :one
INSERT INTO remote_mcp_server_headers (remote_mcp_server_id, name, is_secret, value, value_from_request_header)
VALUES (@remote_mcp_server_id, @name, @is_secret, sqlc.narg('value'), sqlc.narg('value_from_request_header'))
RETURNING id;

-- name: SeedTunneledSourceFixture :exec
INSERT INTO tunneled_mcp_servers (id, project_id, name, key_hash, key_prefix, resource_identifier)
VALUES (@id, @project_id, @name, @key_hash, 'tk_', sqlc.narg('resource_identifier'));

-- name: SeedTunneledHeaderFixture :one
INSERT INTO tunneled_mcp_server_headers (tunneled_mcp_server_id, name, is_secret, value, value_from_request_header)
VALUES (@tunneled_mcp_server_id, @name, @is_secret, sqlc.narg('value'), sqlc.narg('value_from_request_header'))
RETURNING id;

-- name: SeedTunneledMCPServerFixture :exec
INSERT INTO mcp_servers (id, project_id, slug, tunneled_mcp_server_id, visibility) VALUES (@id, @project_id, @slug, @tunneled_mcp_server_id, 'private');

-- name: SeedUserSessionIssuerFixture :exec
INSERT INTO user_session_issuers (id, project_id, slug, authn_challenge_mode, session_duration)
VALUES (@id, @project_id, @slug, 'interactive', '1 day');

-- name: SetServerUserSessionIssuerFixture :exec
UPDATE mcp_servers SET user_session_issuer_id = @user_session_issuer_id WHERE id = @id;

-- name: SeedEMABindingFixture :one
INSERT INTO remote_session_ema_bindings (project_id, organization_id, user_session_issuer_id, remote_session_issuer_id, resource, remote_session_client_id, requested_scopes, state)
VALUES (@project_id, @organization_id, @user_session_issuer_id, @remote_session_issuer_id, @resource, sqlc.narg('remote_session_client_id'), @requested_scopes, 'ready')
RETURNING id;

-- name: ListAuthClientHeadersFixture :many
SELECT name, is_secret, value, value_from_request_header
FROM mcp_auth_client_headers
WHERE auth_client_id = @auth_client_id AND deleted IS FALSE
ORDER BY name;

-- name: GetEMAFixture :one
SELECT auth_client_id, remote_session_issuer_id, remote_session_client_id, resource, requested_scopes
FROM mcp_auth_client_emas
WHERE auth_client_id = @auth_client_id;

-- name: SoftDeleteAuthClientFixture :exec
UPDATE mcp_auth_clients SET deleted_at = clock_timestamp() WHERE id = @id;

-- name: SoftDeleteRemoteHeaderFixture :exec
UPDATE remote_mcp_server_headers SET deleted_at = clock_timestamp() WHERE id = @id;

-- name: SoftDeleteTunneledSourceFixture :exec
UPDATE tunneled_mcp_servers SET deleted_at = clock_timestamp() WHERE id = @id;

-- name: SoftDeleteRemoteSourceFixture :exec
UPDATE remote_mcp_servers SET deleted_at = clock_timestamp() WHERE id = @id;

-- name: RestoreRemoteHeaderFixture :exec
UPDATE remote_mcp_server_headers SET deleted_at = NULL, updated_at = clock_timestamp() WHERE id = @id;

-- name: ListAuthClientSummariesFixture :many
SELECT id, client_type, project_id, name, deleted, enabled
FROM mcp_auth_clients
WHERE organization_id = @organization_id
ORDER BY id;

-- name: DeleteEMABindingFixture :exec
DELETE FROM remote_session_ema_bindings WHERE id = @id;

-- name: GetEMADeletedFixture :one
SELECT deleted FROM mcp_auth_client_emas WHERE auth_client_id = @auth_client_id;


-- name: SetServerRemoteSessionIssuerFixture :exec
UPDATE mcp_servers SET remote_session_issuer_id = @remote_session_issuer_id WHERE id = @id;

-- name: UpdateRemoteHeaderFixture :exec
UPDATE remote_mcp_server_headers
SET name = @name, value = sqlc.narg('value'), value_from_request_header = sqlc.narg('value_from_request_header'), is_secret = @is_secret
WHERE id = @id;

-- name: SetRemoteHeaderRequiredFixture :exec
UPDATE remote_mcp_server_headers SET is_required = @is_required WHERE id = @id;

-- name: SetEMAStoredScopesFixture :exec
UPDATE mcp_auth_client_emas SET requested_scopes = @requested_scopes WHERE auth_client_id = @auth_client_id;

-- name: SetRequiresUpstreamAuthFixture :exec
UPDATE mcp_servers SET requires_upstream_auth = sqlc.narg('requires_upstream_auth') WHERE id = @id;

-- name: GetRequiresUpstreamAuthFixture :one
SELECT requires_upstream_auth FROM mcp_servers WHERE id = @id;

-- name: SeedToolsetFixture :exec
INSERT INTO toolsets (id, organization_id, project_id, name, slug) VALUES (@id, @organization_id, @project_id, @name, @slug);

-- name: SeedHostedMCPServerFixture :exec
INSERT INTO mcp_servers (id, project_id, slug, toolset_id, visibility) VALUES (@id, @project_id, @slug, @toolset_id, 'private');

-- name: SeedValidProtectedResourceFixture :exec
INSERT INTO remote_protected_resources (project_id, organization_id, resource_identifier, metadata_url, authorization_servers, metadata_fetched_at)
VALUES (@project_id, @organization_id, @resource_identifier, @metadata_url, sqlc.narg('authorization_servers'), clock_timestamp());

-- name: SoftDeleteIssuerOfClientFixture :exec
UPDATE remote_session_issuers SET deleted_at = clock_timestamp()
WHERE id = (SELECT remote_session_issuer_id FROM remote_session_clients WHERE remote_session_clients.id = @client_id);

-- name: ListSharedAuthClientServers :many
-- Every live server link to a shared auth client, with both ids mcp:connect
-- can be checked on: the server, and its toolset when it has one.
SELECT DISTINCT l.auth_client_id, ac.client_type, l.project_id, s.id AS mcp_server_id, s.toolset_id
FROM mcp_server_auth_clients AS l
JOIN mcp_auth_clients AS ac ON ac.id = l.auth_client_id AND ac.organization_id = l.organization_id AND ac.deleted IS FALSE
JOIN mcp_servers AS s ON s.id = l.mcp_server_id AND s.project_id = l.project_id AND s.deleted IS FALSE
JOIN projects AS p ON p.id = l.project_id AND p.organization_id = l.organization_id AND p.deleted IS FALSE
WHERE l.organization_id = @organization_id
  AND l.deleted IS FALSE
  AND ac.client_type <> 'remote_session'
ORDER BY l.auth_client_id, l.project_id;

-- name: ListServerAccessGrants :many
-- Grants that can satisfy mcp:connect, held by anyone but API keys, whose
-- policies are provisioned separately. Like grant loading, effect is ignored.
SELECT principal_urn, scope, selectors
FROM principal_grants
WHERE organization_id = @organization_id
  AND scope = ANY(@scopes::text[])
  AND principal_type <> 'api_key'
ORDER BY principal_urn, scope, id;

-- name: ListAgentRolePrincipals :many
-- Roles live agents hold, whose policies include them.
SELECT DISTINCT ara.role_urn::text AS role_urn
FROM agent_role_assignments AS ara
JOIN agents ON agents.organization_id = ara.organization_id AND agents.id = ara.agent_id AND agents.deleted IS FALSE
WHERE ara.organization_id = @organization_id
  AND ara.deleted_at IS NULL
ORDER BY role_urn;

-- name: ListLiveAgentAttachments :many
-- Unrevoked attachments of live agents to a user's connection, with what the
-- attachment runtime (GetPrincipalRemoteSessionBinding) checks before serving
-- one: the exact attached connection row is still live with its current credentials, it
-- belongs to the user who attached it, that user still owns the agent, the
-- agent and its owner are active, and the sign-in, project, upstream client
-- and issuer are live and reach the attachment's project. binding_id, when
-- set, reads that one attachment. It takes no lock: a grant is written only
-- from a read of one attachment taken after LockAttachmentIssuers through
-- LockAttachmentAuthClients hold what it decides from.
SELECT
  b.id AS binding_id,
  b.principal_id AS agent_id,
  b.remote_session_client_id,
  b.attached_by_subject_id,
  a.owner_user_id,
  COALESCE(rs.subject_urn, '')::text AS connection_subject,
  (
    a.revoked_at IS NULL AND a.suspended_at IS NULL AND a.owner_reassignment_required_at IS NULL
    AND EXISTS (
      SELECT 1
      FROM users AS owner
      JOIN organization_user_relationships AS membership ON membership.organization_id = a.organization_id
        AND membership.user_id = owner.id AND membership.deleted_at IS NULL
      WHERE owner.id = a.owner_user_id AND owner.deleted_at IS NULL
    )
  )::boolean AS agent_active,
  (
    rs.id IS NOT NULL AND rs.deleted IS FALSE
    AND rs.remote_session_client_id = b.remote_session_client_id
  )::boolean AS connection_live,
  EXISTS (
    SELECT 1
    FROM remote_session_clients AS c
    JOIN remote_session_issuers AS issuer ON issuer.id = c.remote_session_issuer_id AND issuer.deleted IS FALSE
    JOIN user_session_issuers AS usi ON usi.id = b.user_session_issuer_id AND usi.deleted IS FALSE
    JOIN user_session_issuers AS provenance ON provenance.id = rs.user_session_issuer_id AND provenance.deleted IS FALSE
    JOIN projects AS p ON p.id = b.project_id AND p.organization_id = b.organization_id AND p.deleted IS FALSE
    JOIN remote_session_client_user_session_issuers AS link ON link.remote_session_client_id = c.id AND link.user_session_issuer_id = usi.id
    WHERE c.id = b.remote_session_client_id
      AND c.deleted IS FALSE
      AND (issuer.project_id = p.id OR (issuer.project_id IS NULL AND (issuer.organization_id IS NULL OR issuer.organization_id = p.organization_id)))
      AND (provenance.project_id = p.id OR (provenance.project_id IS NULL AND provenance.organization_id = p.organization_id))
      AND (usi.project_id = p.id OR (usi.project_id IS NULL AND usi.organization_id = p.organization_id))
      AND (c.project_id = p.id OR (c.project_id IS NULL AND (c.organization_id IS NULL OR c.organization_id = p.organization_id)))
  )::boolean AS configuration_live,
  EXISTS (
    SELECT 1
    FROM mcp_auth_clients AS ac
    WHERE ac.organization_id = b.organization_id
      AND ac.remote_session_client_id = b.remote_session_client_id
      AND ac.client_type = 'remote_session'
      AND ac.deleted IS FALSE
  )::boolean AS client_recorded
FROM principal_remote_session_bindings AS b
JOIN agents AS a ON a.organization_id = b.organization_id AND a.id = b.principal_id AND a.deleted IS FALSE
LEFT JOIN remote_sessions AS rs ON rs.id = b.remote_session_id
WHERE b.organization_id = @organization_id
  AND b.revoked_at IS NULL
  AND (sqlc.narg('binding_id')::uuid IS NULL OR b.id = sqlc.narg('binding_id')::uuid)
ORDER BY b.id;

-- The attachment locks below hold, until the grants commit, every row that
-- makes an attachment servable, so none of the writers that make it
-- unservable (issuer deletion or migration, attachment revocation, agent
-- suspension, revocation or reassignment, reauthorization or revocation of
-- the connection, removal of the owner or their membership, and removal of
-- the upstream client, its issuer, the project, the sign-in binding or the
-- auth client) can commit between the read that decides a grant and the
-- grant. FOR SHARE, not FOR KEY SHARE, so they also wait on writers that only
-- update a column, such as a reauthorization's grant_generation. Every
-- candidate's owner and membership are locked first, once for the whole run,
-- then each attachment's issuers, the binding before its connection, the
-- upstream issuer before its client, as user and membership lifecycle
-- writers, issuer-first child writers, agent admission and issuer migration
-- take them. A row already gone locks nothing: the read that follows then
-- reports the attachment for remediation.

-- name: LockAttachmentOwners :exec
-- Every candidate attachment's owner, in id order, before any attachment or
-- agent lock, as user lifecycle writers lock a user before its agents.
SELECT owner.id
FROM users AS owner
WHERE owner.id = ANY(@user_ids::text[])
ORDER BY owner.id
FOR SHARE;

-- name: LockAttachmentOwnerMemberships :exec
-- Their live memberships in the organization, after the users and before any
-- attachment or agent lock, as membership removal locks the membership before
-- latching the agents.
SELECT membership.user_id
FROM organization_user_relationships AS membership
WHERE membership.organization_id = @organization_id
  AND membership.user_id = ANY(@user_ids::text[])
  AND membership.deleted_at IS NULL
ORDER BY membership.user_id
FOR SHARE;

-- name: LockAttachmentIssuers :exec
SELECT usi.id
FROM user_session_issuers AS usi
WHERE usi.id IN (
  SELECT b.user_session_issuer_id
  FROM principal_remote_session_bindings AS b
  WHERE b.id = @binding_id AND b.organization_id = @organization_id
  UNION
  SELECT rs.user_session_issuer_id
  FROM principal_remote_session_bindings AS b
  JOIN remote_sessions AS rs ON rs.id = b.remote_session_id
  WHERE b.id = @binding_id AND b.organization_id = @organization_id
)
ORDER BY usi.id
FOR SHARE;

-- name: LockAttachmentBinding :exec
SELECT b.id
FROM principal_remote_session_bindings AS b
JOIN agents AS a ON a.organization_id = b.organization_id AND a.id = b.principal_id
WHERE b.id = @binding_id AND b.organization_id = @organization_id
FOR SHARE OF b, a;

-- name: LockAttachmentSession :exec
-- The connection may be gone; then the subquery is NULL and nothing is locked.
SELECT rs.id
FROM remote_sessions AS rs
WHERE rs.id = (
  SELECT b.remote_session_id
  FROM principal_remote_session_bindings AS b
  WHERE b.id = @binding_id AND b.organization_id = @organization_id
)
FOR SHARE;

-- name: LockAttachmentUpstreamIssuer :exec
-- The upstream client's issuer, before the client itself, as issuer
-- migration takes them.
SELECT issuer.id
FROM remote_session_issuers AS issuer
WHERE issuer.id = (
  SELECT c.remote_session_issuer_id
  FROM principal_remote_session_bindings AS b
  JOIN remote_session_clients AS c ON c.id = b.remote_session_client_id
  WHERE b.id = @binding_id AND b.organization_id = @organization_id
)
FOR SHARE;

-- name: LockAttachmentConfiguration :exec
SELECT c.id
FROM principal_remote_session_bindings AS b
JOIN remote_session_clients AS c ON c.id = b.remote_session_client_id
JOIN projects AS p ON p.id = b.project_id
JOIN remote_session_client_user_session_issuers AS link
  ON link.remote_session_client_id = c.id
 AND link.user_session_issuer_id = b.user_session_issuer_id
WHERE b.id = @binding_id AND b.organization_id = @organization_id
FOR SHARE OF c, p, link;

-- name: LockAttachmentAuthClients :exec
SELECT ac.id
FROM mcp_auth_clients AS ac
WHERE ac.organization_id = @organization_id
  AND ac.client_type = 'remote_session'
  AND ac.deleted IS FALSE
  AND ac.remote_session_client_id = (
    SELECT b.remote_session_client_id
    FROM principal_remote_session_bindings AS b
    WHERE b.id = @binding_id AND b.organization_id = @organization_id
  )
ORDER BY ac.id
FOR SHARE;

-- name: ReauthorizeAttachmentConnectionFixture :exec
UPDATE remote_sessions AS rs
SET grant_generation = rs.grant_generation + 1
WHERE rs.id = (SELECT b.remote_session_id FROM principal_remote_session_bindings AS b WHERE b.id = @binding_id);

-- name: RevokeAttachmentConnectionFixture :exec
UPDATE remote_sessions AS rs
SET deleted_at = clock_timestamp()
WHERE rs.id = (SELECT b.remote_session_id FROM principal_remote_session_bindings AS b WHERE b.id = @binding_id);

-- name: DisableUserFixture :exec
UPDATE users SET deleted_at = clock_timestamp() WHERE id = @id;

-- name: ProbeUserLockFixture :one
SELECT id FROM users WHERE id = @id FOR UPDATE NOWAIT;

-- name: ProbeAttachmentBindingLockFixture :one
SELECT b.id
FROM principal_remote_session_bindings AS b
WHERE b.id = @binding_id
FOR UPDATE NOWAIT;

-- name: ListAuthClientGrantRows :many
-- Existing grants of the given scopes, to tell which defaults are missing.
SELECT principal_urn, scope, selectors
FROM principal_grants
WHERE organization_id = @organization_id
  AND scope = ANY(@scopes::text[]);

-- name: ListAPIKeyGrantRows :many
-- Every key policy row, to tell provisioned policies from explicit ones.
SELECT principal_urn, scope, selectors
FROM principal_grants
WHERE organization_id = @organization_id
  AND principal_type = 'api_key'
ORDER BY principal_urn, scope, id;

-- name: ListActiveLegacyAPIKeys :many
-- Live legacy keys, matching admission's active predicate.
SELECT id, project_id, scopes
FROM api_keys
WHERE organization_id = @organization_id
  AND deleted IS FALSE
  AND subject_urn IS NULL
  AND (expires_at IS NULL OR expires_at > clock_timestamp())
ORDER BY id;

-- name: ListLiveProjects :many
SELECT id
FROM projects
WHERE organization_id = @organization_id
  AND deleted IS FALSE
ORDER BY id;

-- name: OrganizationExistsForGrants :one
SELECT EXISTS (SELECT 1 FROM organization_metadata WHERE id = @organization_id)::boolean;

-- name: ListOrganizationIDsForGrants :many
-- Every organization, for the offline default auth permission backfill.
SELECT id
FROM organization_metadata
ORDER BY id;

-- name: ListAuthPermissionProvisions :many
-- Every grant the backfill provisioned in the organization, live or since
-- removed, and whether an admin since took it over.
SELECT principal_urn, scope, selectors, (taken_over_at IS NOT NULL)::boolean AS taken_over
FROM auth_permission_provisions
WHERE organization_id = @organization_id;

-- name: RecordAuthPermissionProvision :exec
-- Records one grant the backfill wrote.
INSERT INTO auth_permission_provisions (organization_id, principal_urn, scope, selectors, kind)
VALUES (@organization_id, @principal_urn, @scope, @selectors, @kind)
ON CONFLICT (organization_id, principal_urn, scope, selectors) DO NOTHING;

-- name: ListAuthPermissionProvisionsFixture :many
SELECT principal_urn, scope, selectors, kind, (taken_over_at IS NOT NULL)::boolean AS taken_over
FROM auth_permission_provisions
WHERE organization_id = @organization_id
ORDER BY principal_urn, scope;

-- name: SeedUpstreamSelectionFixture :exec
INSERT INTO upstream_client_selections (project_id, organization_id, credential_kind, credential_id, mcp_server_id, no_auth, source, auth_client_id, auth_client_attachment_scope)
VALUES (
    @project_id, @organization_id, 'user_session', @credential_id, @mcp_server_id, @no_auth, 'issuance',
    sqlc.narg('auth_client_id'),
    (SELECT ac.attachment_scope FROM mcp_auth_clients AS ac WHERE ac.id = sqlc.narg('auth_client_id'))
);

-- name: SeedServerAuthClientLinkFixture :exec
INSERT INTO mcp_server_auth_clients (project_id, organization_id, mcp_server_id, auth_client_id, client_attachment_scope, source)
VALUES (
    @project_id, @organization_id, @mcp_server_id, @auth_client_id,
    (SELECT ac.attachment_scope FROM mcp_auth_clients AS ac WHERE ac.id = @auth_client_id),
    @source
);

-- name: SetAuthClientEnabledFixture :exec
UPDATE mcp_auth_clients SET enabled = @enabled WHERE id = @id;

-- name: SeedAuthClientHeaderFixture :exec
INSERT INTO mcp_auth_client_headers (auth_client_id, organization_id, name, is_required, is_secret, value, value_from_request_header)
VALUES (@auth_client_id, @organization_id, @name, @is_required, @is_secret, sqlc.narg('value'), sqlc.narg('value_from_request_header'));

-- name: SeedAuthClientEMAFixture :exec
INSERT INTO mcp_auth_client_emas (auth_client_id, organization_id, remote_session_client_id, remote_session_issuer_id, resource)
SELECT ac.id, ac.organization_id, ac.remote_session_client_id, c.remote_session_issuer_id, @resource::text
FROM mcp_auth_clients AS ac
JOIN remote_session_clients AS c ON c.id = ac.remote_session_client_id
WHERE ac.id = @auth_client_id::uuid;

-- name: SwapUpstreamAuthClientFixture :exec
UPDATE upstream_client_selections
SET auth_client_id = @auth_client_id,
    auth_client_attachment_scope = (SELECT attachment_scope FROM mcp_auth_clients WHERE id = @auth_client_id)
WHERE credential_id = @credential_id AND mcp_server_id = @mcp_server_id;
-- name: SoftDeleteAttachmentUserSessionIssuerFixture :exec
UPDATE user_session_issuers SET deleted_at = clock_timestamp()
WHERE id = (SELECT user_session_issuer_id FROM principal_remote_session_bindings WHERE principal_remote_session_bindings.id = @binding_id);

-- name: SoftDeleteMCPServerFixture :exec
UPDATE mcp_servers SET deleted_at = clock_timestamp() WHERE id = @id;

-- name: SoftDeleteProjectFixture :exec
UPDATE projects SET deleted_at = clock_timestamp() WHERE id = @id;

-- name: SeedAgentAttachmentFixture :one
WITH link AS (
  INSERT INTO remote_session_client_user_session_issuers (remote_session_client_id, user_session_issuer_id)
  VALUES (@remote_session_client_id, @user_session_issuer_id)
  ON CONFLICT DO NOTHING
), session AS (
  INSERT INTO remote_sessions (subject_urn, user_session_issuer_id, remote_session_client_id, access_token_encrypted)
  VALUES (@connection_subject::text, @user_session_issuer_id, @remote_session_client_id, 'fixture-not-a-token')
  ON CONFLICT (subject_urn, remote_session_client_id) WHERE deleted IS FALSE
  DO UPDATE SET access_token_encrypted = EXCLUDED.access_token_encrypted
  RETURNING id, grant_generation, user_session_issuer_id
)
INSERT INTO principal_remote_session_bindings (project_id, organization_id, principal_id, user_session_issuer_id, remote_session_client_id, remote_session_id, grant_generation, attached_by_subject_id)
SELECT @project_id, @organization_id, @agent_id, session.user_session_issuer_id, @remote_session_client_id, session.id, session.grant_generation + @generation_offset::bigint, @attached_by::text
FROM session
RETURNING id;

-- name: SeedOAuthSelectionFixture :exec
INSERT INTO upstream_client_selections (project_id, organization_id, credential_kind, credential_id, mcp_server_id, remote_session_client_id, client_attachment_scope, no_auth, source)
SELECT @project_id, @organization_id, 'user_session', @credential_id, @mcp_server_id, c.id, c.attachment_scope, FALSE, 'issuance'
FROM remote_session_clients AS c
WHERE c.id = @remote_session_client_id::uuid;

-- name: SeedUserSessionFixture :exec
INSERT INTO user_sessions (project_id, organization_id, user_session_issuer_id, subject_urn, jti, refresh_token_hash, refresh_expires_at, expires_at, credential_id)
VALUES (@project_id, @organization_id, @user_session_issuer_id, @subject_urn, @jti, sqlc.narg('refresh_token_hash'), clock_timestamp() + interval '1 day', clock_timestamp() + interval '1 hour', @credential_id);

-- name: GetSelectionFixture :one
SELECT remote_session_client_id, auth_client_id, no_auth, source
FROM upstream_client_selections
WHERE credential_id = @credential_id AND mcp_server_id = @mcp_server_id AND deleted IS FALSE;

-- name: BindClientIssuerFixture :exec
INSERT INTO remote_session_client_user_session_issuers (remote_session_client_id, user_session_issuer_id)
VALUES (@remote_session_client_id, @user_session_issuer_id)
ON CONFLICT DO NOTHING;

-- name: UnbindClientIssuerFixture :exec
DELETE FROM remote_session_client_user_session_issuers
WHERE remote_session_client_id = @remote_session_client_id AND user_session_issuer_id = @user_session_issuer_id;

-- name: ProbeServerLockFixture :one
-- Fails with lock_not_available while anyone holds a lock on the server row.
SELECT id FROM mcp_servers WHERE id = @id FOR UPDATE NOWAIT;

-- name: SeedOrganizationUserSessionIssuerFixture :exec
INSERT INTO user_session_issuers (id, organization_id, slug, authn_challenge_mode, session_duration)
VALUES (@id, @organization_id, @slug, 'interactive', '1 day');

-- name: SeedOrganizationFeatureFixture :exec
INSERT INTO organization_features (organization_id, feature_name, deleted_at)
VALUES (@organization_id, @feature_name, sqlc.narg('deleted_at'));

-- name: CountServerSelectionsFixture :one
SELECT count(*) FROM upstream_client_selections WHERE mcp_server_id = @mcp_server_id;

-- name: ListEMAPreparationLockScope :many
-- Discover before taking registration locks, including old ownership and all
-- projects sharing this issuer's registrations. Include deleted sources and
-- servers: their old typed links and copies may still need retirement. The
-- conservative project envelope also covers a binding not yet inserted.
WITH registrations AS (
  SELECT c.id FROM remote_session_clients c
  WHERE c.remote_session_issuer_id = @issuer_id::uuid
    AND (c.project_id IN (SELECT p.id FROM projects p WHERE p.organization_id = @organization_id::text)
         OR (c.project_id IS NULL AND (c.organization_id IS NULL OR c.organization_id = @organization_id::text)))
), scope_projects AS (
  SELECT @project_id::uuid AS id
  UNION
  SELECT b.project_id FROM remote_session_ema_bindings b
  WHERE b.organization_id = @organization_id::text AND b.remote_session_issuer_id = @issuer_id::uuid
  UNION
  SELECT s.project_id FROM mcp_servers s
  JOIN mcp_server_auth_clients l ON l.mcp_server_id = s.id
  JOIN mcp_auth_clients ac ON ac.id = l.auth_client_id
  WHERE ac.organization_id = @organization_id::text AND ac.client_type = 'ema'
    AND ac.remote_session_client_id IN (SELECT id FROM registrations)
), owned_projects AS (
  SELECT p.id FROM projects p JOIN scope_projects sp ON sp.id = p.id
  WHERE p.organization_id = @organization_id::text
)
SELECT 'project'::text AS kind, p.id FROM owned_projects p
UNION ALL
SELECT 'remote'::text AS kind, r.id FROM remote_mcp_servers r WHERE r.project_id IN (SELECT id FROM owned_projects)
UNION ALL
SELECT 'tunneled'::text AS kind, t.id FROM tunneled_mcp_servers t WHERE t.project_id IN (SELECT id FROM owned_projects)
UNION ALL
SELECT 'server'::text AS kind, s.id FROM mcp_servers s WHERE s.project_id IN (SELECT id FROM owned_projects)
ORDER BY kind, id;

-- name: LockEMAPreparationServer :exec
SELECT s.id FROM mcp_servers s
JOIN projects p ON p.id = s.project_id
WHERE s.id = @server_id::uuid AND p.organization_id = @organization_id::text
FOR UPDATE OF s;

-- name: GetEMABindingLockScope :one
SELECT b.project_id, b.remote_session_issuer_id, b.remote_session_client_id
FROM remote_session_ema_bindings b
JOIN projects p ON p.id = b.project_id AND p.organization_id = b.organization_id
WHERE b.id = @binding_id::uuid AND b.organization_id = @organization_id::text;

-- name: LockEMACopyClient :exec
-- Administrator mutations lock the typed client before its links.
SELECT ac.id FROM mcp_auth_clients ac
WHERE ac.id = @auth_client_id::uuid AND ac.organization_id = @organization_id::text
FOR UPDATE OF ac;

-- name: LockEMAProjectScope :exec
-- Serializes scope discovery with creation/retargeting, including an empty
-- project. All normal preparation and source/server preflights take these
-- gates in project UUID order before acquiring any source rows.
SELECT pg_advisory_xact_lock(hashtextextended('authclient-ema-scope:' || @organization_id::text || ':' || p.id::text, 0))
FROM projects p WHERE p.id = @project_id::uuid AND p.organization_id = @organization_id::text;

-- name: LockEMAGatewayReachReader :exec
-- Hold before discovering projects or taking project/source locks. Shared
-- acquisition permits unrelated preparation and normal writers to proceed.
SELECT pg_advisory_xact_lock_shared(hashtextextended('authclient-ema-gateway-reach:' || @organization_id::text, 0));

-- name: LockEMAGatewayReachWriter :exec
-- A setting change must exclude first preparations before discovering their
-- project set, including when no gateway bindings have committed yet.
SELECT pg_advisory_xact_lock(hashtextextended('authclient-ema-gateway-reach:' || @organization_id::text, 0));

-- name: GetEMALinkSourceFixture :one
SELECT source FROM mcp_server_auth_clients
WHERE auth_client_id = @auth_client_id AND mcp_server_id = @mcp_server_id AND project_id = @project_id
ORDER BY updated_at DESC LIMIT 1;

-- name: SetEMAClientAdministratorStateFixture :exec
UPDATE mcp_auth_clients SET enabled = @enabled, source = @source
WHERE id = @id AND organization_id = @organization_id;

-- name: ClearEMABindingClientFixture :exec
UPDATE remote_session_ema_bindings SET state = 'manual_setup_required', remote_session_client_id = NULL
WHERE id = @id AND project_id = @project_id;

-- name: GetEMAClientAdministratorStateFixture :one
SELECT deleted, enabled, source FROM mcp_auth_clients
WHERE id = @id AND organization_id = @organization_id;

-- name: RetireServerStaleBackfillLinks :exec
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
  AND l.mcp_server_id = ANY(@mcp_server_ids::uuid[])
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
;


-- name: LockServerOAuthClientsForSync :exec
SELECT ac.id FROM mcp_auth_clients AS ac
WHERE ac.organization_id = @organization_id::text
  AND ac.source = 'backfill'
  AND ac.client_type IN ('remote_session', 'service_account')
  AND (ac.remote_session_client_id = ANY(@registration_ids::uuid[])
    OR EXISTS (SELECT 1 FROM mcp_server_auth_clients AS l
    WHERE l.auth_client_id = ac.id AND l.organization_id = ac.organization_id
      AND l.mcp_server_id = ANY(@mcp_server_ids::uuid[])))
ORDER BY ac.id FOR UPDATE OF ac;

-- name: LockOAuthRegistrationSyncScope :exec
-- A registration gate also covers prospective typed clients with no row yet.
-- Ordered gates precede typed client locks and every server-link mutation.
SELECT pg_advisory_xact_lock(hashtextextended('oauth-authclient-sync:' || @organization_id::text || ':' || registration_id::text, 0))
FROM unnest(@registration_ids::uuid[]) AS registration_id
ORDER BY registration_id;
