-- Queries for managing principal grants (RBAC).
-- principal_grants is org-scoped (no project_id); every query is scoped to organization_id.

-- name: ListPrincipalGrantsByOrg :many
-- Returns all grant rows for an organization, optionally filtered by principal URN.
SELECT id, organization_id, principal_urn, principal_type, scope, selectors, created_at, updated_at
FROM principal_grants
WHERE organization_id = @organization_id
  AND COALESCE(effect, 'allow') = 'allow'
  AND (@principal_urn::text = '' OR principal_urn = @principal_urn)
ORDER BY principal_urn, scope;

-- name: GetPrincipalGrants :many
-- Returns all grant rows matching a set of principal URNs within an org.
-- Used by the access resolver to load grants for a user+role in a single query.
SELECT principal_urn, scope, selectors
FROM principal_grants
WHERE organization_id = @organization_id
  AND COALESCE(effect, 'allow') = 'allow'
  AND principal_urn = ANY(@principal_urns::text[]);

-- name: ListPrincipalGrantsByResource :many
-- Returns grant rows for a single resource selector.
SELECT principal_urn, scope, selectors
FROM principal_grants
WHERE organization_id = @organization_id
  AND COALESCE(effect, 'allow') = 'allow'
  AND scope = @scope
  AND selectors @> jsonb_build_object(
    'resource_kind', sqlc.arg(resource_kind)::text,
    'resource_id', sqlc.arg(resource_id)::text
  )
ORDER BY principal_urn;

-- name: ListPrincipalGrantsByResourceIDs :many
-- Returns grant rows for a set of resources under one scope in an org. Batched
-- form of ListPrincipalGrantsByResource that stays scoped to the caller's
-- resource ids, so listing one project's resources never loads the whole org.
-- Callers group the results by the selector's resource_id.
SELECT principal_urn, scope, selectors
FROM principal_grants
WHERE organization_id = @organization_id
  AND COALESCE(effect, 'allow') = 'allow'
  AND scope = @scope
  AND selectors @> jsonb_build_object(
    'resource_kind', sqlc.arg(resource_kind)::text
  )
  AND selectors->>'resource_id' = ANY(@resource_ids::text[])
ORDER BY principal_urn;

-- name: UpsertPrincipalGrant :one
-- Creates or updates a single grant row. On conflict (same org/principal/scope/selectors),
-- any legacy effect is normalized to the allow-only NULL representation.
INSERT INTO principal_grants (organization_id, principal_urn, scope, selectors)
VALUES (@organization_id, @principal_urn, @scope, @selectors)
ON CONFLICT (organization_id, principal_urn, scope, selectors)
DO UPDATE SET
  effect = NULL,
  updated_at = clock_timestamp()
RETURNING id, organization_id, principal_urn, principal_type, scope, selectors, created_at, updated_at;

-- name: InsertPrincipalGrantIfAbsent :execrows
-- Creates a single grant row, leaves existing allow rows untouched, and converts
-- a conflicting legacy effect row to the allow-only NULL representation.
INSERT INTO principal_grants (organization_id, principal_urn, scope, selectors)
VALUES (@organization_id, @principal_urn, @scope, @selectors)
ON CONFLICT (organization_id, principal_urn, scope, selectors)
DO UPDATE SET
  effect = NULL,
  updated_at = clock_timestamp()
WHERE principal_grants.effect IS NOT NULL;

-- name: DeletePrincipalGrant :execrows
-- Removes a specific grant row by ID, scoped to the organization for safety.
DELETE FROM principal_grants
WHERE id = @id
  AND organization_id = @organization_id;

-- name: DeletePrincipalGrantByIdentity :execrows
-- Removes a specific grant row by principal, scope, and selector.
DELETE FROM principal_grants
WHERE organization_id = @organization_id
  AND principal_urn = @principal_urn
  AND scope = @scope
  AND selectors = @selectors;

-- name: DeletePrincipalGrantsByTarget :execrows
-- Removes every principal row for one exact grant target. Used by audience
-- replacement writes where the caller supplies the full desired principal set.
DELETE FROM principal_grants
WHERE organization_id = @organization_id
  AND scope = @scope
  AND selectors = @selectors;

-- name: DeletePrincipalGrantsByResource :execrows
-- Removes grant rows for a single resource selector.
DELETE FROM principal_grants
WHERE organization_id = @organization_id
  AND scope = @scope
  AND selectors @> jsonb_build_object(
    'resource_kind', sqlc.arg(resource_kind)::text,
    'resource_id', sqlc.arg(resource_id)::text
  );

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

-- name: ListRetainedResolvedChallengeIDs :many
-- Resolutions cannot predate their challenge, so records older than ClickHouse's
-- 90-day challenge retention cannot match a retained bucket.
SELECT challenge_id FROM authz_challenge_resolutions
WHERE organization_id = @organization_id
  AND created_at >= CURRENT_TIMESTAMP - INTERVAL '90 days';

-- name: LockChallengeResolutions :exec
-- One organization-scoped lock serializes challenge resolution without consuming
-- one shared lock entry per challenge in large buckets. Resolution traffic is rare,
-- and this keeps concurrent batches bounded and deterministic.
SELECT pg_advisory_xact_lock(hashtextextended(
  jsonb_build_array('access.challenge-resolution', @organization_id::text)::text, 0
));

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

-- name: ListGlobalRoles :many
SELECT *
FROM global_roles
WHERE deleted_at IS NULL
ORDER BY workos_slug;

-- name: UpsertGlobalRole :exec
-- Upsert an environment-level role. WorkOS sync callers pass an event ID;
-- local/bootstrap callers pass NULL so an existing WorkOS event cursor is preserved.
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
    workos_last_event_id = COALESCE(EXCLUDED.workos_last_event_id, global_roles.workos_last_event_id),
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

-- name: ListOrganizationRolesByOrg :many
SELECT *
FROM organization_roles
WHERE organization_id = @organization_id
  AND deleted_at IS NULL
ORDER BY workos_slug;

-- name: CreateOrganizationRole :one
-- Creates an org-scoped role, reactivating a soft-deleted row for the same slug.
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
    workos_created_at = EXCLUDED.workos_created_at,
    workos_updated_at = EXCLUDED.workos_updated_at,
    deleted_at = NULL,
    workos_deleted_at = NULL,
    updated_at = clock_timestamp()
WHERE organization_roles.deleted_at IS NOT NULL
RETURNING
    id,
    ('role:organization:' || id::text)::text AS role_urn,
    workos_slug,
    workos_name,
    workos_description,
    workos_created_at,
    workos_updated_at,
    0::bigint AS member_count;

-- name: UpsertOrganizationRole :one
-- Upsert an org-scoped role. WorkOS sync callers pass an event ID; local role
-- lifecycle callers pass NULL so an existing WorkOS event cursor is preserved.
WITH upserted AS (
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
    workos_last_event_id = COALESCE(EXCLUDED.workos_last_event_id, organization_roles.workos_last_event_id),
    deleted_at = NULL,
    workos_deleted_at = NULL,
    updated_at = clock_timestamp()
RETURNING
    id,
    organization_id,
    workos_slug,
    workos_name,
    workos_description,
    workos_created_at,
    workos_updated_at
)
SELECT
  upserted.id,
  ('role:organization:' || upserted.id::text)::text AS role_urn,
  upserted.workos_slug,
  upserted.workos_name,
  upserted.workos_description,
  upserted.workos_created_at,
  upserted.workos_updated_at,
  COUNT(ora.id)::bigint AS member_count
FROM upserted
LEFT JOIN organization_role_assignments AS ora
  ON ora.organization_id = upserted.organization_id
  AND ora.role_urn = 'role:organization:' || upserted.id::text
  AND ora.user_id IS NOT NULL
  AND ora.deleted_at IS NULL
GROUP BY upserted.id, upserted.workos_slug, upserted.workos_name, upserted.workos_description, upserted.workos_created_at, upserted.workos_updated_at;

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
SET deleted_at = clock_timestamp(),
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
  ('role:' || active_roles.role_kind || ':' || active_roles.id::text)::text AS role_urn,
  active_roles.workos_slug,
  active_roles.workos_name,
  active_roles.workos_description,
  active_roles.workos_created_at,
  active_roles.workos_updated_at,
  COUNT(DISTINCT ora.id)::bigint AS member_count
FROM active_roles
LEFT JOIN organization_role_assignments AS ora
  ON ora.organization_id = @organization_id
  AND ora.role_urn = 'role:' || active_roles.role_kind || ':' || active_roles.id::text
  AND ora.user_id IS NOT NULL
  AND ora.deleted_at IS NULL
GROUP BY active_roles.id, active_roles.role_kind, active_roles.workos_slug, active_roles.workos_name, active_roles.workos_description, active_roles.workos_created_at, active_roles.workos_updated_at
ORDER BY active_roles.workos_slug;

-- name: GetActiveOrganizationRoleBySlug :one
WITH active_roles AS (
  SELECT id, workos_slug, workos_name, workos_description, workos_created_at, workos_updated_at, 'global'::text AS role_kind
  FROM global_roles
  WHERE global_roles.workos_slug = @workos_slug
    AND deleted IS FALSE
    AND workos_deleted IS FALSE
  UNION ALL
  SELECT id, workos_slug, workos_name, workos_description, workos_created_at, workos_updated_at, 'organization'::text AS role_kind
  FROM organization_roles
  WHERE organization_id = @organization_id
    AND organization_roles.workos_slug = @workos_slug
    AND deleted IS FALSE
    AND workos_deleted IS FALSE
)
SELECT
  active_roles.id,
  ('role:' || active_roles.role_kind || ':' || active_roles.id::text)::text AS role_urn,
  active_roles.workos_slug,
  active_roles.workos_name,
  active_roles.workos_description,
  active_roles.workos_created_at,
  active_roles.workos_updated_at,
  COUNT(DISTINCT ora.id)::bigint AS member_count
FROM active_roles
LEFT JOIN organization_role_assignments AS ora
  ON ora.organization_id = @organization_id
  AND ora.role_urn = 'role:' || active_roles.role_kind || ':' || active_roles.id::text
  AND ora.user_id IS NOT NULL
  AND ora.deleted_at IS NULL
GROUP BY active_roles.id, active_roles.role_kind, active_roles.workos_slug, active_roles.workos_name, active_roles.workos_description, active_roles.workos_created_at, active_roles.workos_updated_at
-- Organization roles shadow global roles if a slug ever collides.
ORDER BY active_roles.role_kind DESC
LIMIT 1;

-- name: LockOrganizationRoleByID :one
-- Platform mutations call this inside their receipt transaction before checking
-- an optimistic role version. Only custom organization roles are eligible.
SELECT id
FROM organization_roles
WHERE organization_id = @organization_id
  AND id = sqlc.arg(id)
  AND deleted IS FALSE
  AND workos_deleted IS FALSE
FOR UPDATE;

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
  ('role:' || active_roles.role_kind || ':' || active_roles.id::text)::text AS role_urn,
  active_roles.workos_slug,
  active_roles.workos_name,
  active_roles.workos_description,
  active_roles.workos_created_at,
  active_roles.workos_updated_at,
  COUNT(DISTINCT ora.id)::bigint AS member_count
FROM active_roles
LEFT JOIN organization_role_assignments AS ora
  ON ora.organization_id = @organization_id
  AND ora.role_urn = 'role:' || active_roles.role_kind || ':' || active_roles.id::text
  AND ora.user_id IS NOT NULL
  AND ora.deleted_at IS NULL
GROUP BY active_roles.id, active_roles.role_kind, active_roles.workos_slug, active_roles.workos_name, active_roles.workos_description, active_roles.workos_created_at, active_roles.workos_updated_at
-- Organization roles shadow global roles if an ID ever collides.
ORDER BY active_roles.role_kind DESC
LIMIT 1;

-- name: ListOrganizationRoleAssignmentsBySlug :many
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
  AND COALESCE(organization_roles.workos_slug, global_roles.workos_slug) = @workos_role_slug
  AND ora.deleted_at IS NULL
ORDER BY ora.workos_user_id;

-- name: LockOrganizationUserRelationship :one
-- Serializes AddMemberRoleTx, UpdateMemberRoles, and connected-member sends.
-- Lock both rows so membership deletion and user soft-deletion cannot race the
-- active-state check. User deletion and membership deletion each need one of
-- these rows and therefore wait until this role mutation commits.
-- Provider event ingestion does not participate; not a lock for all writers.
SELECT our.id
FROM organization_user_relationships AS our
JOIN users ON users.id = our.user_id
WHERE our.organization_id = @organization_id
  AND our.user_id = sqlc.arg(user_id)::text
  AND our.deleted IS FALSE
  AND users.deleted_at IS NULL
FOR UPDATE OF users, our;

-- name: LockMemberRoleSync :exec
-- Cross-process serialization of RoleManager sends, including legacy unlinked members.
SELECT pg_advisory_xact_lock(hashtextextended(
  jsonb_build_array('access.member-role-sync', sqlc.arg(organization_id)::text, sqlc.arg(workos_user_id)::text)::text, 0
));

-- name: LockMemberRoleSyncRelationship :one
-- Keep a connected member's desired roles stable across the read and provider send.
-- Deleted relationships are returned so they cannot fall back to legacy assignments.
SELECT our.user_id, our.workos_membership_id, our.deleted,
  (users.deleted_at IS NOT NULL)::boolean AS user_deleted
FROM organization_user_relationships AS our
JOIN users ON users.id = our.user_id
WHERE our.organization_id = @organization_id
  AND users.workos_id = sqlc.arg(workos_user_id)::text
FOR UPDATE OF our;

-- name: RepairOrganizationRoleAssignmentUserLink :execrows
-- Repair only linkage; preserve roles and provider event/version metadata.
UPDATE organization_role_assignments
SET user_id = sqlc.arg(user_id)::text, updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND workos_user_id = @workos_user_id
  AND role_urn = sqlc.arg(role_urn)::text
  AND user_id IS NULL
  AND deleted_at IS NULL;

-- name: GetOrganizationRoleAssignmentByWorkosUser :one
SELECT
  our.user_id,
  users.workos_id::text AS workos_user_id,
  our.workos_membership_id,
  COALESCE(organization_roles.id::text, global_roles.id::text, '')::text AS role_id,
  COALESCE(organization_roles.workos_slug, global_roles.workos_slug, '')::text AS role_slug,
  COALESCE(ora.created_at, our.created_at)::timestamptz AS created_at
FROM organization_user_relationships AS our
JOIN users
  ON users.id = our.user_id
LEFT JOIN LATERAL (
  SELECT *
  FROM organization_role_assignments
  WHERE organization_role_assignments.organization_id = our.organization_id
    AND organization_role_assignments.workos_user_id = users.workos_id
    AND organization_role_assignments.deleted_at IS NULL
  ORDER BY organization_role_assignments.created_at
  LIMIT 1
) AS ora ON TRUE
LEFT JOIN organization_roles
  ON ora.role_urn = 'role:organization:' || organization_roles.id::text
  AND organization_roles.organization_id = our.organization_id
  AND organization_roles.deleted IS FALSE
  AND organization_roles.workos_deleted IS FALSE
LEFT JOIN global_roles
  ON ora.role_urn = 'role:global:' || global_roles.id::text
  AND global_roles.deleted IS FALSE
  AND global_roles.workos_deleted IS FALSE
WHERE our.organization_id = @organization_id
  AND users.workos_id = sqlc.arg(workos_user_id)::text
  AND users.workos_id IS NOT NULL
  AND our.deleted IS FALSE
LIMIT 1;

-- name: ListOrganizationRoleAssignmentsByWorkosUsers :many
SELECT
  our.user_id,
  users.workos_id::text AS workos_user_id,
  our.workos_membership_id,
  COALESCE(organization_roles.workos_slug, global_roles.workos_slug, '')::text AS role_slug,
  COALESCE(ora.created_at, our.created_at)::timestamptz AS created_at
FROM organization_user_relationships AS our
JOIN users
  ON users.id = our.user_id
LEFT JOIN LATERAL (
  SELECT *
  FROM organization_role_assignments
  WHERE organization_role_assignments.organization_id = our.organization_id
    AND organization_role_assignments.workos_user_id = users.workos_id
    AND organization_role_assignments.deleted_at IS NULL
  ORDER BY organization_role_assignments.created_at
  LIMIT 1
) AS ora ON TRUE
LEFT JOIN organization_roles
  ON ora.role_urn = 'role:organization:' || organization_roles.id::text
  AND organization_roles.organization_id = our.organization_id
  AND organization_roles.deleted IS FALSE
  AND organization_roles.workos_deleted IS FALSE
LEFT JOIN global_roles
  ON ora.role_urn = 'role:global:' || global_roles.id::text
  AND global_roles.deleted IS FALSE
  AND global_roles.workos_deleted IS FALSE
WHERE our.organization_id = @organization_id
  AND users.workos_id = ANY(@workos_user_ids::text[])
  AND users.workos_id IS NOT NULL
  AND our.workos_membership_id IS NOT NULL
  AND our.deleted IS FALSE
ORDER BY users.workos_id, role_slug;

-- name: ListAccessMembers :many
SELECT
  users.id,
  users.display_name,
  users.email,
  users.photo_url,
  COALESCE(organization_roles.id::text, global_roles.id::text, '')::text AS role_id,
  COALESCE(ora.created_at, our.created_at)::timestamptz AS joined_at,
  -- The member's identity-provider profile, when the directory has synced one.
  -- A member with no directory row, or one whose provider does not report the
  -- attribute, comes back as an empty string.
  COALESCE(du.attributes ->> 'department_name', '')::text AS department,
  COALESCE(dg_names.group_names, '{}'::text[])::text[] AS group_names
FROM organization_user_relationships AS our
JOIN users
  ON users.id = our.user_id
LEFT JOIN LATERAL (
  -- The member's directory profile, preferring an explicit user link over an
  -- email match so a stale email row cannot shadow the linked profile. An
  -- email-matched row has a NULL user_id, and NULLs sort first under DESC, so
  -- the link test needs NULLS LAST to actually win; among equals the profile
  -- the directory updated most recently is the current one.
  SELECT d.id, d.attributes
  FROM directory_users d
  WHERE d.organization_id = our.organization_id
    AND d.deleted IS FALSE
    AND d.workos_deleted IS FALSE
    AND (d.user_id = users.id OR LOWER(d.email) = LOWER(users.email))
  ORDER BY (d.user_id = users.id) DESC NULLS LAST, d.workos_updated_at DESC, d.id
  LIMIT 1
) du ON TRUE
LEFT JOIN LATERAL (
  SELECT ARRAY_AGG(DISTINCT dg.name) AS group_names
  FROM directory_user_group_memberships m
  INNER JOIN directory_groups dg
    ON dg.id = m.directory_group_id
    AND dg.organization_id = our.organization_id
    AND dg.deleted IS FALSE
    AND dg.workos_deleted IS FALSE
  WHERE m.directory_user_id = du.id
    AND m.deleted IS FALSE
) dg_names ON TRUE
LEFT JOIN organization_role_assignments AS ora
  ON ora.organization_id = our.organization_id
  AND ora.workos_user_id = users.workos_id
  AND ora.deleted_at IS NULL
LEFT JOIN organization_roles
  ON ora.role_urn = 'role:organization:' || organization_roles.id::text
  AND organization_roles.organization_id = our.organization_id
  AND organization_roles.deleted IS FALSE
  AND organization_roles.workos_deleted IS FALSE
LEFT JOIN global_roles
  ON ora.role_urn = 'role:global:' || global_roles.id::text
  AND global_roles.deleted IS FALSE
  AND global_roles.workos_deleted IS FALSE
WHERE our.organization_id = @organization_id
  AND our.deleted IS FALSE
  AND users.deleted_at IS NULL
ORDER BY users.email, users.id;

-- name: ListAccessNotificationUsers :many
SELECT
  users.id,
  users.email
FROM organization_user_relationships AS our
JOIN users
  ON users.id = our.user_id
WHERE our.organization_id = @organization_id
  AND our.deleted IS FALSE
  AND users.deleted_at IS NULL
  AND users.email <> ''
ORDER BY users.email, users.id;

-- name: ListActiveOrganizationAdmins :many
-- Returns a best-effort notification audience of active organization
-- administrators and their Loops contact fields. This is not an authorization
-- decision or a delivery guarantee; callers use the admins available when the
-- notification is sent.
-- Resolve roles only through the internal user ID. WorkOS role assignments
-- are not treated as internal authorization state until they are linked.
SELECT DISTINCT
  users.id,
  users.display_name,
  users.email
FROM organization_user_relationships AS our
JOIN users
  ON users.id = our.user_id
JOIN organization_role_assignments AS ora
  ON ora.organization_id = our.organization_id
  AND ora.user_id = users.id
  AND ora.deleted_at IS NULL
LEFT JOIN organization_roles
  ON ora.role_urn = 'role:organization:' || organization_roles.id::text
  AND organization_roles.organization_id = ora.organization_id
  AND organization_roles.deleted IS FALSE
  AND organization_roles.workos_deleted IS FALSE
LEFT JOIN global_roles
  ON ora.role_urn = 'role:global:' || global_roles.id::text
  AND global_roles.deleted IS FALSE
  AND global_roles.workos_deleted IS FALSE
WHERE our.organization_id = @organization_id
  AND our.deleted IS FALSE
  AND COALESCE(organization_roles.workos_slug, global_roles.workos_slug) = 'admin'
  AND users.deleted_at IS NULL
  AND users.email <> ''
ORDER BY users.email, users.id;

-- name: GetActiveOrganizationAdmin :one
-- Returns one active organization administrator and their Loops contact fields.
-- Resolve roles only through the internal user ID. WorkOS role assignments
-- are not treated as internal authorization state until they are linked.
SELECT DISTINCT
  users.id,
  users.display_name,
  users.email
FROM organization_user_relationships AS our
JOIN users
  ON users.id = our.user_id
JOIN organization_role_assignments AS ora
  ON ora.organization_id = our.organization_id
  AND ora.user_id = users.id
  AND ora.deleted_at IS NULL
LEFT JOIN organization_roles
  ON ora.role_urn = 'role:organization:' || organization_roles.id::text
  AND organization_roles.organization_id = ora.organization_id
  AND organization_roles.deleted IS FALSE
  AND organization_roles.workos_deleted IS FALSE
LEFT JOIN global_roles
  ON ora.role_urn = 'role:global:' || global_roles.id::text
  AND global_roles.deleted IS FALSE
  AND global_roles.workos_deleted IS FALSE
WHERE our.organization_id = @organization_id
  AND our.user_id = @user_id
  AND our.deleted IS FALSE
  AND COALESCE(organization_roles.workos_slug, global_roles.workos_slug) = 'admin'
  AND users.deleted_at IS NULL
  AND users.email <> '';

-- name: ListMemberRolePrincipalsByWorkosUser :many
SELECT
  COALESCE(organization_roles.workos_slug, global_roles.workos_slug)::text AS role_slug,
  ora.role_urn::text AS principal_urn
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
  AND ora.deleted_at IS NULL
ORDER BY role_slug;

-- name: ListMemberRolePrincipalsByUser :many
SELECT
  COALESCE(organization_roles.workos_slug, global_roles.workos_slug)::text AS role_slug,
  ora.role_urn::text AS principal_urn
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
  AND ora.user_id = sqlc.arg(user_id)::text
  AND COALESCE(organization_roles.workos_slug, global_roles.workos_slug) IS NOT NULL
  AND ora.deleted_at IS NULL
ORDER BY role_slug;

-- name: ListMemberPrincipalsByUsers :many
-- Resolve active user and role principals together for a requested set of owners.
WITH active_users AS (
  SELECT users.id AS user_id
  FROM users
  JOIN organization_user_relationships AS our ON our.user_id = users.id
  WHERE our.organization_id = @organization_id
    AND users.id = ANY(@user_ids::text[])
    AND users.deleted_at IS NULL
    AND our.deleted_at IS NULL
)
SELECT user_id, ('user:' || user_id)::text AS principal_urn
FROM active_users
UNION
SELECT active_users.user_id, ora.role_urn::text AS principal_urn
FROM active_users
JOIN organization_role_assignments AS ora ON ora.user_id = active_users.user_id
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
  AND ora.deleted_at IS NULL
  AND COALESCE(organization_roles.workos_slug, global_roles.workos_slug) IS NOT NULL;

-- name: ListOrganizationRoleAssignmentRecordsByWorkosUser :many
SELECT
  id,
  organization_id,
  workos_user_id,
  user_id,
  role_urn,
  workos_membership_id,
  workos_updated_at,
  workos_last_event_id,
  created_at,
  updated_at,
  deleted_at
FROM organization_role_assignments
WHERE organization_id = @organization_id
  AND workos_user_id = @workos_user_id
ORDER BY created_at;

-- name: UpsertOrganizationRoleAssignment :execrows
WITH input_role_urn AS (
  SELECT role_urn
  FROM (
    SELECT 'role:organization:' || id::text AS role_urn, 'organization'::text AS role_kind
    FROM organization_roles
    WHERE organization_roles.organization_id = @organization_id
      AND organization_roles.workos_slug = sqlc.arg(workos_role_slug)
      AND organization_roles.deleted IS FALSE
      AND organization_roles.workos_deleted IS FALSE
    UNION ALL
    SELECT 'role:global:' || id::text AS role_urn, 'global'::text AS role_kind
    FROM global_roles
    WHERE global_roles.workos_slug = sqlc.arg(workos_role_slug)
      AND global_roles.deleted IS FALSE
      AND global_roles.workos_deleted IS FALSE
  ) roles
  -- Keep assignment writes aligned with role reads: org roles shadow global roles.
  ORDER BY role_kind DESC
  LIMIT 1
)
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
ON CONFLICT (organization_id, workos_user_id, role_urn) WHERE deleted_at IS NULL DO UPDATE SET
  user_id = COALESCE(EXCLUDED.user_id, organization_role_assignments.user_id),
  workos_membership_id = EXCLUDED.workos_membership_id,
  workos_updated_at = EXCLUDED.workos_updated_at,
  workos_last_event_id = COALESCE(EXCLUDED.workos_last_event_id, organization_role_assignments.workos_last_event_id),
  updated_at = clock_timestamp();

-- name: ReplaceOrganizationRoleAssignment :one
WITH input_role_urn AS (
  SELECT role_urn
  FROM (
    SELECT 'role:organization:' || id::text AS role_urn, 'organization'::text AS role_kind
    FROM organization_roles
    WHERE organization_roles.organization_id = @organization_id
      AND organization_roles.workos_slug = sqlc.arg(workos_role_slug)
      AND organization_roles.deleted IS FALSE
      AND organization_roles.workos_deleted IS FALSE
    UNION ALL
    SELECT 'role:global:' || id::text AS role_urn, 'global'::text AS role_kind
    FROM global_roles
    WHERE global_roles.workos_slug = sqlc.arg(workos_role_slug)
      AND global_roles.deleted IS FALSE
      AND global_roles.workos_deleted IS FALSE
  ) roles
  -- Keep assignment writes aligned with role reads: org roles shadow global roles.
  ORDER BY role_kind DESC
  LIMIT 1
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
  ON CONFLICT (organization_id, workos_user_id, role_urn) WHERE deleted_at IS NULL DO UPDATE SET
    user_id = COALESCE(EXCLUDED.user_id, organization_role_assignments.user_id),
    workos_membership_id = EXCLUDED.workos_membership_id,
    workos_updated_at = EXCLUDED.workos_updated_at,
    workos_last_event_id = COALESCE(EXCLUDED.workos_last_event_id, organization_role_assignments.workos_last_event_id),
    updated_at = clock_timestamp()
  RETURNING role_urn
),
deleted AS (
UPDATE organization_role_assignments
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_role_assignments.organization_id = @organization_id
  AND organization_role_assignments.workos_user_id = @workos_user_id
  AND EXISTS (SELECT 1 FROM upserted)
  AND organization_role_assignments.role_urn NOT IN (SELECT role_urn FROM upserted)
  AND organization_role_assignments.deleted_at IS NULL
  RETURNING 1
)
SELECT COUNT(*)::bigint FROM upserted;

-- name: SoftDeleteAllRoleAssignmentsByWorkosUser :execrows
UPDATE organization_role_assignments
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND workos_user_id = @workos_user_id
  AND deleted_at IS NULL;

-- name: ListActiveRoleIDsByWorkosUser :many
SELECT
  COALESCE(organization_roles.id::text, global_roles.id::text, '')::text AS role_id
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
  AND COALESCE(organization_roles.id, global_roles.id) IS NOT NULL
  AND ora.deleted_at IS NULL
ORDER BY role_id;

-- name: SoftDeleteRoleAssignmentsBySlug :execrows
-- Soft-deletes all active role assignments matching a specific role slug for a given user.
-- Used when deleting a role to remove just that role's assignments without affecting other roles.
UPDATE organization_role_assignments
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_role_assignments.organization_id = @organization_id
  AND organization_role_assignments.workos_user_id = @workos_user_id
  AND organization_role_assignments.role_urn IN (
    SELECT 'role:organization:' || organization_roles.id::text
    FROM organization_roles
    WHERE organization_roles.organization_id = @organization_id
      AND organization_roles.workos_slug = sqlc.arg(workos_role_slug)
    UNION ALL
    SELECT 'role:global:' || global_roles.id::text
    FROM global_roles
    WHERE global_roles.workos_slug = sqlc.arg(workos_role_slug)
  )
  AND organization_role_assignments.deleted_at IS NULL;

-- name: FindMCPResourceProject :one
-- Resolves the project owning one MCP resource, so a project-scoped grant is
-- checked against the resource's own project rather than any project the
-- caller happens to hold. A gateway server is addressed by its toolset id, a
-- remote or unproxied one by its own id, and an MCP gateway by its meta
-- server id. Tenancy and soft-deletion are decided by the project row in
-- every branch: a toolset outlives the project it belonged to.
SELECT project_id FROM (
  SELECT toolsets.project_id AS project_id
  FROM toolsets
  JOIN projects ON projects.id = toolsets.project_id
  WHERE projects.organization_id = @organization_id
    AND toolsets.id = sqlc.arg(resource_id)::uuid
    AND toolsets.deleted IS FALSE
    AND projects.deleted IS FALSE
  UNION ALL
  SELECT mcp_servers.project_id AS project_id
  FROM mcp_servers
  JOIN projects ON projects.id = mcp_servers.project_id
  WHERE projects.organization_id = @organization_id
    AND mcp_servers.id = sqlc.arg(resource_id)::uuid
    AND mcp_servers.deleted IS FALSE
    AND projects.deleted IS FALSE
  UNION ALL
  SELECT meta_mcp_servers.project_id AS project_id
  FROM meta_mcp_servers
  JOIN projects ON projects.id = meta_mcp_servers.project_id
  WHERE projects.organization_id = @organization_id
    AND meta_mcp_servers.id = sqlc.arg(resource_id)::uuid
    AND meta_mcp_servers.deleted IS FALSE
    AND projects.deleted IS FALSE
) AS owning
LIMIT 1;

-- name: LockResourceAudience :exec
-- Serializes audience saves for one resource, so the version check and the
-- replacement that follows it cannot interleave with another administrator's.
-- The lock is held until the transaction ends.
SELECT pg_advisory_xact_lock(hashtextextended(@organization_id::text || ':' || sqlc.arg(resource_id)::text, 0));

-- name: ListAccessibleMCPServersForUser :many
-- Returns the MCP servers a user can reach, scoped to the user's principals
-- (user:id and their assigned roles).
--
-- Access here means authorization and nothing else. A plugin assignment is
-- distribution — it decides what a server is offered through, not who may call
-- it — and a user blocked by RBAC stays blocked however many plugins carry the
-- server, so plugin membership is deliberately not consulted.
--
-- The shape mirrors the authorization engine's own: a permission is an allow
-- grant for a scope minus a blocked_ grant for THAT SAME scope proving the same
-- server (see authz/expressions.go). mcp:blocked_connect withdraws
-- mcp:connect; it does not withdraw mcp:read.
WITH user_grants AS (
  SELECT pg.scope, pg.selectors
  FROM principal_grants pg
  WHERE pg.organization_id = @organization_id
    AND COALESCE(pg.effect, 'allow') = 'allow'
    AND pg.principal_urn = ANY(@principal_urns::text[])
    AND pg.scope IN (
      'mcp:connect', 'mcp:read', 'mcp:write',
      'mcp:blocked_connect', 'mcp:blocked_read', 'mcp:blocked_write'
    )
), servers AS (
  SELECT ms.id, ms.name, ms.slug, ms.project_id, p.slug AS project_slug
  FROM mcp_servers ms
  JOIN projects p ON p.id = ms.project_id AND p.organization_id = @organization_id AND p.deleted IS FALSE
  WHERE ms.deleted IS FALSE
    AND ms.visibility <> 'disabled'
), grant_matches AS (
  -- Each grant paired with the servers its selector proves, so the allow and
  -- the block are matched the same way rather than twice over.
  --
  -- This is Selector.Matches against a server-level check
  -- {resource_kind: mcp, resource_id: <server>, project_id: <project>}: every
  -- dimension the grant names must be the wildcard or equal to the check's
  -- value, and a dimension the check does not constrain (tool, disposition) is
  -- skipped. A project-scoped grant therefore reaches only its own project,
  -- and a tool-scoped grant still proves reach to the server.
  SELECT
    s.id AS server_id,
    ug.scope,
    -- Whether this grant would also satisfy StrictMatches, which exclusions
    -- use: every dimension it names must be one the check constrains. A
    -- tool- or disposition-scoped block narrows something inside the server,
    -- so it must not withdraw a server-level permission wholesale.
    NOT EXISTS (
      SELECT 1 FROM jsonb_object_keys(ug.selectors) AS key
      WHERE key NOT IN ('resource_kind', 'resource_id', 'project_id')
    ) AS strict
  FROM servers s
  JOIN user_grants ug ON (
    ug.selectors->>'resource_kind' IN ('*', 'mcp')
    AND ug.selectors->>'resource_id' IN ('*', s.id::text)
    AND (
      ug.selectors->>'project_id' IS NULL
      OR ug.selectors->>'project_id' IN ('*', s.project_id::text)
    )
  )
)
SELECT DISTINCT
  s.id,
  s.name,
  s.slug,
  s.project_id,
  s.project_slug
FROM servers s
JOIN grant_matches allowed
  ON allowed.server_id = s.id
  AND allowed.scope IN ('mcp:connect', 'mcp:read', 'mcp:write')
WHERE NOT EXISTS (
  SELECT 1 FROM grant_matches blocked
  WHERE blocked.server_id = s.id
    AND blocked.strict
    AND blocked.scope = 'mcp:blocked_' || split_part(allowed.scope, ':', 2)
)
ORDER BY s.name;

-- name: ListAccessibleSkillsForUser :many
-- Returns the skills a user can reach, scoped to the user's principals
-- (user:id and their assigned roles).
--
-- Authorization only, on the same terms as the MCP query above: a skill
-- distributed to a plugin the user holds is still unreachable if RBAC does not
-- allow it, so distribution is not consulted.
WITH user_grants AS (
  SELECT pg.scope, pg.selectors
  FROM principal_grants pg
  WHERE pg.organization_id = @organization_id
    AND COALESCE(pg.effect, 'allow') = 'allow'
    AND pg.principal_urn = ANY(@principal_urns::text[])
    AND pg.scope IN (
      'skill:read', 'skill:write',
      'skill:blocked_read', 'skill:blocked_write'
    )
), candidate_skills AS (
  SELECT s.id, s.name, s.display_name, s.project_id, p.slug AS project_slug
  FROM skills s
  JOIN projects p ON p.id = s.project_id AND p.organization_id = @organization_id AND p.deleted IS FALSE
  WHERE s.archived_at IS NULL
), grant_matches AS (
  -- Selector.Matches against {resource_kind: skill, resource_id: <skill>}.
  -- A skill scope permits no dimensions beyond those two (see
  -- authz.allowedSelectorKeys), so there is no project_id to honour here and
  -- no narrower block that could fail StrictMatches — the wildcard and the
  -- per-skill grant are the only shapes a skill grant can take.
  SELECT cs.id AS skill_id, ug.scope
  FROM candidate_skills cs
  JOIN user_grants ug ON (
    ug.selectors->>'resource_kind' IN ('*', 'skill')
    AND ug.selectors->>'resource_id' IN ('*', cs.id::text)
  )
)
SELECT DISTINCT
  cs.id,
  cs.name,
  cs.display_name,
  cs.project_id,
  cs.project_slug
FROM candidate_skills cs
JOIN grant_matches allowed
  ON allowed.skill_id = cs.id
  AND allowed.scope IN ('skill:read', 'skill:write')
WHERE NOT EXISTS (
  SELECT 1 FROM grant_matches blocked
  WHERE blocked.skill_id = cs.id
    AND blocked.scope = 'skill:blocked_' || split_part(allowed.scope, ':', 2)
)
-- SELECT DISTINCT only permits ORDER BY over selected columns, and
-- skills.display_name is NOT NULL, so the coalesce it replaced never fell back.
ORDER BY cs.display_name, cs.name;
-- Agent role membership. Agents have no WorkOS identity, so these assignments
-- are local only and are never reconciled outward. The role joins mirror the
-- member queries above so a deleted role stops granting membership the moment
-- it is deleted, without a foreign key on role_urn.

-- name: ListAgentRoleAssignments :many
SELECT
  ara.agent_id,
  ara.role_urn::text AS principal_urn,
  COALESCE(organization_roles.workos_slug, global_roles.workos_slug)::text AS role_slug
FROM agent_role_assignments AS ara
LEFT JOIN organization_roles
  ON ara.role_urn = 'role:organization:' || organization_roles.id::text
  AND organization_roles.organization_id = ara.organization_id
  AND organization_roles.deleted IS FALSE
  AND organization_roles.workos_deleted IS FALSE
LEFT JOIN global_roles
  ON ara.role_urn = 'role:global:' || global_roles.id::text
  AND global_roles.deleted IS FALSE
  AND global_roles.workos_deleted IS FALSE
JOIN agents
  ON agents.organization_id = ara.organization_id
  AND agents.id = ara.agent_id
  AND agents.deleted IS FALSE
WHERE ara.organization_id = @organization_id
  AND ara.deleted_at IS NULL
  AND COALESCE(organization_roles.workos_slug, global_roles.workos_slug) IS NOT NULL
ORDER BY principal_urn, ara.agent_id;

-- name: ListAgentRolePrincipals :many
-- The role principals one agent holds. Used on the request path to widen an
-- agent's own policy with the grants of the roles it belongs to.
SELECT ara.role_urn::text AS principal_urn
FROM agent_role_assignments AS ara
LEFT JOIN organization_roles
  ON ara.role_urn = 'role:organization:' || organization_roles.id::text
  AND organization_roles.organization_id = ara.organization_id
  AND organization_roles.deleted IS FALSE
  AND organization_roles.workos_deleted IS FALSE
LEFT JOIN global_roles
  ON ara.role_urn = 'role:global:' || global_roles.id::text
  AND global_roles.deleted IS FALSE
  AND global_roles.workos_deleted IS FALSE
JOIN agents
  ON agents.organization_id = ara.organization_id
  AND agents.id = ara.agent_id
  AND agents.deleted IS FALSE
WHERE ara.organization_id = @organization_id
  AND ara.agent_id = @agent_id
  AND ara.deleted_at IS NULL
  AND COALESCE(organization_roles.workos_slug, global_roles.workos_slug) IS NOT NULL
ORDER BY principal_urn;

-- name: ListAssignableAgents :many
-- Agents that may be named in a role or resource audience. Suspended and
-- revoked agents keep the assignments they already hold, but cannot be given
-- new ones, so lifecycle is filtered here rather than at the call site.
SELECT id, name
FROM agents
WHERE organization_id = @organization_id
  AND deleted IS FALSE
  AND suspended_at IS NULL
  AND revoked_at IS NULL
ORDER BY LOWER(name), id;

-- name: UpsertAgentRoleAssignment :execrows
INSERT INTO agent_role_assignments (organization_id, agent_id, role_urn)
SELECT @organization_id, agents.id, sqlc.arg(role_urn)::text
FROM agents
WHERE agents.organization_id = @organization_id
  AND agents.id = @agent_id
  AND agents.deleted IS FALSE
ON CONFLICT (organization_id, agent_id, role_urn) WHERE deleted_at IS NULL DO UPDATE SET
  updated_at = clock_timestamp();

-- name: SoftDeleteAgentRoleAssignmentsExcept :exec
-- Removes the role from every agent not in the retained set, so one write can
-- express the complete agent membership of a role.
UPDATE agent_role_assignments
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND role_urn = sqlc.arg(role_urn)::text
  AND deleted_at IS NULL
  AND NOT (agent_id = ANY(@retained_agent_ids::uuid[]));

-- name: SoftDeleteAgentRoleAssignmentsByRole :exec
UPDATE agent_role_assignments
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND role_urn = sqlc.arg(role_urn)::text
  AND deleted_at IS NULL;

-- name: ListAgentNames :many
-- Every agent a rule or assignment can still name, including suspended and
-- revoked ones. Those keep the access they already hold, so a surface that
-- resolved names from the assignable set alone would render them as deleted.
SELECT id, name
FROM agents
WHERE organization_id = @organization_id
  AND deleted IS FALSE
ORDER BY LOWER(name), id;

-- name: LockAgentRoleAssignments :exec
-- Serializes agent membership writes for one role, so a read-then-replace
-- cannot interleave with another administrator's. Held until the transaction
-- ends. The role row lock is not enough on its own: a system role lives in
-- global_roles and has no per-organization row to lock.
SELECT pg_advisory_xact_lock(hashtextextended(@organization_id::text || ':' || sqlc.arg(role_urn)::text, 0));

-- name: ListUserRolePrincipals :many
-- Every role principal a member holds, in one read: direct role assignments
-- first (the same rows as ListMemberRolePrincipalsByUser), then roles granted
-- through directory role mappings. A mapping applies when its group contains
-- the member's directory profile, or its attribute value matches it. The
-- profile is the directory user linked to the member, falling back to an
-- email match. Mappings that point at a deleted role are skipped. Callers
-- dedupe roles that come from both sources.
WITH direct AS (
  SELECT
    COALESCE(organization_roles.workos_slug, global_roles.workos_slug)::text AS role_slug,
    ora.role_urn::text AS principal_urn
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
    AND ora.user_id = sqlc.arg(user_id)::text
    AND COALESCE(organization_roles.workos_slug, global_roles.workos_slug) IS NOT NULL
    AND ora.deleted_at IS NULL
),
member AS (
  SELECT u.id, u.email
  FROM users AS u
  WHERE u.id = sqlc.arg(user_id)::text
),
profile AS (
  SELECT d.id, d.attributes
  FROM directory_users AS d
  CROSS JOIN member
  WHERE d.organization_id = @organization_id
    AND d.deleted IS FALSE
    AND d.workos_deleted IS FALSE
    AND (d.user_id = member.id OR LOWER(d.email) = LOWER(member.email))
  ORDER BY (d.user_id = member.id) DESC NULLS LAST, d.workos_updated_at DESC, d.id
  LIMIT 1
),
mapped AS (
  SELECT DISTINCT drm.role_urn::text AS principal_urn
  FROM directory_role_mappings AS drm
  CROSS JOIN profile
  WHERE drm.organization_id = @organization_id
    AND drm.deleted IS FALSE
    AND (
      (
        drm.source_kind = 'group'
        AND EXISTS (
          SELECT 1
          FROM directory_user_group_memberships AS m
          JOIN directory_groups AS dg
            ON dg.id = m.directory_group_id
            AND dg.organization_id = drm.organization_id
            AND dg.deleted IS FALSE
            AND dg.workos_deleted IS FALSE
          WHERE m.directory_user_id = profile.id
            AND m.directory_group_id = drm.directory_group_id
            AND m.deleted IS FALSE
        )
      )
      OR (
        drm.source_kind = 'attribute'
        AND profile.attributes ->> drm.attribute_key = drm.attribute_value
      )
    )
    AND (
      EXISTS (
        SELECT 1
        FROM organization_roles AS r
        WHERE drm.role_urn = 'role:organization:' || r.id::text
          AND r.organization_id = drm.organization_id
          AND r.deleted IS FALSE
          AND r.workos_deleted IS FALSE
      )
      OR EXISTS (
        SELECT 1
        FROM global_roles AS g
        WHERE drm.role_urn = 'role:global:' || g.id::text
          AND g.deleted IS FALSE
          AND g.workos_deleted IS FALSE
      )
    )
)
SELECT principal_urn::text AS principal_urn
FROM (
  SELECT 0 AS source_rank, role_slug AS sort_key, principal_urn FROM direct
  UNION ALL
  SELECT 1 AS source_rank, principal_urn AS sort_key, principal_urn FROM mapped
) AS roles
ORDER BY source_rank, sort_key;

-- name: ListDirectoryMappedRoleMemberCounts :many
-- Per role, the active members who hold it only through a directory role
-- mapping. Members with a live direct assignment of the same role are left
-- out, so callers add this to the direct member count. Each member's
-- directory profile is chosen the same way as in ListUserRolePrincipals.
WITH members AS (
  SELECT u.id, u.email
  FROM users AS u
  JOIN organization_user_relationships AS our
    ON our.user_id = u.id
    AND our.organization_id = @organization_id
    AND our.deleted_at IS NULL
  WHERE u.deleted_at IS NULL
),
profiles AS (
  SELECT members.id AS user_id, p.id AS directory_user_id, p.attributes
  FROM members
  CROSS JOIN LATERAL (
    SELECT d.id, d.attributes
    FROM directory_users AS d
    WHERE d.organization_id = @organization_id
      AND d.deleted IS FALSE
      AND d.workos_deleted IS FALSE
      AND (d.user_id = members.id OR LOWER(d.email) = LOWER(members.email))
    ORDER BY (d.user_id = members.id) DESC NULLS LAST, d.workos_updated_at DESC, d.id
    LIMIT 1
  ) AS p
)
SELECT
  drm.role_urn::text AS role_urn,
  COUNT(DISTINCT profiles.user_id)::bigint AS member_count
FROM directory_role_mappings AS drm
JOIN profiles
  ON (
    drm.source_kind = 'group'
    AND EXISTS (
      SELECT 1
      FROM directory_user_group_memberships AS m
      JOIN directory_groups AS dg
        ON dg.id = m.directory_group_id
        AND dg.organization_id = drm.organization_id
        AND dg.deleted IS FALSE
        AND dg.workos_deleted IS FALSE
      WHERE m.directory_user_id = profiles.directory_user_id
        AND m.directory_group_id = drm.directory_group_id
        AND m.deleted IS FALSE
    )
  )
  OR (
    drm.source_kind = 'attribute'
    AND profiles.attributes ->> drm.attribute_key = drm.attribute_value
  )
WHERE drm.organization_id = @organization_id
  AND drm.deleted IS FALSE
  AND NOT EXISTS (
    SELECT 1
    FROM organization_role_assignments AS ora
    WHERE ora.organization_id = drm.organization_id
      AND ora.role_urn = drm.role_urn
      AND ora.user_id = profiles.user_id
      AND ora.deleted_at IS NULL
  )
GROUP BY drm.role_urn;

-- name: ListDirectoryRoleMappings :many
SELECT
  drm.id,
  drm.source_kind,
  drm.directory_group_id,
  dg.name AS directory_group_name,
  drm.attribute_key,
  drm.attribute_value,
  drm.role_urn,
  drm.created_at,
  drm.updated_at
FROM directory_role_mappings AS drm
LEFT JOIN directory_groups AS dg
  ON dg.id = drm.directory_group_id
  AND dg.organization_id = drm.organization_id
WHERE drm.organization_id = @organization_id
  AND drm.deleted IS FALSE
ORDER BY drm.source_kind, dg.name, drm.attribute_key, drm.attribute_value, drm.id;

-- name: UpsertDirectoryGroupRoleMapping :one
INSERT INTO directory_role_mappings (
  organization_id,
  source_kind,
  directory_group_id,
  role_urn
)
VALUES (
  @organization_id,
  'group',
  @directory_group_id,
  @role_urn
)
ON CONFLICT (organization_id, directory_group_id)
  WHERE deleted IS FALSE AND directory_group_id IS NOT NULL
DO UPDATE SET
  role_urn = EXCLUDED.role_urn,
  updated_at = clock_timestamp()
RETURNING id, role_urn, created_at, updated_at;

-- name: UpsertDirectoryAttributeRoleMapping :one
INSERT INTO directory_role_mappings (
  organization_id,
  source_kind,
  attribute_key,
  attribute_value,
  role_urn
)
VALUES (
  @organization_id,
  'attribute',
  @attribute_key,
  @attribute_value,
  @role_urn
)
ON CONFLICT (organization_id, attribute_key, attribute_value)
  WHERE deleted IS FALSE AND attribute_key IS NOT NULL
DO UPDATE SET
  role_urn = EXCLUDED.role_urn,
  updated_at = clock_timestamp()
RETURNING id, role_urn, created_at, updated_at;

-- name: GetLiveDirectoryRoleMappingRoleForSource :one
-- The role a group or attribute value is mapped to now, locked so a
-- concurrent set cannot slip between this read and the upsert.
SELECT role_urn
FROM directory_role_mappings
WHERE organization_id = @organization_id
  AND deleted IS FALSE
  AND (
    (sqlc.narg('directory_group_id')::uuid IS NOT NULL AND directory_group_id = sqlc.narg('directory_group_id')::uuid)
    OR (
      sqlc.narg('attribute_key')::text IS NOT NULL
      AND attribute_key = sqlc.narg('attribute_key')::text
      AND attribute_value = sqlc.narg('attribute_value')::text
    )
  )
FOR UPDATE;

-- name: GetActiveDirectoryGroupName :one
SELECT name
FROM directory_groups
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE
  AND workos_deleted IS FALSE;

-- name: GetDirectoryRoleMapping :one
SELECT id, source_kind, directory_group_id, attribute_key, attribute_value, role_urn
FROM directory_role_mappings
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE;

-- name: DeleteDirectoryRoleMapping :execrows
UPDATE directory_role_mappings
SET deleted_at = clock_timestamp(),
  updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE;
