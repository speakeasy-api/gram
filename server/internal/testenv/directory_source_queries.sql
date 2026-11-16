-- Shared directory lifecycle fixtures for event handling and attribution tests.

-- name: SeedDirectoryUsersFixture :execrows
INSERT INTO directory_users (organization_id, workos_directory_user_id, directory_id, workos_created_at, workos_updated_at)
SELECT @organization_id, @id_prefix::text || n, sqlc.narg(directory_id)::text, @workos_created_at, @workos_created_at
FROM generate_series(1, @row_count::integer) n;

-- name: SeedDirectoryGroupsFixture :execrows
INSERT INTO directory_groups (organization_id, workos_directory_group_id, directory_id, name, workos_created_at, workos_updated_at)
SELECT @organization_id, @id_prefix::text || n, sqlc.narg(directory_id)::text, 'Group ' || n, @workos_created_at, @workos_created_at
FROM generate_series(1, @row_count::integer) n;

-- name: CountDeletedDirectorySourcesFixture :one
SELECT
  (SELECT COUNT(*) FROM directory_users du WHERE du.organization_id = @organization_id AND du.directory_id = @directory_id::text AND du.workos_deleted IS TRUE) AS users,
  (SELECT COUNT(*) FROM directory_groups dg WHERE dg.organization_id = @organization_id AND dg.directory_id = @directory_id::text AND dg.workos_deleted IS TRUE) AS groups;

-- name: GetDirectoryUserLifecycleFixture :one
SELECT directory_id, deleted, workos_deleted, workos_last_event_id
FROM directory_users
WHERE organization_id = @organization_id AND workos_directory_user_id = @workos_directory_user_id;

-- name: GetDirectoryGroupLifecycleFixture :one
SELECT id, directory_id, deleted, workos_deleted, workos_last_event_id
FROM directory_groups
WHERE organization_id = @organization_id AND workos_directory_group_id = @workos_directory_group_id;
