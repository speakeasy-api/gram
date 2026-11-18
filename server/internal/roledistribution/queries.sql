-- name: LockOrganizationRoleDistribution :exec
-- Serializes setup attempts and bounded expansion for one organization.
SELECT pg_advisory_xact_lock(hashtextextended('role-distribution-setup:' || @organization_id::text, 0));

-- name: GlobalRoleIsActive :one
SELECT EXISTS (
  SELECT 1 FROM global_roles
  WHERE id = @id AND deleted IS FALSE AND workos_deleted IS FALSE
);

-- name: ListActiveOrganizationsPage :many
SELECT id
FROM organization_metadata
WHERE disabled_at IS NULL
  AND id > @cursor
ORDER BY id
LIMIT @page_limit;

-- name: LockActiveOrganization :one
SELECT id
FROM organization_metadata
WHERE id = @id
  AND disabled_at IS NULL
FOR SHARE;

-- name: ListActiveOrganizationRoleURNsPage :many
WITH active_roles AS (
  SELECT 'role:global:' || id::text AS role_urn
  FROM global_roles
  WHERE deleted IS FALSE AND workos_deleted IS FALSE
  UNION ALL
  SELECT 'role:organization:' || id::text
  FROM organization_roles
  WHERE organization_id = @organization_id AND deleted IS FALSE AND workos_deleted IS FALSE
)
SELECT role_urn::text AS role_urn
FROM active_roles
WHERE role_urn > @cursor::text
ORDER BY role_urn
LIMIT @page_limit;

-- name: GetRoleSetupProject :one
-- The organization's oldest live project, when the organization is enabled.
SELECT p.id
FROM projects p
JOIN organization_metadata o ON o.id = p.organization_id
WHERE p.organization_id = @organization_id
  AND p.deleted IS FALSE
  AND o.disabled_at IS NULL
ORDER BY p.created_at, p.id
LIMIT 1;

-- name: LockLiveGlobalRoleName :one
SELECT workos_name
FROM global_roles
WHERE id = @id
  AND deleted IS FALSE
  AND workos_deleted IS FALSE
  AND 'role:global:' || id::text = @role_urn::text
FOR SHARE;

-- name: LockLiveOrganizationRoleName :one
SELECT workos_name
FROM organization_roles
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE
  AND workos_deleted IS FALSE
  AND 'role:organization:' || id::text = @role_urn::text
FOR SHARE;

-- name: LockOldestLiveProject :one
SELECT id
FROM projects
WHERE organization_id = @organization_id
  AND deleted IS FALSE
ORDER BY created_at, id
LIMIT 1
FOR SHARE;

-- name: LockLivePluginBySlug :one
-- Assignment writers lock the parent plugin FOR UPDATE; reusing that lock
-- serializes with concurrent admin edits and deletion.
SELECT id
FROM plugins
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND deleted IS FALSE
  AND slug = @slug
FOR UPDATE;

-- name: CreateRolePlugin :exec
INSERT INTO plugins (id, organization_id, project_id, name, slug, auto_created)
VALUES (@id, @organization_id, @project_id, @name, @slug, true);

-- name: AddRolePluginAssignment :exec
INSERT INTO plugin_assignments (plugin_id, organization_id, principal_urn)
VALUES (@plugin_id, @organization_id, @principal_urn)
ON CONFLICT (plugin_id, principal_urn) DO NOTHING;
