-- name: ListCandidateToolsets :many
SELECT id, project_id
FROM toolsets
WHERE deleted IS FALSE
  AND mcp_slug IS NOT NULL
  AND mcp_slug <> ''
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id')::uuid)
  AND id > @after_id::uuid
ORDER BY id
LIMIT @page_size::int;

-- name: LockToolsetBackfill :exec
SELECT pg_advisory_xact_lock(hashtextextended('hosted-mcp-backfill:' || @toolset_id::text, 0));

-- name: GetServerIdentity :one
-- Any state: a tombstoned row still owns its id.
SELECT id, toolset_id, deleted
FROM mcp_servers
WHERE id = @id AND project_id = @project_id;

-- name: CountFreshIDServers :one
SELECT count(*)
FROM mcp_servers
WHERE project_id = @project_id
  AND toolset_id = @toolset_id::uuid
  AND id <> @toolset_id::uuid
  AND deleted IS FALSE;

-- name: LiveCustomDomainExists :one
SELECT EXISTS (
  SELECT 1
  FROM custom_domains
  WHERE id = @id AND organization_id = @organization_id AND deleted IS FALSE
);

-- name: EndpointAddressHeldElsewhere :one
-- Addresses are a global namespace, mirroring the partial unique indexes, so
-- this probe is deliberately not project-scoped.
SELECT EXISTS (
  SELECT 1
  FROM mcp_endpoints
  WHERE slug = @slug
    AND custom_domain_id IS NOT DISTINCT FROM sqlc.narg('custom_domain_id')::uuid
    AND deleted IS FALSE
    AND mcp_server_id IS DISTINCT FROM @mcp_server_id::uuid
);

-- name: ServerSlugHeldElsewhere :one
SELECT EXISTS (
  SELECT 1
  FROM mcp_servers
  WHERE project_id = @project_id
    AND slug = @slug
    AND deleted IS FALSE
    AND id <> @id
);

-- TEST FIXTURE ONLY: every query below is for package tests and may create impossible states.

-- name: SeedOrganizationFixture :exec
INSERT INTO organization_metadata (id, name, slug)
VALUES (@id, @name, @slug);

-- name: SeedProjectFixture :exec
INSERT INTO projects (id, name, slug, organization_id)
VALUES (@id, @name, @slug, @organization_id);

-- name: SeedCustomDomainFixture :exec
INSERT INTO custom_domains (id, organization_id, domain, verified, activated, deleted_at)
VALUES (@id, @organization_id, @domain, TRUE, TRUE, @deleted_at);

-- name: SeedToolsetFixture :exec
INSERT INTO toolsets (
  id, organization_id, project_id, name, slug, mcp_slug, mcp_is_public, mcp_enabled, custom_domain_id
) VALUES (
  @id, @organization_id, @project_id, @name, @slug, @mcp_slug, @mcp_is_public, @mcp_enabled, @custom_domain_id
);

-- name: SeedServerFixture :exec
INSERT INTO mcp_servers (id, project_id, name, slug, toolset_id, visibility)
VALUES (@id, @project_id, @name, @slug, @toolset_id, @visibility);

-- name: SeedEndpointFixture :exec
INSERT INTO mcp_endpoints (id, project_id, custom_domain_id, mcp_server_id, slug)
VALUES (@id, @project_id, @custom_domain_id, @mcp_server_id, @slug);

-- name: ListServersFixture :many
SELECT *
FROM mcp_servers
WHERE project_id = @project_id
ORDER BY id;

-- name: ListEndpointsFixture :many
SELECT *
FROM mcp_endpoints
WHERE project_id = @project_id
ORDER BY id;

-- name: ListAuditActorsFixture :many
SELECT actor_id, actor_type, action
FROM audit_logs
WHERE organization_id = @organization_id
ORDER BY seq;
