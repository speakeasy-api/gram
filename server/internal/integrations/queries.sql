-- name: ListIntegrations :many
WITH latest_versions AS (
    -- Select package versions that are not prereleases versions,
    -- and assign a rank within each package based on version number (highest first).
    SELECT
        pv.package_id,
        pv.deployment_id,
        pv.major,
        pv.minor,
        pv.patch,
        pv.prerelease,
        pv.build,
        pv.created_at,
        ROW_NUMBER() OVER(PARTITION BY pv.package_id ORDER BY pv.major DESC, pv.minor DESC, pv.patch DESC) as rn
    FROM
        package_versions pv
    WHERE
        pv.visibility = 'public'
        AND pv.prerelease IS NULL -- Exclude prerelease versions
        AND pv.deleted IS FALSE  -- Exclude soft-deleted versions
)
SELECT 
  p.id AS package_id,
  p.name AS package_name,
  p.title AS package_title,
  p.summary AS package_summary,
  p.keywords AS package_keywords,
  lv.major AS version_major,
  lv.minor AS version_minor,
  lv.patch AS version_patch,
  lv.prerelease AS version_prerelease,
  lv.build AS version_build,
  lv.created_at AS version_created_at,
  (SELECT COUNT(*) FROM http_tool_definitions WHERE http_tool_definitions.deployment_id = lv.deployment_id) as tool_count
FROM packages p
JOIN latest_versions lv ON p.id = lv.package_id
WHERE p.deleted IS FALSE;
