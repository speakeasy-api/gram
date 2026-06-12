-- name: GetAgentPluginSet :many
-- Returns the device agent's full plugin set for an org, marketplace-first.
--
-- The base is every *published* marketplace in the org (a
-- plugin_github_connections row with a marketplace_token), so a marketplace —
-- and its always-required observability plugin, synthesized in the view layer —
-- is returned for every published project. Every non-deleted plugin in those
-- projects is LEFT JOINed on top and returned to every org member: per-principal
-- assignment scoping is intentionally disabled for now (see DNO-239) and will be
-- reinstated once RBAC-backed assignment management ships. Projects with no
-- plugins still yield one row with null plugin columns.
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
WHERE pr.organization_id = @organization_id
  AND pgc.marketplace_token IS NOT NULL
ORDER BY pr.id, p.slug;
