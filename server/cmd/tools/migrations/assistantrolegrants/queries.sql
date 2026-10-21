-- name: ListCandidates :many
-- Built-in role principals that hold stored grants in an organization but no
-- assistant-kind grant, and whose role audit history never shows an assistant
-- grant. Those roles were seeded before the assistant scopes existed. Any
-- other empty assistant grant set is an explicit revocation and is excluded.
-- Roles with no stored grants at all are left to SeedSystemRoleGrants, which
-- writes the full canonical set.
SELECT grants.organization_id, roles.workos_slug AS role_slug, grants.principal_urn
FROM global_roles AS roles
JOIN principal_grants AS grants ON grants.principal_urn = 'role:global:' || roles.id::text
WHERE roles.workos_slug = ANY(@role_slugs::text[])
  AND roles.deleted IS FALSE
  AND roles.workos_deleted IS FALSE
  AND (sqlc.narg('organization_id')::text IS NULL OR grants.organization_id = sqlc.narg('organization_id')::text)
  AND NOT EXISTS (
    SELECT 1
    FROM principal_grants AS assistant_grants
    WHERE assistant_grants.organization_id = grants.organization_id
      AND assistant_grants.principal_urn = grants.principal_urn
      AND assistant_grants.scope LIKE 'assistant:%'
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
        jsonb_path_exists(logs.before_snapshot, '$.Grants[*] ? (@.Scope starts with "assistant:")')
        OR jsonb_path_exists(logs.after_snapshot, '$.Grants[*] ? (@.Scope starts with "assistant:")')
      )
  )
GROUP BY grants.organization_id, roles.workos_slug, grants.principal_urn
ORDER BY grants.organization_id, roles.workos_slug;

-- name: LockCandidate :exec
-- Serializes concurrent runs of this backfill for one role principal.
SELECT pg_advisory_xact_lock(hashtextextended('assistant-role-grants:' || @organization_id::text || ':' || @principal_urn::text, 0));
