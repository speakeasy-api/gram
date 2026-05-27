-- Queries for managing principal grants (RBAC).
-- principal_grants is org-scoped (no project_id); every query is scoped to organization_id.

-- name: ListPrincipalGrantsByOrg :many
-- Returns all grant rows for an organization, optionally filtered by principal URN.
SELECT id, organization_id, principal_urn, principal_type, scope, effect, selectors, created_at, updated_at
FROM principal_grants
WHERE organization_id = @organization_id
  AND (@principal_urn::text = '' OR principal_urn = @principal_urn)
ORDER BY principal_urn, scope;

-- name: GetPrincipalGrants :many
-- Returns all grant rows matching a set of principal URNs within an org.
-- Used by the access resolver to load grants for a user+role in a single query.
SELECT principal_urn, scope, effect, selectors
FROM principal_grants
WHERE organization_id = @organization_id
  AND principal_urn = ANY(@principal_urns::text[]);

-- name: UpsertPrincipalGrant :one
-- Creates or updates a single grant row. On conflict (same org/principal/scope/effect/selectors),
-- the updated_at is refreshed. Uses COALESCE to match the functional unique index.
INSERT INTO principal_grants (organization_id, principal_urn, scope, effect, selectors)
VALUES (@organization_id, @principal_urn, @scope, @effect, @selectors)
ON CONFLICT (organization_id, principal_urn, scope, COALESCE(effect, 'allow'), selectors)
DO UPDATE SET updated_at = clock_timestamp()
RETURNING id, organization_id, principal_urn, principal_type, scope, effect, selectors, created_at, updated_at;

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

-- name: ListGlobalRoles :many
SELECT *
FROM global_roles
WHERE deleted_at IS NULL
ORDER BY workos_slug;

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

-- name: ListOrganizationRolesByOrg :many
SELECT *
FROM organization_roles
WHERE organization_id = @organization_id
  AND deleted_at IS NULL
ORDER BY workos_slug;

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

-- Queries for access approval requests and managed access rules.

-- name: ListAccessApprovalRequests :many
SELECT *
FROM access_approval_requests
WHERE organization_id = @organization_id
  AND deleted IS FALSE
  AND (@resource_type::text = '' OR resource_type = @resource_type)
  AND (@status::text = '' OR status = @status)
  AND (@project_id::text = '' OR project_id::text = @project_id)
  AND (
    sqlc.narg(cursor_requested_at)::timestamptz IS NULL
    OR (requested_at, id) < (sqlc.narg(cursor_requested_at)::timestamptz, sqlc.narg(cursor_id)::uuid)
  )
ORDER BY requested_at DESC, id DESC
LIMIT @limit_count;

-- name: GetAccessApprovalRequest :one
SELECT *
FROM access_approval_requests
WHERE organization_id = @organization_id
  AND id = @id
  AND resource_type = @resource_type
  AND deleted IS FALSE;

-- name: GetAccessApprovalRequestForUpdate :one
SELECT *
FROM access_approval_requests
WHERE organization_id = @organization_id
  AND id = @id
  AND resource_type = @resource_type
  AND deleted IS FALSE
FOR UPDATE;

-- name: UpsertAccessApprovalRequest :one
INSERT INTO access_approval_requests (
  organization_id,
  project_id,
  resource_type,
  requester_user_id,
  requester_email,
  requester_display_name,
  status,
  request_fingerprint,
  display_name,
  observed_summary,
  first_blocked_at,
  last_blocked_at
) VALUES (
  @organization_id,
  @project_id,
  @resource_type,
  @requester_user_id,
  @requester_email,
  @requester_display_name,
  'requested',
  @request_fingerprint,
  @display_name,
  @observed_summary,
  clock_timestamp(),
  clock_timestamp()
)
ON CONFLICT (organization_id, project_id, resource_type, requester_user_id, request_fingerprint)
WHERE deleted IS FALSE AND status = 'requested' AND requester_user_id IS NOT NULL AND request_fingerprint IS NOT NULL
DO UPDATE SET
  requester_email = EXCLUDED.requester_email,
  requester_display_name = EXCLUDED.requester_display_name,
  display_name = COALESCE(EXCLUDED.display_name, access_approval_requests.display_name),
  observed_summary = CASE
    WHEN EXCLUDED.observed_summary = '{}'::jsonb THEN access_approval_requests.observed_summary
    ELSE EXCLUDED.observed_summary
  END,
  blocked_count = access_approval_requests.blocked_count + 1,
  last_blocked_at = clock_timestamp(),
  updated_at = clock_timestamp()
RETURNING *, (xmax = 0) AS was_created;

-- name: DecideAccessApprovalRequest :one
UPDATE access_approval_requests
SET status = @status,
    decided_at = clock_timestamp(),
    decided_by = @decided_by,
    decision_note = @decision_note,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND resource_type = @resource_type
  AND deleted IS FALSE
RETURNING *;

-- name: ListAccessRules :many
SELECT *
FROM access_rules
WHERE organization_id = @organization_id
  AND deleted IS FALSE
  AND (@resource_type::text = '' OR resource_type = @resource_type)
  AND (@disposition::text = '' OR disposition = @disposition)
  AND (@access_scope::text = '' OR access_scope = @access_scope)
  AND (@project_id::text = '' OR project_id::text = @project_id)
  AND (
    sqlc.narg(cursor_created_at)::timestamptz IS NULL
    OR (created_at, id) < (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT @limit_count;

-- name: GetAccessRule :one
SELECT *
FROM access_rules
WHERE organization_id = @organization_id
  AND id = @id
  AND resource_type = @resource_type
  AND deleted IS FALSE;

-- name: GetAccessRuleByMatch :one
SELECT *
FROM access_rules
WHERE organization_id = @organization_id
  AND resource_type = @resource_type
  AND access_scope = @access_scope
  AND (
    (@access_scope::text = 'organization' AND project_id IS NULL)
    OR (@access_scope::text = 'project' AND project_id = @project_id)
  )
  AND match_kind = @match_kind
  AND match_value = @match_value
  AND deleted IS FALSE;

-- name: ListMatchingAccessRules :many
SELECT *
FROM access_rules
WHERE organization_id = @organization_id
  AND deleted IS FALSE
  AND resource_type = @resource_type
  AND (
    access_scope = 'organization'
    OR (access_scope = 'project' AND project_id = @project_id)
  )
  AND EXISTS (
    SELECT 1
    FROM unnest(@match_kinds::text[]) WITH ORDINALITY AS candidate_kinds(match_kind, index)
    JOIN unnest(@match_values::text[]) WITH ORDINALITY AS candidate_values(match_value, index)
      ON candidate_kinds.index = candidate_values.index
    WHERE access_rules.match_kind = candidate_kinds.match_kind
      AND access_rules.match_value = candidate_values.match_value
  )
ORDER BY
  CASE WHEN disposition = 'denied' THEN 0 ELSE 1 END,
  created_at DESC,
  id DESC;

-- name: CreateAccessRule :one
INSERT INTO access_rules (
  organization_id,
  project_id,
  access_scope,
  resource_type,
  disposition,
  match_kind,
  match_value,
  display_name,
  observed_summary,
  source_request_id,
  created_by,
  updated_by,
  reason
) VALUES (
  @organization_id,
  @project_id,
  @access_scope,
  @resource_type,
  @disposition,
  @match_kind,
  @match_value,
  @display_name,
  @observed_summary,
  @source_request_id,
  @created_by,
  @updated_by,
  @reason
)
RETURNING *;

-- name: UpdateAccessRule :one
UPDATE access_rules
SET disposition = @disposition,
    project_id = @project_id,
    access_scope = @access_scope,
    resource_type = @resource_type,
    match_kind = @match_kind,
    match_value = @match_value,
    display_name = @display_name,
    observed_summary = @observed_summary,
    updated_by = @updated_by,
    reason = @reason,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND resource_type = @resource_type
  AND deleted IS FALSE
RETURNING *;

-- name: DeleteAccessRule :one
UPDATE access_rules
SET deleted_at = clock_timestamp(),
    updated_by = @updated_by,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND resource_type = @resource_type
  AND deleted IS FALSE
RETURNING *;
