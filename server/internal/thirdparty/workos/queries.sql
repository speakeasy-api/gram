-- name: GetOrganizationSyncLastEventID :one
SELECT last_event_id
FROM workos_organization_syncs
WHERE workos_organization_id = @workos_organization_id;

-- name: SetOrganizationSyncLastEventID :one
INSERT INTO workos_organization_syncs (workos_organization_id, last_event_id)
VALUES (@workos_organization_id, @last_event_id)
ON CONFLICT (workos_organization_id) DO UPDATE SET
    last_event_id = EXCLUDED.last_event_id,
    updated_at = clock_timestamp()
RETURNING id;

-- name: GetUserSyncLastEventID :one
SELECT last_event_id
FROM workos_user_syncs
WHERE workos_user_id = @workos_user_id;

-- name: SetUserSyncLastEventID :one
INSERT INTO workos_user_syncs (workos_user_id, last_event_id)
VALUES (@workos_user_id, @last_event_id)
ON CONFLICT (workos_user_id) WHERE workos_user_id IS NOT NULL DO UPDATE SET
    last_event_id = EXCLUDED.last_event_id,
    updated_at = clock_timestamp()
RETURNING id;

-- name: UpsertDirectoryGroup :one
INSERT INTO directory_groups (
  organization_id,
  workos_directory_group_id,
  name,
  attributes,
  deleted_at
)
VALUES (
  @organization_id,
  @workos_directory_group_id,
  @name,
  @attributes,
  NULL
)
ON CONFLICT (workos_directory_group_id) DO UPDATE SET
  organization_id = EXCLUDED.organization_id,
  name = EXCLUDED.name,
  attributes = EXCLUDED.attributes,
  deleted_at = NULL,
  updated_at = clock_timestamp()
RETURNING id;

-- name: GetDirectoryGroupIDByWorkOSID :one
SELECT id
FROM directory_groups
WHERE workos_directory_group_id = @workos_directory_group_id;

-- name: GetDirectoryGroupForMembershipByWorkOSID :one
SELECT id, organization_id
FROM directory_groups
WHERE workos_directory_group_id = @workos_directory_group_id;

-- name: GetDirectoryGroupByWorkOSID :one
SELECT organization_id, name, attributes, deleted
FROM directory_groups
WHERE workos_directory_group_id = @workos_directory_group_id;

-- name: DeleteDirectoryGroupByWorkOSID :execrows
UPDATE directory_groups
SET deleted_at = COALESCE(deleted_at, clock_timestamp()),
  updated_at = clock_timestamp()
WHERE workos_directory_group_id = @workos_directory_group_id
  AND deleted_at IS NULL;

-- name: UpsertDirectoryUser :one
INSERT INTO directory_users (
  organization_id,
  user_id,
  workos_directory_user_id,
  email,
  attributes,
  deleted_at
)
VALUES (
  @organization_id,
  @user_id,
  @workos_directory_user_id,
  @email,
  @attributes,
  NULL
)
ON CONFLICT (workos_directory_user_id) DO UPDATE SET
  organization_id = EXCLUDED.organization_id,
  user_id = COALESCE(EXCLUDED.user_id, directory_users.user_id),
  email = EXCLUDED.email,
  attributes = EXCLUDED.attributes,
  deleted_at = NULL,
  updated_at = clock_timestamp()
RETURNING id;

-- name: GetDirectoryUserByWorkOSID :one
SELECT *
FROM directory_users
WHERE workos_directory_user_id = @workos_directory_user_id
  AND deleted_at IS NULL;

-- name: GetDirectoryUserIDByWorkOSID :one
SELECT id
FROM directory_users
WHERE workos_directory_user_id = @workos_directory_user_id
  AND deleted_at IS NULL;

-- name: LinkDirectoryUsersToUserByEmail :execrows
UPDATE directory_users
SET user_id = @user_id,
  updated_at = clock_timestamp()
WHERE email = @email
  AND user_id IS NULL
  AND deleted_at IS NULL;

-- name: DeleteDirectoryUserByWorkOSID :execrows
UPDATE directory_users
SET deleted_at = COALESCE(deleted_at, clock_timestamp()),
  updated_at = clock_timestamp()
WHERE workos_directory_user_id = @workos_directory_user_id
  AND deleted_at IS NULL;

-- name: OpenDirectoryUserGroupMembership :one
INSERT INTO directory_user_group_memberships (
  directory_user_id,
  directory_group_id,
  workos_directory_user_id,
  workos_directory_group_id
)
VALUES (
  @directory_user_id,
  @directory_group_id,
  @workos_directory_user_id,
  @workos_directory_group_id
)
ON CONFLICT (directory_user_id, directory_group_id) WHERE deleted IS FALSE DO UPDATE SET
  workos_directory_user_id = EXCLUDED.workos_directory_user_id,
  workos_directory_group_id = EXCLUDED.workos_directory_group_id,
  deleted_at = NULL,
  updated_at = clock_timestamp()
RETURNING id;

-- name: CloseDirectoryUserGroupMembership :execrows
UPDATE directory_user_group_memberships
SET deleted_at = COALESCE(deleted_at, clock_timestamp()),
  updated_at = clock_timestamp()
WHERE directory_user_id = @directory_user_id
  AND directory_group_id = @directory_group_id
  AND deleted_at IS NULL;

-- name: CloseDirectoryUserGroupMembershipByWorkOSIDs :one
UPDATE directory_user_group_memberships
SET deleted_at = COALESCE(deleted_at, clock_timestamp()),
  updated_at = clock_timestamp()
WHERE workos_directory_user_id = @workos_directory_user_id
  AND workos_directory_group_id = @workos_directory_group_id
  AND deleted_at IS NULL
RETURNING id;

-- name: CloseDirectoryUserGroupMembershipsForGroup :execrows
UPDATE directory_user_group_memberships
SET deleted_at = COALESCE(deleted_at, clock_timestamp()),
  updated_at = clock_timestamp()
WHERE directory_group_id = @directory_group_id
  AND deleted_at IS NULL;

-- name: CountDirectoryUserGroupMembershipsByWorkOSIDs :one
SELECT COUNT(*)
FROM directory_user_group_memberships
WHERE workos_directory_group_id = @workos_directory_group_id
  AND workos_directory_user_id = @workos_directory_user_id
  AND deleted_at IS NULL;

-- name: GetDirectoryUserAttributesByUserID :one
SELECT attributes
FROM directory_users
WHERE user_id = @user_id
  AND organization_id = @organization_id
  AND deleted_at IS NULL
ORDER BY updated_at DESC
LIMIT 1;

-- name: ListCurrentDirectoryGroupsByUserID :many
SELECT DISTINCT ON (dg.workos_directory_group_id)
  dg.workos_directory_group_id,
  dg.name
FROM directory_users AS du
JOIN directory_user_group_memberships AS m
  ON m.directory_user_id = du.id
  AND m.deleted_at IS NULL
JOIN directory_groups AS dg
  ON dg.id = m.directory_group_id
  AND dg.deleted_at IS NULL
WHERE du.user_id = @user_id
  AND du.organization_id = @organization_id
  AND du.deleted_at IS NULL
ORDER BY dg.workos_directory_group_id;
