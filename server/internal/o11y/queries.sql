-- name: StatOrganizationsCount :one
SELECT COUNT(*) as count
FROM organization_metadata;


-- name: StatProjectsCount :one
SELECT COUNT(*) as count
FROM projects
WHERE deleted = FALSE;

-- name: StatOrganizationMembershipsCount :one
SELECT COUNT(*) as count
FROM organization_user_relationships
WHERE deleted IS FALSE;

-- name: StatOrganizationMembershipsMissingWorkosMembershipCount :one
SELECT COUNT(*) as count
FROM organization_user_relationships
WHERE deleted IS FALSE
  AND workos_membership_id IS NULL;

-- name: StatHTTPSecuritySchemes :many
WITH latest_deployments AS (
  SELECT DISTINCT ON (project_id) 
    id,
    project_id,
    seq
  FROM deployments
  ORDER BY project_id, seq DESC
)
SELECT 
  hs.type,
  hs.scheme,
  COUNT(*) as count
FROM latest_deployments ld
JOIN http_security hs ON hs.deployment_id = ld.id
WHERE hs.deleted = FALSE
GROUP BY hs.type, hs.scheme
ORDER BY hs.type, hs.scheme;