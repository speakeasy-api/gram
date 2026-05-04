-- =============================================================================
-- organizations
-- =============================================================================

-- name: CreateOrganization :one
INSERT INTO organizations (name, slug, account_type, workos_id)
VALUES (
  @name,
  @slug,
  COALESCE(sqlc.narg('account_type')::text, 'enterprise'),
  sqlc.narg('workos_id')
)
RETURNING *;

-- name: UpdateOrganization :one
UPDATE organizations
SET
  name = COALESCE(sqlc.narg('name'), name),
  slug = COALESCE(sqlc.narg('slug'), slug),
  account_type = COALESCE(sqlc.narg('account_type'), account_type),
  workos_id = CASE
    WHEN @clear_workos_id::boolean THEN NULL
    ELSE COALESCE(sqlc.narg('workos_id'), workos_id)
  END,
  updated_at = clock_timestamp()
WHERE id = @id
RETURNING *;

-- name: GetOrganization :one
SELECT * FROM organizations WHERE id = @id;

-- ListOrganizations uses keyset pagination on the (random) uuid id. Stable
-- across concurrent inserts but not insertion-ordered. Caller passes
-- uuid.Nil for the first page and the last returned id for subsequent ones.
-- Caller is also responsible for fetching limit+1 to detect `next_cursor`.
-- name: ListOrganizations :many
SELECT * FROM organizations
WHERE id > @after
ORDER BY id ASC
LIMIT @max_rows;

-- name: DeleteOrganization :exec
DELETE FROM organizations WHERE id = @id;

-- UpsertOrganizationBySlug is the find-or-create path used by the
-- default-user bootstrap. Same no-op-update trick as UpsertUserByEmail.
-- name: UpsertOrganizationBySlug :one
INSERT INTO organizations (name, slug)
VALUES (@name, @slug)
ON CONFLICT (slug) DO UPDATE SET slug = EXCLUDED.slug
RETURNING *;

-- =============================================================================
-- WorkOS emulation: users (ListUsers with filters)
-- =============================================================================

-- ListUsersFiltered is the WorkOS user_management ListUsers shape: keyset
-- pagination on id with optional email-equality and organization-membership
-- filters. Both filters are NULL-aware sqlc.narg parameters so a single
-- query handles every combination.
-- name: ListUsersFiltered :many
SELECT u.* FROM users u
WHERE u.id > @after
  AND (sqlc.narg('email')::text IS NULL OR u.email = sqlc.narg('email')::text)
  AND (sqlc.narg('organization_id')::uuid IS NULL OR EXISTS (
    SELECT 1 FROM memberships m
    WHERE m.user_id = u.id AND m.organization_id = sqlc.narg('organization_id')::uuid
  ))
ORDER BY u.id ASC
LIMIT @max_rows;

-- =============================================================================
-- WorkOS emulation: memberships (ListOrganizationMemberships, GetMembership)
-- =============================================================================

-- ListMembershipsWithOrgName joins memberships with organizations so the
-- WorkOS-shaped response can include `organization_name` (the SDK's
-- OrganizationMembership type carries it).
-- name: ListMembershipsWithOrgName :many
SELECT
  m.id,
  m.user_id,
  m.organization_id,
  o.name AS organization_name,
  m.role,
  m.created_at,
  m.updated_at
FROM memberships m
JOIN organizations o ON o.id = m.organization_id
WHERE m.id > @after
  AND (sqlc.narg('user_id')::uuid IS NULL OR m.user_id = sqlc.narg('user_id')::uuid)
  AND (sqlc.narg('organization_id')::uuid IS NULL OR m.organization_id = sqlc.narg('organization_id')::uuid)
ORDER BY m.id ASC
LIMIT @max_rows;

-- GetMembershipWithOrgName is the by-id variant used by the membership
-- update/delete endpoints to render the response.
-- name: GetMembershipWithOrgName :one
SELECT
  m.id,
  m.user_id,
  m.organization_id,
  o.name AS organization_name,
  m.role,
  m.created_at,
  m.updated_at
FROM memberships m
JOIN organizations o ON o.id = m.organization_id
WHERE m.id = @id;

-- =============================================================================
-- WorkOS emulation: invitations
-- =============================================================================

-- name: CreateInvitation :one
INSERT INTO invitations (email, organization_id, token, inviter_user_id, expires_at)
VALUES (@email, @organization_id, @token, sqlc.narg('inviter_user_id'), @expires_at)
RETURNING *;

-- name: GetInvitation :one
SELECT * FROM invitations WHERE id = @id;

-- name: GetInvitationByToken :one
SELECT * FROM invitations WHERE token = @token;

-- ListInvitationsByOrg keyset-paginates within an organization.
-- name: ListInvitationsByOrg :many
SELECT * FROM invitations
WHERE organization_id = @organization_id
  AND id > @after
ORDER BY id ASC
LIMIT @max_rows;

-- RevokeInvitation flips the state and stamps revoked_at. Idempotent —
-- repeated revokes return the same row.
-- name: RevokeInvitation :one
UPDATE invitations
SET
  state = 'revoked',
  revoked_at = COALESCE(revoked_at, clock_timestamp()),
  updated_at = clock_timestamp()
WHERE id = @id
RETURNING *;

-- TouchInvitation bumps updated_at without changing state. Used by the
-- /resend endpoint (which doesn't actually re-send anything in local dev,
-- but the timestamp matters for the dashboard).
-- name: TouchInvitation :one
UPDATE invitations
SET updated_at = clock_timestamp()
WHERE id = @id
RETURNING *;

-- AcceptInvitation flips the state, stamps accepted_at, and lets the
-- caller (the dashboard's accept-flow handler) follow up with the
-- membership creation. Idempotent.
-- name: AcceptInvitation :one
UPDATE invitations
SET
  state = 'accepted',
  accepted_at = COALESCE(accepted_at, clock_timestamp()),
  updated_at = clock_timestamp()
WHERE id = @id
RETURNING *;

-- =============================================================================
-- WorkOS emulation: organization roles
-- =============================================================================

-- name: CreateOrganizationRole :one
INSERT INTO organization_roles (organization_id, slug, name, description)
VALUES (@organization_id, @slug, @name, COALESCE(sqlc.narg('description')::text, ''))
RETURNING *;

-- UpsertOrganizationRole is the seed path used by the default-user
-- bootstrap to ensure (admin, member) exist on the Speakeasy org.
-- name: UpsertOrganizationRole :one
INSERT INTO organization_roles (organization_id, slug, name, description)
VALUES (@organization_id, @slug, @name, COALESCE(sqlc.narg('description')::text, ''))
ON CONFLICT (organization_id, slug) DO UPDATE SET slug = EXCLUDED.slug
RETURNING *;

-- name: GetOrganizationRoleBySlug :one
SELECT * FROM organization_roles
WHERE organization_id = @organization_id AND slug = @slug;

-- name: ListOrganizationRoles :many
SELECT * FROM organization_roles
WHERE organization_id = @organization_id
ORDER BY slug ASC;

-- name: UpdateOrganizationRole :one
UPDATE organization_roles
SET
  name = COALESCE(sqlc.narg('name'), name),
  description = COALESCE(sqlc.narg('description'), description),
  updated_at = clock_timestamp()
WHERE organization_id = @organization_id AND slug = @slug
RETURNING *;

-- name: DeleteOrganizationRole :exec
DELETE FROM organization_roles
WHERE organization_id = @organization_id AND slug = @slug;

-- =============================================================================
-- users
-- =============================================================================

-- name: CreateUser :one
INSERT INTO users (email, display_name, photo_url, github_handle, admin, whitelisted)
VALUES (
  @email,
  @display_name,
  sqlc.narg('photo_url'),
  sqlc.narg('github_handle'),
  COALESCE(sqlc.narg('admin')::boolean, FALSE),
  COALESCE(sqlc.narg('whitelisted')::boolean, TRUE)
)
RETURNING *;

-- UpdateUser is a partial patch: every COALESCE leaves the column unchanged
-- when the corresponding sqlc.narg parameter is NULL. There is no
-- clear-via-empty semantics on the optional text columns; if a test needs to
-- null out photo_url or github_handle, recreate the user.
-- name: UpdateUser :one
UPDATE users
SET
  email = COALESCE(sqlc.narg('email'), email),
  display_name = COALESCE(sqlc.narg('display_name'), display_name),
  photo_url = COALESCE(sqlc.narg('photo_url'), photo_url),
  github_handle = COALESCE(sqlc.narg('github_handle'), github_handle),
  admin = COALESCE(sqlc.narg('admin')::boolean, admin),
  whitelisted = COALESCE(sqlc.narg('whitelisted')::boolean, whitelisted),
  updated_at = clock_timestamp()
WHERE id = @id
RETURNING *;

-- name: GetUser :one
SELECT * FROM users WHERE id = @id;

-- name: ListUsers :many
SELECT * FROM users
WHERE id > @after
ORDER BY id ASC
LIMIT @max_rows;

-- UpsertUserByEmail is the find-or-create path used by the default-user
-- bootstrap (idp-design.md §3). The DO UPDATE SET email=EXCLUDED.email is
-- a no-op overwrite so RETURNING fires on the existing row; the rest of
-- the user's fields are NEVER overwritten — once a user exists, only
-- explicit users.update mutates it.
-- name: UpsertUserByEmail :one
INSERT INTO users (email, display_name)
VALUES (@email, @display_name)
ON CONFLICT (email) DO UPDATE SET email = EXCLUDED.email
RETURNING *;

-- DeleteCurrentUsersBySubjectRef sweeps any current_users row whose
-- subject_ref matches the given text (local-mode rows store
-- users.id.String()). No FK exists because workos-mode subject_refs are
-- external WorkOS subs, not UUIDs — see idp-design.md §5.
-- name: DeleteCurrentUsersBySubjectRef :exec
DELETE FROM current_users WHERE subject_ref = @subject_ref;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = @id;

-- =============================================================================
-- memberships
-- =============================================================================

-- CreateMembership is idempotent on (user_id, organization_id) per
-- idp-design.md §6.1. ON CONFLICT DO UPDATE with a no-op SET lets RETURNING
-- fire on the existing row so callers always get the canonical record back.
-- The role from the original insert wins; callers wanting a different role
-- on an existing membership should call UpdateMembership.
-- name: CreateMembership :one
INSERT INTO memberships (user_id, organization_id, role)
VALUES (@user_id, @organization_id, COALESCE(sqlc.narg('role')::text, 'member'))
ON CONFLICT (user_id, organization_id) DO UPDATE SET
  user_id = EXCLUDED.user_id
RETURNING *;

-- name: UpdateMembership :one
UPDATE memberships
SET
  role = @role,
  updated_at = clock_timestamp()
WHERE id = @id
RETURNING *;

-- name: GetMembership :one
SELECT * FROM memberships WHERE id = @id;

-- ListMemberships keyset-paginates by id with optional (user_id,
-- organization_id) exact-match filters. Either or both narg parameters
-- may be NULL, in which case the corresponding filter is not applied.
-- name: ListMemberships :many
SELECT * FROM memberships
WHERE id > @after
  AND (sqlc.narg('user_id')::uuid IS NULL OR user_id = sqlc.narg('user_id')::uuid)
  AND (sqlc.narg('organization_id')::uuid IS NULL OR organization_id = sqlc.narg('organization_id')::uuid)
ORDER BY id ASC
LIMIT @max_rows;

-- name: DeleteMembership :exec
DELETE FROM memberships WHERE id = @id;

-- =============================================================================
-- current_users (per-mode currentUser; idp-design.md §3, §6.2)
-- =============================================================================

-- name: GetCurrentUser :one
SELECT * FROM current_users WHERE mode = @mode;

-- name: UpsertCurrentUser :one
INSERT INTO current_users (mode, subject_ref)
VALUES (@mode, @subject_ref)
ON CONFLICT (mode) DO UPDATE SET
  subject_ref = EXCLUDED.subject_ref,
  updated_at = clock_timestamp()
RETURNING *;

-- DeleteCurrentUser is the clear path — wipes the row entirely. The next
-- identity-resolving request on the mode will fall through to the
-- default-user bootstrap (idp-design.md §3).
-- name: DeleteCurrentUser :exec
DELETE FROM current_users WHERE mode = @mode;

-- =============================================================================
-- auth_codes / tokens (shared by every OAuth-shaped mode; idp-design.md §5)
-- =============================================================================

-- name: CreateAuthCode :one
INSERT INTO auth_codes (
  code, mode, user_id, client_id, redirect_uri,
  code_challenge, code_challenge_method, scope, expires_at
)
VALUES (
  @code, @mode, @user_id, @client_id, @redirect_uri,
  sqlc.narg('code_challenge'), sqlc.narg('code_challenge_method'),
  sqlc.narg('scope'), @expires_at
)
RETURNING *;

-- ConsumeAuthCode atomically reads-and-deletes an auth code, enforcing
-- single-use. Returns ErrNoRows when the code is unknown for that mode,
-- already consumed, or expired.
-- name: ConsumeAuthCode :one
DELETE FROM auth_codes
WHERE code = @code
  AND mode = @mode
  AND expires_at > clock_timestamp()
RETURNING *;

-- name: CreateToken :one
INSERT INTO tokens (
  token, mode, user_id, client_id, kind, scope, expires_at
)
VALUES (
  @token, @mode, @user_id, @client_id, @kind, sqlc.narg('scope'), @expires_at
)
RETURNING *;

-- name: GetActiveToken :one
SELECT * FROM tokens
WHERE token = @token
  AND mode = @mode
  AND revoked_at IS NULL
  AND expires_at > clock_timestamp();

-- name: RevokeToken :exec
UPDATE tokens
SET revoked_at = clock_timestamp()
WHERE token = @token AND mode = @mode AND revoked_at IS NULL;

-- name: ListOrganizationsForUser :many
SELECT o.* FROM organizations o
JOIN memberships m ON m.organization_id = o.id
WHERE m.user_id = @user_id
ORDER BY o.name ASC;
