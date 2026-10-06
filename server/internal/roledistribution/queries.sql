-- name: LockOrganization :exec
SELECT pg_advisory_xact_lock(hashtextextended('role-distribution-setup:' || sqlc.arg(organization_id)::text, 0));

-- name: GetSetupProject :one
SELECT p.id, o.slug AS organization_slug, p.slug AS project_slug
FROM projects p
JOIN organization_metadata o ON o.id = p.organization_id
WHERE p.organization_id = sqlc.arg(organization_id)
  AND p.deleted IS FALSE AND o.disabled_at IS NULL
ORDER BY p.created_at, p.id LIMIT 1;

-- name: LockEnabledFeature :one
SELECT id FROM organization_features
WHERE organization_id = sqlc.arg(organization_id)
  AND feature_name = 'automatic-role-distribution' AND deleted IS FALSE
FOR UPDATE;

-- name: LockActiveOrganization :one
SELECT id FROM organization_metadata
WHERE id = sqlc.arg(organization_id) AND disabled_at IS NULL
FOR SHARE;

-- name: LockGlobalRole :one
SELECT workos_name FROM global_roles
WHERE id = sqlc.arg(role_id) AND deleted IS FALSE AND workos_deleted IS FALSE
  AND 'role:global:' || id::text = sqlc.arg(role_urn)::text
FOR SHARE;

-- name: LockOrganizationRole :one
SELECT workos_name FROM organization_roles
WHERE id = sqlc.arg(role_id) AND organization_id = sqlc.arg(organization_id)
  AND deleted IS FALSE AND workos_deleted IS FALSE
  AND 'role:organization:' || id::text = sqlc.arg(role_urn)::text
FOR SHARE;

-- name: LockFirstProject :one
SELECT id FROM projects
WHERE organization_id = sqlc.arg(organization_id) AND deleted IS FALSE
ORDER BY created_at, id LIMIT 1 FOR SHARE;

-- name: LockPluginBySlug :one
SELECT id FROM plugins
WHERE organization_id = sqlc.arg(organization_id) AND project_id = sqlc.arg(project_id)
  AND deleted IS FALSE AND slug = sqlc.arg(slug)
FOR UPDATE;

-- name: CreateRolePlugin :exec
INSERT INTO plugins (id, organization_id, project_id, name, slug, auto_created)
VALUES (sqlc.arg(id), sqlc.arg(organization_id), sqlc.arg(project_id), sqlc.arg(name), sqlc.arg(slug), true);

-- name: AssignRolePlugin :exec
INSERT INTO plugin_assignments (plugin_id, organization_id, principal_urn)
SELECT p.id, p.organization_id, sqlc.arg(principal_urn)::text
FROM plugins p
WHERE p.id = sqlc.arg(plugin_id) AND p.organization_id = sqlc.arg(organization_id)
  AND p.project_id = sqlc.arg(project_id) AND p.deleted IS FALSE
ON CONFLICT (plugin_id, principal_urn) DO NOTHING;

-- name: IsGlobalRoleActive :one
SELECT EXISTS (
  SELECT 1 FROM global_roles
  WHERE id = sqlc.arg(role_id) AND deleted IS FALSE AND workos_deleted IS FALSE
);

-- name: ListActiveOrganizations :many
SELECT id FROM organization_metadata
WHERE disabled_at IS NULL AND id > sqlc.arg(cursor)::text
ORDER BY id LIMIT sqlc.arg(page_size);

-- name: IsFeatureEnabled :one
SELECT EXISTS (
  SELECT 1 FROM organization_features
  WHERE organization_id = sqlc.arg(organization_id)
    AND feature_name = 'automatic-role-distribution' AND deleted IS FALSE
);

-- name: ListActiveRoles :many
WITH active_roles AS (
  SELECT 'role:global:' || id::text AS role_urn FROM global_roles
  WHERE deleted IS FALSE AND workos_deleted IS FALSE
  UNION ALL
  SELECT 'role:organization:' || id::text FROM organization_roles
  WHERE organization_id = sqlc.arg(organization_id)
    AND deleted IS FALSE AND workos_deleted IS FALSE
)
SELECT role_urn::text FROM active_roles
WHERE role_urn > sqlc.arg(cursor)::text
ORDER BY role_urn LIMIT sqlc.arg(page_size);
