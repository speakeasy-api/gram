-- name: LockUserSessionIssuerForMetaMCP :one
-- Lock a live user session issuer in the caller's project. Meta MCP
-- create/update hold this lock while validating the reference so a concurrent
-- issuer delete cannot race the attach.
SELECT id
FROM user_session_issuers
WHERE id = @user_session_issuer_id
  AND project_id = @project_id::uuid
  AND deleted IS FALSE
FOR UPDATE;

-- name: CreateMetaMCPServer :one
INSERT INTO meta_mcp_servers (
    organization_id,
    project_id,
    name,
    user_session_issuer_id,
    visibility
)
VALUES (
    @organization_id,
    @project_id,
    @name,
    sqlc.narg('user_session_issuer_id'),
    @visibility
)
RETURNING *;

-- name: GetMetaMCPServer :one
SELECT *
FROM meta_mcp_servers
WHERE id = @id
  AND organization_id = @organization_id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: GetMetaMCPServerByIDAndProjectID :one
-- Project-scoped lookup for the public endpoint resolution path, which
-- holds an mcp_endpoints row (and so a trusted project id) but no
-- organization context.
SELECT *
FROM meta_mcp_servers
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: LockMetaMCPServer :one
SELECT *
FROM meta_mcp_servers
WHERE id = @id
  AND organization_id = @organization_id
  AND project_id = @project_id
  AND deleted IS FALSE
FOR UPDATE;

-- name: ListMetaMCPServers :many
SELECT sqlc.embed(meta_mcp_servers),
       (SELECT count(*)
        FROM meta_mcp_server_members AS mm
        WHERE mm.meta_mcp_server_id = meta_mcp_servers.id
          AND mm.deleted IS FALSE) AS member_count
FROM meta_mcp_servers
WHERE meta_mcp_servers.organization_id = @organization_id
  AND meta_mcp_servers.project_id = @project_id
  AND meta_mcp_servers.deleted IS FALSE
ORDER BY meta_mcp_servers.created_at DESC, meta_mcp_servers.id DESC;

-- name: UpdateMetaMCPServer :one
-- The service always supplies user_session_issuer_id (an omitted payload
-- issuer resolves to the preserved or freshly minted one), so the narg here
-- never arrives null from production code. A null visibility preserves the
-- stored value so callers that do not manage visibility cannot re-enable a
-- disabled gateway.
UPDATE meta_mcp_servers
SET name = @name,
    user_session_issuer_id = sqlc.narg('user_session_issuer_id'),
    visibility = COALESCE(sqlc.narg('visibility'), visibility),
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: DeleteMetaMCPServer :one
UPDATE meta_mcp_servers
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: DeleteMetaMCPMembersByMetaMCPServerID :many
-- Soft-delete all live memberships of a meta MCP server. Used when the parent
-- meta MCP is soft-deleted (FK cascades only fire on hard deletes). Returns
-- the affected rows so the caller can emit per-membership audit events.
UPDATE meta_mcp_server_members
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE meta_mcp_server_id = @meta_mcp_server_id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: DeleteMetaMCPMembersByMCPServerID :many
-- Soft-delete all live memberships that reference a generic MCP server. Used
-- when the member server is soft-deleted so meta MCPs don't keep live
-- membership rows pointing at a tombstoned server. Returns the affected rows
-- with the owning meta's name so the caller can emit per-membership audit
-- events without re-reading each meta.
UPDATE meta_mcp_server_members
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
FROM meta_mcp_servers
WHERE meta_mcp_servers.id = meta_mcp_server_members.meta_mcp_server_id
  AND meta_mcp_server_members.mcp_server_id = @mcp_server_id
  AND meta_mcp_server_members.project_id = @project_id
  AND meta_mcp_server_members.deleted IS FALSE
RETURNING meta_mcp_server_members.*, meta_mcp_servers.name AS meta_mcp_server_name;

-- name: CreateMetaMCPMember :one
INSERT INTO meta_mcp_server_members (
    project_id,
    meta_mcp_server_id,
    mcp_server_id,
    sort_order
)
VALUES (
    @project_id,
    @meta_mcp_server_id,
    @mcp_server_id,
    @sort_order
)
RETURNING *;

-- name: CountMetaMCPMembersSharingBackend :one
-- Count live members of @meta_mcp_server_id, other than @mcp_server_id, that
-- front one of the given backends. Two mcp_servers rows may name the same
-- backend, and a meta MCP server holding both would serve identical tools
-- under two slugs with nothing to route between them.
--
-- A null argument never matches: `column = NULL` evaluates to NULL, so an
-- unset backend kind cannot pair with a member's null column.
SELECT count(*)
FROM meta_mcp_server_members m
JOIN mcp_servers s
  ON s.id = m.mcp_server_id
 AND s.project_id = m.project_id
 AND s.deleted IS FALSE
WHERE m.meta_mcp_server_id = @meta_mcp_server_id
  AND m.project_id = @project_id
  AND m.deleted IS FALSE
  AND m.mcp_server_id <> @mcp_server_id
  AND (s.remote_mcp_server_id = sqlc.narg('remote_mcp_server_id')
    OR s.tunneled_mcp_server_id = sqlc.narg('tunneled_mcp_server_id')
    OR s.toolset_id = sqlc.narg('toolset_id')
    OR s.unproxied_mcp_server_id = sqlc.narg('unproxied_mcp_server_id'));

-- name: FindMetaMCPSiblingSharingBackend :one
-- Same rule as CountMetaMCPMembersSharingBackend, asked from the member
-- server's side: name a meta MCP server where @mcp_server_id already sits
-- alongside a live co-member fronting one of the given backends. Guards a
-- backend repoint on an already-attached server.
SELECT meta.name
FROM meta_mcp_server_members mine
JOIN meta_mcp_server_members sibling
  ON sibling.meta_mcp_server_id = mine.meta_mcp_server_id
 AND sibling.project_id = mine.project_id
 AND sibling.deleted IS FALSE
 AND sibling.mcp_server_id <> mine.mcp_server_id
JOIN mcp_servers s
  ON s.id = sibling.mcp_server_id
 AND s.project_id = sibling.project_id
 AND s.deleted IS FALSE
JOIN meta_mcp_servers meta
  ON meta.id = mine.meta_mcp_server_id
 AND meta.project_id = mine.project_id
 AND meta.deleted IS FALSE
WHERE mine.mcp_server_id = @mcp_server_id
  AND mine.project_id = @project_id
  AND mine.deleted IS FALSE
  AND (s.remote_mcp_server_id = sqlc.narg('remote_mcp_server_id')
    OR s.tunneled_mcp_server_id = sqlc.narg('tunneled_mcp_server_id')
    OR s.toolset_id = sqlc.narg('toolset_id')
    OR s.unproxied_mcp_server_id = sqlc.narg('unproxied_mcp_server_id'))
LIMIT 1;

-- name: GetMetaMCPMember :one
SELECT *
FROM meta_mcp_server_members
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: LockMetaMCPMember :one
SELECT *
FROM meta_mcp_server_members
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
FOR UPDATE;

-- name: ListMetaMCPMembers :many
-- List live memberships whose member server is itself still live, with the
-- member server's name and slug joined in for display.
SELECT
    m.id,
    m.mcp_server_id,
    m.sort_order,
    s.name AS mcp_server_name,
    s.slug AS mcp_server_slug
FROM meta_mcp_server_members m
JOIN mcp_servers s
  ON s.id = m.mcp_server_id
 AND s.project_id = m.project_id
 AND s.deleted IS FALSE
WHERE m.meta_mcp_server_id = @meta_mcp_server_id
  AND m.project_id = @project_id
  AND m.deleted IS FALSE
ORDER BY m.sort_order, m.created_at, m.id;

-- name: ListServableMetaMCPMembers :many
-- Serving-path variant of ListMetaMCPMembers: additionally hides members
-- whose server is disabled, matching the resolution path's rule that a
-- disabled server does not exist for unauthenticated callers, and members
-- whose server has no slug (legacy pre-2026-05 rows), which the qualified
-- serverslug--toolname contract cannot address. The dashboard listing keeps
-- the unfiltered query so admins still see every member. Carries the backend
-- and dispatch columns the gateway runtime needs to classify and execute
-- against each member, including a tunneled member's recorded resource
-- identifier so credential routing needs no second read per dial. A member
-- whose tunneled source is soft-deleted reads as no identifier and routes
-- anonymously; the dial then fails member-scoped on the missing tunnel.
SELECT
    m.id,
    m.mcp_server_id,
    m.sort_order,
    s.name AS mcp_server_name,
    s.slug AS mcp_server_slug,
    s.visibility AS mcp_server_visibility,
    s.toolset_id AS mcp_server_toolset_id,
    s.remote_mcp_server_id AS mcp_server_remote_mcp_server_id,
    s.tunneled_mcp_server_id AS mcp_server_tunneled_mcp_server_id,
    s.unproxied_mcp_server_id AS mcp_server_unproxied_mcp_server_id,
    s.environment_id AS mcp_server_environment_id,
    s.tool_variations_group_id AS mcp_server_tool_variations_group_id,
    s.remote_session_issuer_id AS mcp_server_remote_session_issuer_id,
    COALESCE(t.resource_identifier, '')::text AS tunneled_resource_identifier
FROM meta_mcp_server_members m
JOIN mcp_servers s
  ON s.id = m.mcp_server_id
 AND s.project_id = m.project_id
 AND s.deleted IS FALSE
 AND s.visibility <> 'disabled'
 AND s.slug IS NOT NULL
LEFT JOIN tunneled_mcp_servers t
  ON t.id = s.tunneled_mcp_server_id
 AND t.project_id = m.project_id
 AND t.deleted IS FALSE
WHERE m.meta_mcp_server_id = @meta_mcp_server_id
  AND m.project_id = @project_id
  AND m.deleted IS FALSE
ORDER BY m.sort_order, m.created_at, m.id;

-- name: UpdateMetaMCPMemberSortOrder :one
UPDATE meta_mcp_server_members
SET sort_order = @sort_order,
    updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: DeleteMetaMCPMember :one
UPDATE meta_mcp_server_members
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: ListMetaMCPMembersForRemoteSessionIssuer :many
-- The meta MCP's proxied (remote or tunneled) members that authenticate
-- against a given authorization server, filtered exactly as
-- ListServableMetaMCPMembers so a member invisible to the serving path cannot
-- claim a credential either.
--
-- A client names exactly one remote_session_issuer, so matching it against the
-- member's own is the whole lookup; the caller still fails closed on none or
-- several, since a grant records one resource.
--
-- upstream_url is the member's RFC 8707 resource: the remote server URL or
-- the tunneled server's recorded resource identifier (empty when a tunneled
-- member records none — the claim still lands, minting an unqualified grant).
-- Hosted and unproxied members have no upstream and cannot claim.
SELECT
    s.id AS mcp_server_id,
    s.visibility AS mcp_server_visibility,
    COALESCE(r.url, t.resource_identifier, '')::text AS upstream_url
FROM meta_mcp_server_members m
JOIN mcp_servers s
  ON s.id = m.mcp_server_id
 AND s.project_id = m.project_id
 AND s.deleted IS FALSE
 AND s.visibility <> 'disabled'
LEFT JOIN remote_mcp_servers r
  ON r.id = s.remote_mcp_server_id
 AND r.project_id = m.project_id
 AND r.deleted IS FALSE
LEFT JOIN tunneled_mcp_servers t
  ON t.id = s.tunneled_mcp_server_id
 AND t.project_id = m.project_id
 AND t.deleted IS FALSE
WHERE m.meta_mcp_server_id = @meta_mcp_server_id
  AND m.project_id = @project_id
  AND m.deleted IS FALSE
  AND s.slug IS NOT NULL
  AND (r.id IS NOT NULL OR t.id IS NOT NULL)
  AND s.remote_session_issuer_id = @remote_session_issuer_id
ORDER BY m.sort_order, m.created_at, m.id;

-- name: AutoAttachMemberProviderClient :execrows
-- Bind the member's upstream OAuth client to the gateway's issuer so consent
-- can offer the member's provider. No-op when the gateway's issuer already
-- holds a client for that upstream (one client per upstream per issuer), or
-- when the member has no derivable client. Tenancy mirrors the resync
-- derivation: the member's own project client, or an org-level client.
INSERT INTO remote_session_client_user_session_issuers (remote_session_client_id, user_session_issuer_id)
SELECT c.id, @gateway_issuer_id
FROM remote_session_clients AS c
JOIN remote_session_client_user_session_issuers AS l
  ON l.remote_session_client_id = c.id
JOIN projects AS p
  ON p.id = @project_id
WHERE l.user_session_issuer_id = @member_issuer_id
  AND c.remote_session_issuer_id = @remote_issuer_id
  AND c.deleted IS FALSE
  AND (c.project_id = @project_id
       OR (c.project_id IS NULL AND c.organization_id = p.organization_id))
  AND NOT EXISTS (
    SELECT 1
    FROM remote_session_client_user_session_issuers AS l2
    JOIN remote_session_clients AS c2
      ON c2.id = l2.remote_session_client_id
     AND c2.deleted IS FALSE
    WHERE l2.user_session_issuer_id = @gateway_issuer_id
      AND c2.remote_session_issuer_id = @remote_issuer_id
  )
ORDER BY c.created_at
LIMIT 1
ON CONFLICT DO NOTHING;

-- name: AutoDetachMemberProviderClient :execrows
-- Reverse of AutoAttachMemberProviderClient: unbind the gateway issuer's
-- client(s) for a removed member's upstream so the provider stops appearing on
-- the gateway's consent screen. The binding is scoped to the user_session_issuer,
-- so it must survive as long as ANY live consumer of that issuer still fronts
-- the upstream — a live member of a meta server on the issuer, or a server
-- directly issuer-gated to it (issuers can be shared across gateways/servers).
-- Run after the member row is soft-deleted so the just-removed member is
-- already excluded by the deleted filter below.
DELETE FROM remote_session_client_user_session_issuers AS l
USING remote_session_clients AS c
WHERE l.remote_session_client_id = c.id
  AND l.user_session_issuer_id = @gateway_issuer_id
  AND c.remote_session_issuer_id = @remote_issuer_id
  AND NOT EXISTS (
    -- All consumers of the gateway issuer live in its project (the issuer is
    -- project-scoped), so scope the scan there — both for tenancy and to keep
    -- the anti-join off a cross-tenant sequential scan.
    SELECT 1
    FROM mcp_servers AS s
    WHERE s.deleted IS FALSE
      AND s.project_id = @project_id
      AND s.remote_session_issuer_id = @remote_issuer_id
      AND (
        s.user_session_issuer_id = @gateway_issuer_id
        OR EXISTS (
          SELECT 1
          FROM meta_mcp_server_members AS m
          JOIN meta_mcp_servers AS mm
            ON mm.project_id = m.project_id
           AND mm.id = m.meta_mcp_server_id
           AND mm.deleted IS FALSE
          WHERE m.project_id = s.project_id
            AND m.mcp_server_id = s.id
            AND m.deleted IS FALSE
            AND mm.user_session_issuer_id = @gateway_issuer_id
        )
      )
  );

-- name: ListMemberProviderIdentities :many
-- Distinct provider identity pairs across a meta server's live members, for
-- re-running consent wiring when the gateway's issuer changes. Ordered so
-- callers take the per-remote-issuer binding locks deterministically.
SELECT DISTINCT s.remote_session_issuer_id, s.user_session_issuer_id
FROM meta_mcp_server_members m
JOIN mcp_servers s
  ON s.id = m.mcp_server_id
 AND s.project_id = m.project_id
 AND s.deleted IS FALSE
WHERE m.meta_mcp_server_id = @meta_mcp_server_id
  AND m.project_id = @project_id
  AND m.deleted IS FALSE
  AND s.remote_session_issuer_id IS NOT NULL
  AND s.user_session_issuer_id IS NOT NULL
ORDER BY s.remote_session_issuer_id, s.user_session_issuer_id;
