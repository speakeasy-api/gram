-- name: ListCandidates :many
-- Built-in role principals that hold stored grants in an organization but no
-- memory grant, and whose role audit history never shows a memory grant.
-- Those roles were seeded before the memory scopes existed. Any other empty
-- memory grant set is an explicit revocation and is excluded. Roles with no
-- stored grants at all are left to SeedSystemRoleGrants, which writes the
-- full canonical set.
SELECT grants.organization_id, roles.workos_slug AS role_slug, grants.principal_urn
FROM global_roles AS roles
JOIN principal_grants AS grants ON grants.principal_urn = 'role:global:' || roles.id::text
WHERE roles.workos_slug = ANY(@role_slugs::text[])
  AND roles.deleted IS FALSE
  AND roles.workos_deleted IS FALSE
  AND (sqlc.narg('organization_id')::text IS NULL OR grants.organization_id = sqlc.narg('organization_id')::text)
  AND NOT EXISTS (
    SELECT 1
    FROM principal_grants AS memory_grants
    WHERE memory_grants.organization_id = grants.organization_id
      AND memory_grants.principal_urn = grants.principal_urn
      AND memory_grants.scope LIKE 'memory:%'
  )
  AND NOT EXISTS (
    SELECT 1
    FROM audit_logs AS logs
    WHERE logs.organization_id = grants.organization_id
      AND logs.action = 'access_role:update'
      AND (
        logs.before_snapshot ->> 'PrincipalUrn' = grants.principal_urn
        OR logs.after_snapshot ->> 'PrincipalUrn' = grants.principal_urn
      )
      AND (
        jsonb_path_exists(logs.before_snapshot, '$.Grants[*] ? (@.Scope starts with "memory:")')
        OR jsonb_path_exists(logs.after_snapshot, '$.Grants[*] ? (@.Scope starts with "memory:")')
      )
  )
GROUP BY grants.organization_id, roles.workos_slug, grants.principal_urn
ORDER BY grants.organization_id, roles.workos_slug;
