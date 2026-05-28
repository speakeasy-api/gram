-- name: GetAssignedPluginsForAgent :many
-- Resolves plugins assigned to any of the given principal URNs within an org,
-- joining the per-project marketplace token so the agent can register the
-- marketplace in Claude Code's settings. Plugins whose project has no
-- marketplace_token (the admin has not published yet) are excluded.
SELECT
  p.id AS plugin_id,
  p.slug AS plugin_slug,
  p.updated_at AS plugin_updated_at,
  p.organization_id,
  p.project_id,
  om.slug AS organization_slug,
  pr.slug AS project_slug,
  pgc.marketplace_token,
  pgc.updated_at AS marketplace_updated_at
FROM plugin_assignments pa
JOIN plugins p
  ON p.id = pa.plugin_id
  AND p.deleted IS FALSE
JOIN projects pr
  ON pr.id = p.project_id
  AND pr.deleted IS FALSE
JOIN organization_metadata om
  ON om.id = p.organization_id
JOIN plugin_github_connections pgc
  ON pgc.project_id = p.project_id
  AND pgc.marketplace_token IS NOT NULL
WHERE pa.organization_id = @organization_id
  AND pa.principal_urn = ANY(@principal_urns::text[])
ORDER BY p.project_id, p.slug;
