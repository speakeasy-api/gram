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

-- name: GetDirectoryUserByWorkOSID :one
SELECT *
FROM directory_users
WHERE workos_directory_user_id = @workos_directory_user_id
  AND deleted_at IS NULL;

-- name: GetDirectoryUserIDByWorkOSID :one
SELECT user_id
FROM directory_users
WHERE workos_directory_user_id = @workos_directory_user_id
  AND user_id IS NOT NULL
  AND deleted_at IS NULL;

-- name: GetDirectoryUserIDByOrganizationEmail :one
SELECT user_id
FROM directory_users
WHERE organization_id = @organization_id
  AND email = @email
  AND user_id IS NOT NULL
  AND deleted_at IS NULL
LIMIT 1;

-- name: SetUserSyncLastEventID :one
INSERT INTO workos_user_syncs (workos_user_id, last_event_id)
VALUES (@workos_user_id, @last_event_id)
ON CONFLICT (workos_user_id) WHERE workos_user_id IS NOT NULL DO UPDATE SET
    last_event_id = EXCLUDED.last_event_id,
    updated_at = clock_timestamp()
RETURNING id;

-- name: UpsertDirectoryUser :execrows
INSERT INTO directory_users (
    organization_id,
    user_id,
    workos_directory_user_id,
    email,
    attributes,
    attributes_content_hash,
    deleted_at
)
VALUES (
    @organization_id,
    @user_id,
    @workos_directory_user_id,
    @email,
    @attributes,
    @attributes_content_hash,
    NULL
)
ON CONFLICT (workos_directory_user_id) DO UPDATE SET
    organization_id = EXCLUDED.organization_id,
    user_id = COALESCE(EXCLUDED.user_id, directory_users.user_id),
    email = EXCLUDED.email,
    attributes = EXCLUDED.attributes,
    attributes_content_hash = EXCLUDED.attributes_content_hash,
    deleted_at = NULL,
    updated_at = clock_timestamp()
WHERE directory_users.organization_id IS DISTINCT FROM EXCLUDED.organization_id
   OR directory_users.user_id IS DISTINCT FROM COALESCE(EXCLUDED.user_id, directory_users.user_id)
   OR directory_users.email IS DISTINCT FROM EXCLUDED.email
   OR directory_users.attributes_content_hash IS DISTINCT FROM EXCLUDED.attributes_content_hash
   OR directory_users.deleted_at IS NOT NULL;

-- name: LinkDirectoryUserToUser :execrows
UPDATE directory_users
SET user_id = @user_id,
    updated_at = clock_timestamp()
WHERE workos_directory_user_id = @workos_directory_user_id
  AND user_id IS DISTINCT FROM @user_id;

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
