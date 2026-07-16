-- name: GetAgentPluginSet :many
-- Returns the device agent's full plugin set for an org, marketplace-first.
--
-- The base is every *published* marketplace in the org (a
-- plugin_github_connections row with a marketplace_token), so a marketplace —
-- and its always-required observability plugin, synthesized in the view layer —
-- is returned even when the caller has no explicit assignments. Plugins whose
-- assignment principal_urn matches the caller's resolved principal set (email,
-- user:<id>, user:all, role:<...>, or the org wildcard) are LEFT JOINed on top;
-- projects with no matching assignment still yield one row with null plugin
-- columns.
--
-- Each project resolves to a marketplace name the way the publish path does:
-- the per-project override (project_marketplace_settings.marketplace_name) when
-- set, else the org-derived default (computed in the view). Projects with
-- distinct names surface as distinct marketplaces; projects that share a name
-- (e.g. several on the org default) still collapse to one in the view.
--
-- Rows are ordered by pr.id so the org's default project (first by id ASC, the
-- one created at org setup) sorts first. When projects share a name and the view
-- collapses them, keeping the first row's token makes that collapse resolve to
-- the default project rather than the arbitrary alphabetically-first one.
SELECT
  pr.id AS project_id,
  pr.slug AS project_slug,
  om.slug AS organization_slug,
  om.name AS organization_name,
  pgc.marketplace_token,
  pgc.updated_at AS marketplace_updated_at,
  -- The hooks subtree may be pinned by the rollout gate under a pre-rename org
  -- name; the view derives the observability slug from this snapshot so devices
  -- install the plugin that actually exists in the published repo.
  pgc.published_hooks_config,
  pms.marketplace_name AS marketplace_name_override,
  -- The org's default project (oldest, by id ASC over ALL non-deleted projects,
  -- not just published ones) keeps the bare org-derived marketplace name; others
  -- are project-scoped. Resolved the same way the publish path does so the names
  -- match. The subquery spans unpublished projects too, so an unpublished default
  -- doesn't hand the bare name to a different project.
  (pr.id = (
    -- Pinned to the @organization_id parameter (not pr.organization_id) so this
    -- stays uncorrelated and Postgres evaluates the org's default project once,
    -- not per row.
    SELECT p2.id
    FROM projects p2
    WHERE p2.organization_id = @organization_id
      AND p2.deleted IS FALSE
    ORDER BY p2.id ASC
    LIMIT 1
  )) AS is_default_project,
  p.id AS plugin_id,
  p.slug AS plugin_slug,
  p.updated_at AS plugin_updated_at
FROM plugin_github_connections pgc
JOIN projects pr
  ON pr.id = pgc.project_id
  AND pr.deleted IS FALSE
JOIN organization_metadata om
  ON om.id = pr.organization_id
LEFT JOIN project_marketplace_settings pms
  ON pms.project_id = pr.id
LEFT JOIN plugins p
  ON p.project_id = pr.id
  AND p.deleted IS FALSE
  AND EXISTS (
    SELECT 1 FROM plugin_assignments pa
    WHERE pa.plugin_id = p.id
      AND pa.principal_urn = ANY(@principal_urns::text[])
  )
WHERE pr.organization_id = @organization_id
  AND pgc.marketplace_token IS NOT NULL
ORDER BY pr.id, p.slug;

-- name: UpsertDeviceAgentSync :exec
-- Best-effort record that the device agent for @email in @organization_id polled.
-- The ON CONFLICT WHERE guard caps writes to at most once per minute per
-- (org, email) so the agent's ~60s poll cadence doesn't create write spikes,
-- mirroring how api_keys.last_accessed_at is throttled.
INSERT INTO device_agent_syncs (organization_id, email)
VALUES (@organization_id, @email)
ON CONFLICT (organization_id, email) DO UPDATE
SET last_seen_at = clock_timestamp()
  , updated_at   = clock_timestamp()
WHERE device_agent_syncs.last_seen_at < clock_timestamp() - interval '1 minute';

-- name: ListDeviceAgentSyncs :many
-- Lists every distinct email seen polling the device agent for an org, most
-- recently active first, for the dashboard's device-agent users view.
SELECT organization_id, email, first_seen_at, last_seen_at
FROM device_agent_syncs
WHERE organization_id = @organization_id
ORDER BY last_seen_at DESC;
