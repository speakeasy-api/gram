-- name: ListIntegrations :many
SELECT 
  p.id AS package_id,
  p.name AS package_name,
  p.title AS package_title,
  p.summary AS package_summary,
  p.url AS package_url,
  p.keywords AS package_keywords,
  p.image_asset_id AS package_image_asset_id,
  lv.major AS version_major,
  lv.minor AS version_minor,
  lv.patch AS version_patch,
  lv.prerelease AS version_prerelease,
  lv.build AS version_build,
  lv.created_at AS version_created_at,
  (SELECT COUNT(id) FROM http_tool_definitions WHERE http_tool_definitions.deployment_id = lv.deployment_id) as tool_count
FROM packages p
JOIN LATERAL (
    SELECT
        pv.deployment_id,
        pv.major,
        pv.minor,
        pv.patch,
        pv.prerelease,
        pv.build,
        pv.created_at
    FROM package_versions pv
    WHERE pv.package_id = p.id
        AND pv.visibility = 'public'
        AND pv.prerelease IS NULL -- Exclude prerelease versions
        AND pv.deleted IS FALSE  -- Exclude soft-deleted versions
    ORDER BY pv.major DESC, pv.minor DESC, pv.patch DESC
    LIMIT 1
) lv ON TRUE
WHERE p.deleted IS FALSE;
