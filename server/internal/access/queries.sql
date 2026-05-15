-- Queries for managing principal grants (RBAC).
-- principal_grants is org-scoped (no project_id); every query is scoped to organization_id.

-- name: ListPrincipalGrantsByOrg :many
-- Returns all grant rows for an organization, optionally filtered by principal URN.
SELECT id, organization_id, principal_urn, principal_type, scope, selectors, created_at, updated_at
FROM principal_grants
WHERE organization_id = @organization_id
  AND (@principal_urn::text = '' OR principal_urn = @principal_urn)
ORDER BY principal_urn, scope;

-- name: GetPrincipalGrants :many
-- Returns all grant rows matching a set of principal URNs within an org.
-- Used by the access resolver to load grants for a user+role in a single query.
SELECT principal_urn, scope, selectors
FROM principal_grants
WHERE organization_id = @organization_id
  AND principal_urn = ANY(@principal_urns::text[]);

-- name: UpsertPrincipalGrant :one
-- Creates or updates a single grant row. On conflict (same org/principal/scope/selectors),
-- the updated_at is refreshed.
INSERT INTO principal_grants (organization_id, principal_urn, scope, selectors)
VALUES (@organization_id, @principal_urn, @scope, @selectors)
ON CONFLICT (organization_id, principal_urn, scope, selectors)
DO UPDATE SET updated_at = clock_timestamp()
RETURNING id, organization_id, principal_urn, principal_type, scope, selectors, created_at, updated_at;

-- name: DeletePrincipalGrant :execrows
-- Removes a specific grant row by ID, scoped to the organization for safety.
DELETE FROM principal_grants
WHERE id = @id
  AND organization_id = @organization_id;

-- name: DeletePrincipalGrantsByPrincipal :execrows
-- Removes all grants for a specific principal within an org.
-- Useful when removing a user from an organization.
DELETE FROM principal_grants
WHERE organization_id = @organization_id
  AND principal_urn = @principal_urn;

-- Queries for authz challenge resolutions.
-- authz_challenge_resolutions is org-scoped (no project_id).

-- name: ListChallengeResolutions :many
-- Returns resolution records for a batch of challenge IDs within an org.
SELECT * FROM authz_challenge_resolutions
WHERE organization_id = @organization_id
  AND challenge_id = ANY(@challenge_ids::text[]);

-- name: InsertChallengeResolutions :many
-- Creates resolution records for one or more denied challenges.
-- Silently skips challenges that are already resolved (ON CONFLICT DO NOTHING).
INSERT INTO authz_challenge_resolutions (
  organization_id, challenge_id, principal_urn, scope,
  resource_kind, resource_id, resolution_type, role_slug, resolved_by
)
SELECT
  @organization_id, unnest(@challenge_ids::text[]), @principal_urn, @scope,
  @resource_kind, @resource_id, @resolution_type, @role_slug, @resolved_by
ON CONFLICT (organization_id, challenge_id) DO NOTHING
RETURNING *;

-- name: GetGlobalRoleBySlug :one
SELECT *
FROM global_roles
WHERE workos_slug = @workos_slug;

-- name: UpsertGlobalRole :exec
-- Upsert an environment-level WorkOS role. Caller must have already passed
-- the row through ShouldProcessEvent. Resurrects a previously soft-deleted
-- role on conflict.
INSERT INTO global_roles (
    workos_slug,
    workos_name,
    workos_description,
    workos_created_at,
    workos_updated_at,
    workos_last_event_id
) VALUES (
    @workos_slug,
    @workos_name,
    @workos_description,
    @workos_created_at,
    @workos_updated_at,
    @workos_last_event_id
)
ON CONFLICT (workos_slug) DO UPDATE SET
    workos_name = EXCLUDED.workos_name,
    workos_description = EXCLUDED.workos_description,
    workos_updated_at = EXCLUDED.workos_updated_at,
    workos_last_event_id = EXCLUDED.workos_last_event_id,
    deleted_at = NULL,
    workos_deleted_at = NULL,
    updated_at = clock_timestamp();

-- name: MarkGlobalRoleDeleted :execrows
UPDATE global_roles
SET workos_deleted_at = @workos_deleted_at,
    workos_last_event_id = @workos_last_event_id,
    deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE workos_slug = @workos_slug
  AND deleted_at IS NULL;

-- name: GetOrganizationRoleBySlug :one
SELECT *
FROM organization_roles
WHERE organization_id = @organization_id
  AND workos_slug = @workos_slug;

-- name: UpsertOrganizationRole :exec
-- Upsert an org-scoped WorkOS role. Caller must have already passed the row
-- through ShouldProcessEvent. Resurrects a previously soft-deleted role on
-- conflict.
INSERT INTO organization_roles (
    organization_id,
    workos_slug,
    workos_name,
    workos_description,
    workos_created_at,
    workos_updated_at,
    workos_last_event_id
) VALUES (
    @organization_id,
    @workos_slug,
    @workos_name,
    @workos_description,
    @workos_created_at,
    @workos_updated_at,
    @workos_last_event_id
)
ON CONFLICT (organization_id, workos_slug) DO UPDATE SET
    workos_name = EXCLUDED.workos_name,
    workos_description = EXCLUDED.workos_description,
    workos_updated_at = EXCLUDED.workos_updated_at,
    workos_last_event_id = EXCLUDED.workos_last_event_id,
    deleted_at = NULL,
    workos_deleted_at = NULL,
    updated_at = clock_timestamp();

-- name: MarkOrganizationRoleDeleted :execrows
UPDATE organization_roles
SET workos_deleted_at = @workos_deleted_at,
    workos_last_event_id = @workos_last_event_id,
    deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND workos_slug = @workos_slug
  AND deleted_at IS NULL;

-- name: MarkOrganizationRoleDeletedLocally :execrows
UPDATE organization_roles
SET workos_last_event_id = @workos_last_event_id,
    deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND workos_slug = @workos_slug
  AND deleted_at IS NULL;

-- name: ListActiveOrganizationRoles :many
WITH active_roles AS (
  SELECT id, workos_slug, workos_name, workos_description, workos_created_at, workos_updated_at, 'global'::text AS role_kind
  FROM global_roles
  WHERE deleted IS FALSE
    AND workos_deleted IS FALSE
  UNION ALL
  SELECT id, workos_slug, workos_name, workos_description, workos_created_at, workos_updated_at, 'organization'::text AS role_kind
  FROM organization_roles
  WHERE organization_id = @organization_id
    AND deleted IS FALSE
    AND workos_deleted IS FALSE
)
SELECT
  active_roles.id,
  active_roles.workos_slug,
  active_roles.workos_name,
  active_roles.workos_description,
  active_roles.workos_created_at,
  active_roles.workos_updated_at,
  COUNT(ora.id)::bigint AS member_count
FROM active_roles
LEFT JOIN organization_role_assignments AS ora
  ON ora.organization_id = @organization_id
  AND ora.role_urn = 'role:' || active_roles.role_kind || ':' || active_roles.id::text
  AND ora.user_id IS NOT NULL
GROUP BY active_roles.id, active_roles.workos_slug, active_roles.workos_name, active_roles.workos_description, active_roles.workos_created_at, active_roles.workos_updated_at
ORDER BY active_roles.workos_slug;

-- name: GetActiveOrganizationRoleBySlug :one
SELECT
  organization_roles.id,
  organization_roles.workos_slug,
  organization_roles.workos_name,
  organization_roles.workos_description,
  organization_roles.workos_created_at,
  organization_roles.workos_updated_at,
  COUNT(ora.id)::bigint AS member_count
FROM organization_roles
LEFT JOIN organization_role_assignments AS ora
  ON ora.organization_id = organization_roles.organization_id
  AND ora.role_urn = 'role:organization:' || organization_roles.id::text
  AND ora.user_id IS NOT NULL
WHERE organization_roles.organization_id = @organization_id
  AND organization_roles.workos_slug = @workos_slug
  AND organization_roles.deleted IS FALSE
  AND organization_roles.workos_deleted IS FALSE
GROUP BY organization_roles.id, organization_roles.workos_slug, organization_roles.workos_name, organization_roles.workos_description, organization_roles.workos_created_at, organization_roles.workos_updated_at;

-- name: GetOrganizationRoleByID :one
WITH active_roles AS (
  SELECT id, workos_slug, workos_name, workos_description, workos_created_at, workos_updated_at, 'global'::text AS role_kind
  FROM global_roles
  WHERE global_roles.id = sqlc.arg(id)
    AND deleted IS FALSE
    AND workos_deleted IS FALSE
UNION ALL
  SELECT id, workos_slug, workos_name, workos_description, workos_created_at, workos_updated_at, 'organization'::text AS role_kind
  FROM organization_roles
  WHERE organization_id = @organization_id
    AND organization_roles.id = sqlc.arg(id)
    AND deleted IS FALSE
    AND workos_deleted IS FALSE
)
SELECT
  active_roles.id,
  active_roles.workos_slug,
  active_roles.workos_name,
  active_roles.workos_description,
  active_roles.workos_created_at,
  active_roles.workos_updated_at,
  COUNT(ora.id)::bigint AS member_count
FROM active_roles
LEFT JOIN organization_role_assignments AS ora
  ON ora.organization_id = @organization_id
  AND ora.role_urn = 'role:' || active_roles.role_kind || ':' || active_roles.id::text
  AND ora.user_id IS NOT NULL
GROUP BY active_roles.id, active_roles.workos_slug, active_roles.workos_name, active_roles.workos_description, active_roles.workos_created_at, active_roles.workos_updated_at
LIMIT 1;

-- name: ListOrganizationRoleAssignmentsForOrg :many
SELECT
  ora.user_id,
  ora.workos_user_id,
  ora.workos_membership_id,
  COALESCE(organization_roles.workos_slug, global_roles.workos_slug)::text AS role_slug,
  ora.created_at
FROM organization_role_assignments AS ora
LEFT JOIN organization_roles
  ON ora.role_urn = 'role:organization:' || organization_roles.id::text
  AND organization_roles.organization_id = ora.organization_id
  AND organization_roles.deleted IS FALSE
  AND organization_roles.workos_deleted IS FALSE
LEFT JOIN global_roles
  ON ora.role_urn = 'role:global:' || global_roles.id::text
  AND global_roles.deleted IS FALSE
  AND global_roles.workos_deleted IS FALSE
WHERE ora.organization_id = @organization_id
  AND COALESCE(organization_roles.workos_slug, global_roles.workos_slug) IS NOT NULL
ORDER BY ora.workos_user_id, role_slug;

-- name: ListMemberRoleSlugsByWorkosUser :many
SELECT DISTINCT COALESCE(organization_roles.workos_slug, global_roles.workos_slug)::text AS role_slug
FROM organization_role_assignments AS ora
LEFT JOIN organization_roles
  ON ora.role_urn = 'role:organization:' || organization_roles.id::text
  AND organization_roles.organization_id = ora.organization_id
  AND organization_roles.deleted IS FALSE
  AND organization_roles.workos_deleted IS FALSE
LEFT JOIN global_roles
  ON ora.role_urn = 'role:global:' || global_roles.id::text
  AND global_roles.deleted IS FALSE
  AND global_roles.workos_deleted IS FALSE
WHERE ora.organization_id = @organization_id
  AND ora.workos_user_id = @workos_user_id
  AND COALESCE(organization_roles.workos_slug, global_roles.workos_slug) IS NOT NULL
ORDER BY role_slug;

-- name: ReplaceOrganizationRoleAssignment :one
WITH input_role_urn AS (
  SELECT 'role:organization:' || id::text AS role_urn
  FROM organization_roles
  WHERE organization_roles.organization_id = @organization_id
    AND organization_roles.workos_slug = sqlc.arg(workos_role_slug)
    AND organization_roles.deleted IS FALSE
    AND organization_roles.workos_deleted IS FALSE
  UNION ALL
  SELECT 'role:global:' || id::text AS role_urn
  FROM global_roles
  WHERE global_roles.workos_slug = sqlc.arg(workos_role_slug)
    AND global_roles.deleted IS FALSE
    AND global_roles.workos_deleted IS FALSE
),
upserted AS (
  INSERT INTO organization_role_assignments (
    organization_id,
    workos_user_id,
    user_id,
    role_urn,
    workos_membership_id,
    workos_updated_at,
    workos_last_event_id
  )
  SELECT
    @organization_id,
    @workos_user_id,
    @user_id,
    input_role_urn.role_urn,
    @workos_membership_id,
    @workos_updated_at,
    @workos_last_event_id
  FROM input_role_urn
  ON CONFLICT (organization_id, workos_user_id, role_urn) DO UPDATE SET
    user_id = COALESCE(EXCLUDED.user_id, organization_role_assignments.user_id),
    workos_membership_id = EXCLUDED.workos_membership_id,
    workos_updated_at = EXCLUDED.workos_updated_at,
    workos_last_event_id = EXCLUDED.workos_last_event_id,
    updated_at = clock_timestamp()
  RETURNING role_urn
),
deleted AS (
DELETE FROM organization_role_assignments
WHERE organization_role_assignments.organization_id = @organization_id
  AND organization_role_assignments.workos_user_id = @workos_user_id
  AND EXISTS (SELECT 1 FROM upserted)
  AND organization_role_assignments.role_urn NOT IN (SELECT role_urn FROM upserted)
  RETURNING 1
)
SELECT COUNT(*)::bigint FROM upserted;
