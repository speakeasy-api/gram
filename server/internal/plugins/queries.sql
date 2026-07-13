-- Queries for managing plugins (project-scoped distributable MCP server bundles).
-- plugins is project-scoped; every query is scoped to project_id (and organization_id where needed).

-- name: CreatePlugin :one
INSERT INTO plugins (organization_id, project_id, name, slug, description)
VALUES (@organization_id, @project_id, @name, @slug, sqlc.narg('description'))
RETURNING *;

-- name: CreateDefaultPlugin :one
-- Creates the project's fallback plugin (new servers land here absent explicit
-- routing to a named plugin). Called once, in the same transaction as project
-- creation. plugins_project_id_is_default_key enforces at most one per project.
INSERT INTO plugins (organization_id, project_id, name, slug, is_default)
VALUES (@organization_id, @project_id, 'Default', 'default', TRUE)
RETURNING *;

-- name: GetDefaultPlugin :one
-- Used by AttachToDefaultPlugin to find the fallback plugin new servers get
-- auto-attached to. No rows is expected for projects that predate the
-- Default-plugin feature; callers treat pgx.ErrNoRows as a no-op.
SELECT *
FROM plugins
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND is_default IS TRUE
  AND deleted IS FALSE;

-- name: PromoteToDefaultPlugin :one
-- Self-heals projects that already have a plugin sitting on the reserved
-- "default" slug (e.g. created manually before this feature shipped) by
-- flagging it as the project's is_default plugin. Called by
-- EnsureDefaultPlugin when CreateDefaultPlugin loses to
-- plugins_organization_id_project_id_slug_key instead of the is_default race.
UPDATE plugins
SET is_default = TRUE,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND slug = 'default'
  AND deleted IS FALSE
RETURNING *;

-- name: GetPlugin :one
SELECT *
FROM plugins
WHERE id = @id
  AND organization_id = @organization_id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: ListPlugins :many
SELECT
  p.*,
  (SELECT count(*) FROM plugin_servers ps WHERE ps.plugin_id = p.id AND ps.deleted IS FALSE) AS server_count,
  (SELECT count(*) FROM plugin_assignments pa WHERE pa.plugin_id = p.id) AS assignment_count
FROM plugins p
WHERE p.organization_id = @organization_id
  AND p.project_id = @project_id
  AND p.deleted IS FALSE
ORDER BY p.created_at DESC;

-- name: UpdatePlugin :one
UPDATE plugins
SET name = @name,
    slug = @slug,
    description = sqlc.narg('description'),
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: SoftDeletePluginServers :exec
-- Soft-deletes all servers belonging to a plugin.
UPDATE plugin_servers
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE plugin_id = @plugin_id
  AND deleted IS FALSE;

-- name: DeletePlugin :exec
UPDATE plugins
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: GetPluginServerByBackend :one
-- Look up a live plugin server by its backend (toolset or mcp_server).
-- Used by AttachToDefaultPlugin to no-op on already-attached servers before
-- inserting: a duplicate insert can trip the (plugin_id, display_name)
-- unique index rather than a backend one, and either way the failed
-- statement aborts the caller's surrounding transaction.
SELECT * FROM plugin_servers
WHERE plugin_id = @plugin_id
  AND toolset_id IS NOT DISTINCT FROM sqlc.narg('toolset_id')::uuid
  AND mcp_server_id IS NOT DISTINCT FROM sqlc.narg('mcp_server_id')::uuid
  AND deleted IS FALSE;

-- name: AddPluginServer :one
-- Inserts a plugin server backed by exactly one of a toolset or an mcp_server.
-- The plugin_servers backend-exclusivity CHECK enforces the XOR; callers must
-- supply exactly one of toolset_id / mcp_server_id.
INSERT INTO plugin_servers (plugin_id, toolset_id, mcp_server_id, display_name, policy, sort_order)
VALUES (
  @plugin_id,
  sqlc.narg('toolset_id'),
  sqlc.narg('mcp_server_id'),
  @display_name,
  @policy,
  @sort_order
)
RETURNING *;

-- name: GetMcpServerForPluginServer :one
-- Resolve an mcp_server for plugin-server validation, scoped to the project so
-- IDs alone are never trusted. has_endpoint reports whether the server has at
-- least one usable endpoint so the caller can reject unpublishable servers.
SELECT
  s.id,
  s.name,
  s.slug,
  s.visibility,
  EXISTS (
    SELECT 1 FROM mcp_endpoints e
    WHERE e.mcp_server_id = s.id AND e.deleted IS FALSE
  ) AS has_endpoint
FROM mcp_servers s
WHERE
  s.id = @mcp_server_id
  AND s.project_id = @project_id
  AND s.deleted IS FALSE;

-- name: ListPluginServers :many
SELECT *
FROM plugin_servers
WHERE plugin_id = @plugin_id
  AND deleted IS FALSE
ORDER BY sort_order ASC, created_at ASC;

-- name: ListPluginServersByPluginIDs :many
-- Batch variant of ListPluginServers for callers that need every server for
-- a set of plugins (e.g. ListPlugins) without one round-trip per plugin.
-- Joins plugins and scopes by project_id as defense-in-depth, so this stays
-- safe even if a future caller passes plugin IDs it hasn't already scoped.
SELECT plugin_servers.*
FROM plugin_servers
JOIN plugins ON plugins.id = plugin_servers.plugin_id
WHERE plugin_servers.plugin_id = ANY(@plugin_ids::uuid[])
  AND plugins.project_id = @project_id
  AND plugin_servers.deleted IS FALSE
ORDER BY plugin_servers.plugin_id, plugin_servers.sort_order ASC, plugin_servers.created_at ASC;

-- name: UpdatePluginServer :one
UPDATE plugin_servers
SET display_name = @display_name,
    policy = @policy,
    sort_order = @sort_order,
    updated_at = clock_timestamp()
WHERE id = @id
  AND plugin_id = @plugin_id
  AND deleted IS FALSE
RETURNING *;

-- name: RemovePluginServer :one
-- Soft-deletes a plugin server and returns the removed row so the caller can
-- record the correct backend (toolset vs mcp_server) in the audit log.
UPDATE plugin_servers
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id
  AND plugin_id = @plugin_id
  AND deleted IS FALSE
RETURNING *;

-- name: AddPluginAssignment :one
INSERT INTO plugin_assignments (plugin_id, organization_id, principal_urn)
VALUES (@plugin_id, @organization_id, @principal_urn)
ON CONFLICT (plugin_id, principal_urn) DO UPDATE
  SET principal_urn = EXCLUDED.principal_urn
RETURNING *;

-- name: ListPluginAssignments :many
SELECT *
FROM plugin_assignments
WHERE plugin_id = @plugin_id;

-- name: RemoveAllPluginAssignments :execrows
DELETE FROM plugin_assignments
WHERE plugin_id = @plugin_id;

-- name: ListPluginsWithServersForProject :many
-- Used during plugin generation: returns all active plugin servers joined with
-- their parent plugin and toolset mcp_slug for URL construction.
SELECT
  p.id AS plugin_id,
  p.name AS plugin_name,
  p.slug AS plugin_slug,
  p.description AS plugin_description,
  ps.id AS server_id,
  ps.display_name AS server_display_name,
  ps.policy AS server_policy,
  ps.sort_order AS server_sort_order,
  ps.toolset_id,
  t.mcp_slug AS toolset_mcp_slug,
  t.mcp_is_public AS toolset_is_public,
  (t.user_session_issuer_id IS NOT NULL)::bool AS toolset_is_oauth,
  cd.domain AS toolset_custom_domain
FROM plugins p
JOIN plugin_servers ps ON ps.plugin_id = p.id AND ps.deleted IS FALSE
JOIN toolsets t ON t.id = ps.toolset_id AND t.deleted IS FALSE AND t.mcp_enabled IS TRUE
LEFT JOIN custom_domains cd ON cd.id = t.custom_domain_id AND cd.activated IS TRUE AND cd.verified IS TRUE AND cd.deleted IS FALSE
WHERE p.project_id = @project_id
  AND p.deleted IS FALSE
ORDER BY p.slug, ps.sort_order ASC;

-- name: ListPluginsWithMcpServersForProject :many
-- Plugin-generation companion to ListPluginsWithServersForProject covering
-- mcp_server-backed (Remote MCP) plugin servers. Each server resolves to a
-- single usable endpoint via a lateral pick: custom-domain endpoints win over
-- platform endpoints, then oldest created_at, limit 1 (mirrors the collections
-- rule; per-plugin endpoint preference is a follow-up). Resolving the host
-- inside the selection keeps endpoint choice and URL-host construction in
-- lockstep, so a dangling custom-domain endpoint is never picked and emitted as
-- a (wrong) platform URL. Servers without a usable endpoint are dropped.
-- Scoped to project_id; the mcp_server must live in the same project as the
-- plugin, and disabled servers are excluded.
SELECT
  p.id AS plugin_id,
  p.name AS plugin_name,
  p.slug AS plugin_slug,
  p.description AS plugin_description,
  ps.id AS server_id,
  ps.display_name AS server_display_name,
  ps.policy AS server_policy,
  ps.sort_order AS server_sort_order,
  ps.mcp_server_id,
  ep.slug AS endpoint_slug,
  ep.custom_domain AS endpoint_custom_domain
FROM plugins p
JOIN plugin_servers ps ON ps.plugin_id = p.id AND ps.deleted IS FALSE
JOIN mcp_servers s ON s.id = ps.mcp_server_id AND s.deleted IS FALSE AND s.project_id = p.project_id AND s.visibility <> 'disabled'
LEFT JOIN LATERAL (
  SELECT e.slug, cd.domain AS custom_domain, e.created_at
  FROM mcp_endpoints e
  LEFT JOIN custom_domains cd
    ON cd.id = e.custom_domain_id
    AND cd.activated IS TRUE
    AND cd.verified IS TRUE
    AND cd.deleted IS FALSE
  WHERE e.mcp_server_id = s.id
    AND e.deleted IS FALSE
    AND (e.custom_domain_id IS NULL OR cd.id IS NOT NULL)
  ORDER BY (e.custom_domain_id IS NULL) ASC, e.created_at ASC
  LIMIT 1
) ep ON TRUE
WHERE p.project_id = @project_id
  AND p.deleted IS FALSE
  AND ep.slug IS NOT NULL
ORDER BY p.slug, ps.sort_order ASC;

-- name: GetOrganizationName :one
SELECT name FROM organization_metadata WHERE id = @id;

-- name: IsOrganizationFeatureEnabled :one
-- Reports whether an organization feature flag is enabled. Mirrors the
-- productfeatures service's read against organization_features so the generator
-- can honour org-level toggles (e.g. observability_mode) at generation time.
SELECT EXISTS (
  SELECT 1
  FROM organization_features
  WHERE organization_id = @organization_id
    AND feature_name = @feature_name
    AND deleted IS FALSE
) AS enabled;

-- name: GetGitHubConnection :one
SELECT *
FROM plugin_github_connections
WHERE project_id = @project_id;

-- name: ListPluginPublishCandidates :many
-- Lists candidates for the automated generator rollout, paginated by
-- project_id (pass the zero UUID to start): the union of (a) projects that
-- have published before (a plugin_github_connections row exists) -- the
-- original rollout population, kept so an already-connected project isn't
-- silently dropped just because it predates the Default-plugin feature and
-- has had no new attach activity since -- and (b) projects with a Default
-- plugin, published or not. (b) is the periodic safety net for the
-- best-effort initial-publish trigger fired inline by
-- CreateProject/toolsets/mcpendpoints: if that enqueue is ever lost (e.g. a
-- crash between commit and enqueue), this sweep picks it up within one tick
-- instead of leaving it stuck until a human notices. Republishing an
-- unchanged project is cheap -- SkipIfUnchanged short-circuits on the
-- fingerprint check before any GitHub/key work. Each row carries the user
-- that created the project's most recent plugins-mcp API key as the
-- publish actor, falling back to 'system' for a project that has never
-- published (no such key exists yet). This is a deliberate cross-project
-- sweep, so unlike the tenant-scoped queries it is not constrained to a
-- single project_id. The after_project_id filter is applied inside each
-- UNION branch rather than the outer query -- sqlc's analyzer can't resolve
-- an outer WHERE referencing the derived table's alias once a LATERAL join
-- follows it ("table alias does not exist").
SELECT
  cp.project_id,
  COALESCE(k.created_by_user_id, 'system') AS created_by_user_id
FROM (
  SELECT c.project_id FROM plugin_github_connections c WHERE c.project_id > @after_project_id
  UNION
  SELECT dp.project_id FROM plugins dp WHERE dp.is_default IS TRUE AND dp.deleted IS FALSE AND dp.project_id > @after_project_id
) cp
JOIN projects p ON p.id = cp.project_id AND p.deleted IS FALSE
LEFT JOIN LATERAL (
  SELECT created_by_user_id
  FROM api_keys
  WHERE project_id = cp.project_id
    AND deleted IS FALSE
    AND name LIKE 'plugins-mcp-%'
  ORDER BY created_at DESC
  LIMIT 1
) k ON TRUE
ORDER BY cp.project_id ASC
LIMIT @result_limit;

-- name: GetGitHubConnectionByMarketplaceToken :one
-- Resolves a marketplace proxy URL token to the upstream connection. The token
-- is the auth credential — there's no project scope to apply ahead of it.
SELECT *
FROM plugin_github_connections
WHERE marketplace_token = @marketplace_token;

-- name: UpsertGitHubConnection :one
-- Inserts or refreshes a project's GitHub connection. The marketplace_token
-- argument is the candidate token to use if no token is currently set; on
-- conflict the existing token is preserved via COALESCE so callers can pass a
-- freshly-generated token on every publish without overwriting prior state.
-- Token rotation goes through a separate query. published_mcp_fingerprints,
-- published_hooks_version, and published_hooks_config record the per-plugin
-- MCP content hashes, the hooks generator version, and the org-level hooks
-- settings just published; all are always overwritten so subsequent rollout
-- runs can detect independently whether the MCP or hooks component changed.
INSERT INTO plugin_github_connections (project_id, installation_id, repo_owner, repo_name, marketplace_token, published_mcp_fingerprints, published_hooks_version, published_hooks_config)
VALUES (@project_id, @installation_id, @repo_owner, @repo_name, @marketplace_token, @published_mcp_fingerprints, @published_hooks_version, @published_hooks_config)
ON CONFLICT (project_id) DO UPDATE
  SET installation_id = EXCLUDED.installation_id,
      repo_owner = EXCLUDED.repo_owner,
      repo_name = EXCLUDED.repo_name,
      marketplace_token = COALESCE(plugin_github_connections.marketplace_token, EXCLUDED.marketplace_token),
      published_mcp_fingerprints = EXCLUDED.published_mcp_fingerprints,
      published_hooks_version = EXCLUDED.published_hooks_version,
      published_hooks_config = EXCLUDED.published_hooks_config,
      updated_at = clock_timestamp()
RETURNING *;

-- name: GetGitHubConnectionOwner :one
-- Resolves which project currently owns a given installation/repo pair, and
-- whether that project has since been soft-deleted. Used to self-heal a
-- plugin_github_connections_installation_repo_key conflict on
-- UpsertGitHubConnection: a soft-deleted project's repo claim is stale (soft
-- deletes never clean up this table) and can be reclaimed by whichever
-- active project computes the same repo name next.
SELECT c.project_id, p.deleted AS project_deleted
FROM plugin_github_connections c
JOIN projects p ON p.id = c.project_id
WHERE c.installation_id = @installation_id
  AND LOWER(c.repo_owner) = LOWER(@repo_owner)
  AND LOWER(c.repo_name) = LOWER(@repo_name);

-- name: DeleteGitHubConnection :exec
-- Hard-deletes a project's GitHub connection row. plugin_github_connections
-- has no soft-delete column of its own; used only to reclaim a stale row left
-- behind by a soft-deleted project (see GetGitHubConnectionOwner) so its repo
-- slot can be reused by the project that now legitimately claims it.
DELETE FROM plugin_github_connections WHERE project_id = @project_id;

-- name: GetMarketplaceSettings :one
SELECT *
FROM project_marketplace_settings
WHERE project_id = @project_id;

-- name: GetProjectMarketplaceNameContext :one
-- Returns a project's slug and whether it's its org's default project (oldest by
-- id ASC), the two inputs needed to resolve its marketplace name — read from the
-- project row rather than trusting auth-context fields that some auth flows
-- (e.g. project-scoped API keys) leave unset.
SELECT
  pr.slug AS project_slug,
  (pr.id = (
    SELECT p2.id
    FROM projects p2
    WHERE p2.organization_id = pr.organization_id
      AND p2.deleted IS FALSE
    ORDER BY p2.id ASC
    LIMIT 1
  )) AS is_default_project
FROM projects pr
WHERE pr.id = @project_id
  AND pr.deleted IS FALSE;

-- name: UpsertMarketplaceSettings :one
-- Sets the marketplace name override for a project. Pass NULL to clear the
-- override and fall back to the server-side default.
INSERT INTO project_marketplace_settings (project_id, marketplace_name)
VALUES (@project_id, sqlc.narg('marketplace_name'))
ON CONFLICT (project_id) DO UPDATE
  SET marketplace_name = EXCLUDED.marketplace_name,
      updated_at = clock_timestamp()
RETURNING *;
