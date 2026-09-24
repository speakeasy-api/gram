-- name: ListSlackDirectoryConnections :many
SELECT * FROM slack_directory_connections
WHERE organization_id = @organization_id
ORDER BY created_at, id;

-- name: GetSlackDirectoryConnection :one
SELECT * FROM slack_directory_connections
WHERE organization_id = @organization_id AND id = @id;

-- name: LockSlackDirectoryConnection :one
SELECT * FROM slack_directory_connections
WHERE organization_id = @organization_id AND id = @id
FOR UPDATE;

-- name: LockSlackDirectoryConnectionByTeam :one
SELECT * FROM slack_directory_connections
WHERE organization_id = @organization_id AND slack_team_id = @slack_team_id
FOR UPDATE;

-- name: CreateSlackDirectoryConnection :one
INSERT INTO slack_directory_connections (organization_id, slack_team_id, slack_team_name, credentials_encrypted, granted_scopes, generation, health)
VALUES (@organization_id, @slack_team_id, @slack_team_name, @credentials_encrypted, @granted_scopes, @generation, 'connected')
ON CONFLICT (organization_id, slack_team_id) DO NOTHING
RETURNING *;

-- name: AuthorizeSlackDirectoryConnection :one
UPDATE slack_directory_connections SET
slack_team_name = @slack_team_name,
credentials_encrypted = @credentials_encrypted,
granted_scopes = @granted_scopes,
generation = @generation,
health = 'connected', disconnected_at = NULL, last_error_code = NULL,
updated_at = clock_timestamp()
WHERE organization_id = @organization_id AND id = @id AND generation = @expected_generation AND slack_team_id = @slack_team_id
RETURNING *;

-- name: DisconnectSlackDirectoryConnection :one
UPDATE slack_directory_connections SET
credentials_encrypted = NULL, granted_scopes = '{}', generation = @generation,
health = 'disconnected', disconnected_at = clock_timestamp(), updated_at = clock_timestamp(), last_error_code = NULL
WHERE organization_id = @organization_id AND id = @id AND generation = @expected_generation
RETURNING *;

-- name: TryLockSlackDirectorySync :one
SELECT pg_try_advisory_lock(hashtextextended(@lock_key::text, 0))::boolean AS acquired;

-- name: UnlockSlackDirectorySync :one
SELECT pg_advisory_unlock(hashtextextended(@lock_key::text, 0))::boolean AS released;

-- name: StartSlackDirectorySync :execrows
UPDATE slack_directory_connections
SET last_sync_started_at = @started_at, updated_at = clock_timestamp()
WHERE organization_id = @organization_id AND id = @id AND generation = @generation
  AND disconnected_at IS NULL AND (last_sync_started_at IS NULL OR last_sync_started_at <= @started_at);

-- name: FailSlackDirectorySync :execrows
UPDATE slack_directory_connections
SET last_sync_failed_at = clock_timestamp(), last_error_code = @error_code,
    health = CASE WHEN @reconnect::boolean THEN 'reconnect_required' ELSE health END,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id AND id = @id AND generation = @generation
  AND last_sync_started_at = @started_at AND disconnected_at IS NULL;

-- name: PublishSlackDirectorySync :one
UPDATE slack_directory_connections
SET last_full_sync_generation = generation, last_full_sync_succeeded_at = @published_at,
    last_error_code = NULL, health = 'connected', updated_at = clock_timestamp()
WHERE organization_id = @organization_id AND id = @id AND generation = @generation
RETURNING *;

-- name: MarkAbsentSlackDirectoryMembershipsUnknown :exec
UPDATE slack_directory_memberships SET status = 'unknown', updated_at = clock_timestamp()
WHERE organization_id = @organization_id AND slack_team_id = @slack_team_id
  AND last_seen_at <> @published_at AND status <> 'unknown';

-- name: ListSlackDirectoryConnectionSummaries :many
SELECT sqlc.embed(c),
  (SELECT count(*) FROM slack_directory_memberships m WHERE m.organization_id = c.organization_id
   AND m.slack_team_id = c.slack_team_id AND m.last_seen_at = c.last_full_sync_succeeded_at)::bigint AS member_count
FROM slack_directory_connections c
WHERE c.organization_id = @organization_id
ORDER BY c.created_at, c.id;

-- name: ListSlackDirectoryMembers :many
SELECT m.*, c.id AS connection_id, c.slack_team_name AS workspace_name,
    (m.last_seen_at = c.last_full_sync_succeeded_at)::boolean AS observed_in_last_sync
FROM slack_directory_memberships m
JOIN slack_directory_connections c ON c.organization_id = m.organization_id AND c.slack_team_id = m.slack_team_id
WHERE m.organization_id = @organization_id AND c.disconnected_at IS NULL
  AND (sqlc.narg(connection_id)::uuid IS NULL OR c.id = sqlc.narg(connection_id))
  AND (@search::text = '' OR strpos(lower(coalesce(m.display_name, '')), lower(@search)) > 0
    OR strpos(lower(coalesce(m.email, '')), lower(@search)) > 0 OR strpos(lower(m.slack_user_id), lower(@search)) > 0)
  AND (sqlc.narg(cursor)::uuid IS NULL OR m.id > sqlc.narg(cursor))
ORDER BY m.id
LIMIT @page_size;

-- name: CountSlackDirectoryMembers :one
SELECT count(*)::bigint FROM slack_directory_memberships m
JOIN slack_directory_connections c ON c.organization_id = m.organization_id AND c.slack_team_id = m.slack_team_id
WHERE m.organization_id = @organization_id AND c.disconnected_at IS NULL
  AND (sqlc.narg(connection_id)::uuid IS NULL OR c.id = sqlc.narg(connection_id))
  AND (@search::text = '' OR strpos(lower(coalesce(m.display_name, '')), lower(@search)) > 0
    OR strpos(lower(coalesce(m.email, '')), lower(@search)) > 0 OR strpos(lower(m.slack_user_id), lower(@search)) > 0);

-- name: UpsertSlackDirectoryMembershipBatch :exec
INSERT INTO slack_directory_memberships
(organization_id, slack_team_id, slack_user_id, display_name, email, status, member_type, provider_updated_at, last_seen_at)
SELECT @organization_id, @slack_team_id, u.user_id, nullif(u.display_name, ''), nullif(u.email, ''), u.status, u.member_type, u.provider_updated_at, @last_seen_at
FROM (SELECT unnest(@user_ids::text[]) AS user_id,
 unnest(@display_names::text[]) AS display_name, unnest(@emails::text[]) AS email,
 unnest(@statuses::text[]) AS status, unnest(@member_types::text[]) AS member_type,
 unnest(@provider_updated_ats::timestamptz[]) AS provider_updated_at) u
ON CONFLICT (organization_id, slack_team_id, slack_user_id) DO UPDATE SET
    display_name = EXCLUDED.display_name, email = EXCLUDED.email, status = EXCLUDED.status,
    member_type = EXCLUDED.member_type, provider_updated_at = EXCLUDED.provider_updated_at,
    last_seen_at = EXCLUDED.last_seen_at, updated_at = clock_timestamp();

-- name: CreateSlackMappingForTest :one
INSERT INTO slack_identity_mappings (organization_id, slack_team_id, slack_user_id, user_id)
VALUES (@organization_id, @slack_team_id, @slack_user_id, @user_id)
RETURNING *;

-- name: SetSlackMappingRevisionForTest :exec
UPDATE slack_directory_memberships SET mapping_revision = @mapping_revision,
 mapping_conflict_reason = 'test_review', mapping_conflict_detected_at = clock_timestamp()
WHERE organization_id = @organization_id AND id = @id;

-- name: GetSlackMappingForTest :one
SELECT * FROM slack_identity_mappings WHERE organization_id = @organization_id AND id = @id;

-- name: CountSlackDirectorySnapshotMembers :one
SELECT count(*)::bigint
FROM slack_directory_memberships m
JOIN slack_directory_connections c ON c.organization_id = m.organization_id AND c.slack_team_id = m.slack_team_id
WHERE c.organization_id = @organization_id AND c.id = @id
 AND m.last_seen_at = c.last_full_sync_succeeded_at;

-- name: DeleteSlackDirectoryMemberships :exec
-- Disconnect forgets the workspace directory; connecting again starts a fresh sync.
DELETE FROM slack_directory_memberships WHERE organization_id = @organization_id AND slack_team_id = @slack_team_id;

-- name: ResetSlackDirectorySnapshot :exec
UPDATE slack_directory_connections SET last_full_sync_generation = NULL, last_full_sync_succeeded_at = NULL, updated_at = clock_timestamp()
WHERE organization_id = @organization_id AND id = @id;
