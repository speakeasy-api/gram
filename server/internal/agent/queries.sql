-- name: GetAgentPluginSet :many
-- Returns the device agent's full plugin set for an org, marketplace-first.
--
-- The base is every *published* marketplace in the org (a
-- plugin_github_connections row with a marketplace_token), so a marketplace —
-- and its always-required observability plugin, synthesized in the view layer —
-- is returned even when the user has no explicit assignments. Plugins the user
-- is assigned to (via principal_urn) are LEFT JOINed on top; projects with no
-- matching assignment still yield one row with null plugin columns.
SELECT
  pr.id AS project_id,
  pr.slug AS project_slug,
  om.slug AS organization_slug,
  om.name AS organization_name,
  pgc.marketplace_token,
  pgc.updated_at AS marketplace_updated_at,
  p.id AS plugin_id,
  p.slug AS plugin_slug,
  p.updated_at AS plugin_updated_at
FROM plugin_github_connections pgc
JOIN projects pr
  ON pr.id = pgc.project_id
  AND pr.deleted IS FALSE
JOIN organization_metadata om
  ON om.id = pr.organization_id
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
ORDER BY pr.slug, p.slug;
