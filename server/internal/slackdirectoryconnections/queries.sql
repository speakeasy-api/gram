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

-- name: LockSlackDirectoryMembershipsForPublication :many
-- Acquire locks in a separate statement so later mapping reads use a fresh snapshot.
SELECT id FROM slack_directory_memberships
WHERE organization_id = @organization_id AND slack_team_id = @slack_team_id
ORDER BY id FOR UPDATE;

-- name: MarkAbsentSlackDirectoryMembershipsUnknown :exec
UPDATE slack_directory_memberships m SET status = 'unknown', updated_at = clock_timestamp(),
 mapping_conflict_reason = CASE WHEN EXISTS (SELECT 1 FROM slack_identity_mappings im WHERE im.organization_id = m.organization_id AND im.slack_team_id = m.slack_team_id AND im.slack_user_id = m.slack_user_id AND im.revoked_at IS NULL)
 THEN coalesce(m.mapping_conflict_reason, 'member_absent') ELSE m.mapping_conflict_reason END,
 mapping_conflict_detected_at = CASE WHEN EXISTS (SELECT 1 FROM slack_identity_mappings im WHERE im.organization_id = m.organization_id AND im.slack_team_id = m.slack_team_id AND im.slack_user_id = m.slack_user_id AND im.revoked_at IS NULL)
 THEN coalesce(m.mapping_conflict_detected_at, clock_timestamp()) ELSE m.mapping_conflict_detected_at END
WHERE m.organization_id = @organization_id AND m.slack_team_id = @slack_team_id
 AND m.last_seen_at <> @published_at
 AND (m.status <> 'unknown' OR m.last_seen_at = sqlc.narg(previous_published_at));

-- name: ListSlackDirectoryConnectionSummaries :many
SELECT sqlc.embed(c),
  (SELECT count(*) FROM slack_directory_memberships m WHERE m.organization_id = c.organization_id
   AND m.slack_team_id = c.slack_team_id AND m.last_seen_at = c.last_full_sync_succeeded_at)::bigint AS member_count
FROM slack_directory_connections c
WHERE c.organization_id = @organization_id
ORDER BY c.created_at, c.id;

-- name: ListSlackDirectoryMembers :many
SELECT m.*, coalesce(im.id::text, '')::text AS mapping_id, coalesce(im.user_id, '')::text AS mapped_user_id,
 coalesce(u.display_name, '')::text AS mapped_display_name, coalesce(u.email, '')::text AS mapped_email,
 u.photo_url AS mapped_photo_url, (im.id IS NOT NULL AND u.deleted_at IS NULL AND our.deleted_at IS NULL)::boolean AS mapped_user_active,
 c.id AS connection_id, c.slack_team_name AS workspace_name,
    (m.last_seen_at = c.last_full_sync_succeeded_at)::boolean AS observed_in_last_sync
FROM slack_directory_memberships m
JOIN slack_directory_connections c ON c.organization_id = m.organization_id AND c.slack_team_id = m.slack_team_id
LEFT JOIN slack_identity_mappings im ON im.organization_id = m.organization_id AND im.slack_team_id = m.slack_team_id AND im.slack_user_id = m.slack_user_id AND im.revoked_at IS NULL
LEFT JOIN users u ON u.id = im.user_id
LEFT JOIN organization_user_relationships our ON our.organization_id = m.organization_id AND our.user_id = im.user_id
WHERE m.organization_id = @organization_id AND c.disconnected_at IS NULL
 AND (@mapping_status::text = '' OR CASE WHEN im.id IS NULL THEN 'unmapped' WHEN m.mapping_conflict_reason IS NOT NULL OR u.deleted_at IS NOT NULL OR our.deleted_at IS NOT NULL THEN 'needs_review' ELSE 'mapped' END = @mapping_status)
  AND (sqlc.narg(connection_id)::uuid IS NULL OR c.id = sqlc.narg(connection_id))
  AND (@search::text = '' OR strpos(lower(coalesce(m.display_name, '')), lower(@search)) > 0
    OR strpos(lower(coalesce(m.email, '')), lower(@search)) > 0 OR strpos(lower(m.slack_user_id), lower(@search)) > 0)
  AND (sqlc.narg(member_id)::uuid IS NULL OR m.id = sqlc.narg(member_id))
  AND (sqlc.narg(cursor)::uuid IS NULL OR m.id > sqlc.narg(cursor))
ORDER BY m.id
LIMIT @page_size;

-- name: CountSlackDirectoryMembers :one
SELECT count(*)::bigint FROM slack_directory_memberships m
JOIN slack_directory_connections c ON c.organization_id = m.organization_id AND c.slack_team_id = m.slack_team_id
LEFT JOIN slack_identity_mappings im ON im.organization_id = m.organization_id AND im.slack_team_id = m.slack_team_id AND im.slack_user_id = m.slack_user_id AND im.revoked_at IS NULL
LEFT JOIN users u ON u.id = im.user_id
LEFT JOIN organization_user_relationships our ON our.organization_id = m.organization_id AND our.user_id = im.user_id
WHERE m.organization_id = @organization_id AND c.disconnected_at IS NULL
 AND (@mapping_status::text = '' OR CASE WHEN im.id IS NULL THEN 'unmapped' WHEN m.mapping_conflict_reason IS NOT NULL OR u.deleted_at IS NOT NULL OR our.deleted_at IS NOT NULL THEN 'needs_review' ELSE 'mapped' END = @mapping_status)
  AND (sqlc.narg(connection_id)::uuid IS NULL OR c.id = sqlc.narg(connection_id))
  AND (@search::text = '' OR strpos(lower(coalesce(m.display_name, '')), lower(@search)) > 0
    OR strpos(lower(coalesce(m.email, '')), lower(@search)) > 0 OR strpos(lower(m.slack_user_id), lower(@search)) > 0);

-- name: UpsertSlackDirectoryMembershipBatch :exec
WITH observations AS (
 SELECT unnest(@user_ids::text[]) AS user_id,
 unnest(@display_names::text[]) AS display_name, unnest(@emails::text[]) AS email,
 unnest(@statuses::text[]) AS status, unnest(@member_types::text[]) AS member_type,
 unnest(@provider_updated_ats::timestamptz[]) AS provider_updated_at
), findings AS (
 SELECT u.*, CASE WHEN im.id IS NULL THEN NULL
 WHEN m.member_type IS DISTINCT FROM u.member_type AND u.member_type = 'bot' THEN 'member_became_bot'
 WHEN m.member_type IS DISTINCT FROM u.member_type AND u.member_type = 'unknown' THEN 'member_type_unknown'
 WHEN m.status IS DISTINCT FROM u.status AND u.status = 'deactivated' THEN 'member_deactivated'
 WHEN m.status IS DISTINCT FROM u.status AND u.status = 'unknown' THEN 'member_unknown'
 WHEN nullif(trim(m.email), '') IS NOT NULL AND nullif(trim(u.email), '') IS NOT NULL
  AND lower(trim(m.email)) <> lower(trim(u.email)) THEN 'email_changed'
 ELSE NULL END AS reason
 FROM observations u
 LEFT JOIN slack_directory_memberships m ON m.organization_id = @organization_id AND m.slack_team_id = @slack_team_id AND m.slack_user_id = u.user_id
 LEFT JOIN slack_identity_mappings im ON im.organization_id = m.organization_id AND im.slack_team_id = m.slack_team_id AND im.slack_user_id = m.slack_user_id AND im.revoked_at IS NULL
)
INSERT INTO slack_directory_memberships
(organization_id, slack_team_id, slack_user_id, display_name, email, status, member_type, provider_updated_at, last_seen_at, mapping_conflict_reason, mapping_conflict_detected_at)
SELECT @organization_id, @slack_team_id, user_id, nullif(display_name, ''), nullif(email, ''), status, member_type, provider_updated_at, @last_seen_at, reason,
 CASE WHEN reason IS NOT NULL THEN clock_timestamp() ELSE NULL END FROM findings
ON CONFLICT (organization_id, slack_team_id, slack_user_id) DO UPDATE SET
 display_name = EXCLUDED.display_name, email = EXCLUDED.email, status = EXCLUDED.status,
 member_type = EXCLUDED.member_type, provider_updated_at = EXCLUDED.provider_updated_at,
 last_seen_at = EXCLUDED.last_seen_at, updated_at = clock_timestamp(),
 mapping_conflict_reason = coalesce(slack_directory_memberships.mapping_conflict_reason, EXCLUDED.mapping_conflict_reason),
 mapping_conflict_detected_at = coalesce(slack_directory_memberships.mapping_conflict_detected_at, EXCLUDED.mapping_conflict_detected_at);

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

-- name: UpdateSlackDirectoryCredentials :execrows
UPDATE slack_directory_connections SET credentials_encrypted = @credentials_encrypted, updated_at = clock_timestamp()
WHERE organization_id = @organization_id AND id = @id AND generation = @generation AND disconnected_at IS NULL;

-- name: ListDueSlackDirectorySyncs :many
-- Connected workspaces whose last sync started before the cutoff, oldest first.
SELECT organization_id, id, generation FROM slack_directory_connections
WHERE disconnected_at IS NULL AND health = 'connected' AND credentials_encrypted IS NOT NULL
  AND organization_id <> @excluded_organization_id
  AND (last_sync_started_at IS NULL OR last_sync_started_at < @started_before)
  -- Back off a workspace whose latest attempt failed, so a permanent error is not retried every tick.
  AND NOT (last_sync_failed_at IS NOT NULL AND last_sync_failed_at > coalesce(last_full_sync_succeeded_at, '-infinity'::timestamptz) AND last_sync_failed_at > @failed_after)
ORDER BY last_sync_started_at ASC NULLS FIRST, id
LIMIT @max_rows;

-- name: LockSlackDirectoryMembership :one
SELECT * FROM slack_directory_memberships WHERE organization_id = @organization_id AND id = @id FOR UPDATE;

-- name: RevokeSlackIdentityMapping :exec
UPDATE slack_identity_mappings SET revoked_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE organization_id = @organization_id AND slack_team_id = @slack_team_id AND slack_user_id = @slack_user_id AND revoked_at IS NULL;

-- name: ConfirmSlackIdentityMapping :exec
INSERT INTO slack_identity_mappings (organization_id, slack_team_id, slack_user_id, user_id)
VALUES (@organization_id, @slack_team_id, @slack_user_id, @user_id);

-- name: AdvanceSlackMappingRevision :exec
UPDATE slack_directory_memberships SET mapping_revision = mapping_revision + 1,
 mapping_conflict_reason = NULL, mapping_conflict_detected_at = NULL, updated_at = clock_timestamp()
WHERE organization_id = @organization_id AND id = @id;

-- name: CreateSlackMappingPersonForTest :exec
WITH person AS (
 INSERT INTO users (id, email, display_name) VALUES (@user_id, @user_id::text || '@demo.getgram.ai', 'Synthetic Person') RETURNING id
)
INSERT INTO organization_user_relationships (organization_id, user_id)
SELECT @organization_id, id FROM person;

-- name: DeactivateSlackMappingPersonForTest :exec
UPDATE organization_user_relationships SET deleted_at = clock_timestamp()
WHERE organization_id = @organization_id AND user_id = @user_id;

-- name: DeleteSlackMappingUserForTest :exec
UPDATE users SET deleted_at = clock_timestamp() WHERE id = @user_id;
