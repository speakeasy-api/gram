-- name: SeedPlatformChatEvidenceParticipantFixture :exec
-- Synthetic participant snapshots for transcript authorization/attribution tests.
INSERT INTO chat_message_participants(project_id,chat_id,message_id,provider,provider_user_id,provider_team_id,user_id,display_name)
VALUES(sqlc.arg('project_id')::uuid,sqlc.arg('chat_id')::uuid,sqlc.arg('message_id')::uuid,
 sqlc.arg('provider')::text,sqlc.arg('provider_user_id')::text,
 nullif(sqlc.arg('provider_team_id')::text,''),nullif(sqlc.arg('user_id')::text,''),nullif(sqlc.arg('display_name')::text,''));

-- name: SeedPlatformChatEvidenceDirectoryFixture :exec
-- Synthetic directory/mapping state, without credentials or provider requests.
WITH person AS (
 INSERT INTO users(id,email,display_name)
 SELECT sqlc.arg('mapped_user_id')::text,'fixture@example.test','Fixture user'
 WHERE sqlc.arg('mapped_user_id')::text<>'' ON CONFLICT(id) DO NOTHING
), relationship AS (
 INSERT INTO organization_user_relationships(organization_id,user_id)
 SELECT sqlc.arg('organization_id')::text,sqlc.arg('mapped_user_id')::text
 WHERE sqlc.arg('mapped_user_id')::text<>'' ON CONFLICT(organization_id,user_id) DO NOTHING
), connection AS (
 INSERT INTO slack_directory_connections(organization_id,slack_team_id)
 VALUES(sqlc.arg('organization_id')::text,sqlc.arg('team_id')::text)
), membership AS (
 INSERT INTO slack_directory_memberships(organization_id,slack_team_id,slack_user_id,display_name,last_seen_at)
 VALUES(sqlc.arg('organization_id')::text,sqlc.arg('team_id')::text,sqlc.arg('provider_user_id')::text,sqlc.arg('display_name')::text,sqlc.arg('observed_at')::timestamptz)
)
INSERT INTO slack_identity_mappings(organization_id,slack_team_id,slack_user_id,user_id)
SELECT sqlc.arg('organization_id')::text,sqlc.arg('team_id')::text,sqlc.arg('provider_user_id')::text,sqlc.arg('mapped_user_id')::text
WHERE sqlc.arg('mapped_user_id')::text<>'';

-- name: SetPlatformChatEvidenceDirectoryStateFixture :exec
-- Independently exercise invalidation states and restore the synthetic fixture.
WITH membership AS (
 UPDATE slack_directory_memberships SET mapping_conflict_reason=CASE WHEN sqlc.arg('state')::text='conflict' THEN 'fixture_conflict' END
 WHERE organization_id=sqlc.arg('organization_id')::text
), person AS (
 UPDATE users SET deleted_at=CASE WHEN sqlc.arg('state')::text='deleted_user' THEN clock_timestamp() END
 WHERE id=sqlc.arg('user_id')::text
), relationship AS (
 UPDATE organization_user_relationships SET deleted_at=CASE WHEN sqlc.arg('state')::text='deleted_membership' THEN clock_timestamp() END
 WHERE organization_id=sqlc.arg('organization_id')::text AND user_id=sqlc.arg('user_id')::text
), mapping AS (
 UPDATE slack_identity_mappings SET revoked_at=CASE WHEN sqlc.arg('state')::text='revoked_mapping' THEN clock_timestamp() END
 WHERE organization_id=sqlc.arg('organization_id')::text
)
UPDATE slack_directory_connections SET disconnected_at=CASE WHEN sqlc.arg('state')::text='disconnected'
 AND (sqlc.arg('team_id')::text='' OR slack_team_id=sqlc.arg('team_id')) THEN clock_timestamp() END
WHERE organization_id=sqlc.arg('organization_id')::text;

-- name: SeedPlatformChatEvidenceParticipantBatchFixture :exec
-- A dense synthetic attribution page proves bounded work and continuation.
WITH messages AS (
 INSERT INTO chat_messages(chat_id,project_id,role,content,created_at)
 SELECT sqlc.arg('chat_id')::uuid,sqlc.arg('project_id')::uuid,'user','attribution_fixture',sqlc.arg('at')::timestamptz
 FROM generate_series(1,sqlc.arg('message_count')::integer) RETURNING id
)
INSERT INTO chat_message_participants(project_id,chat_id,message_id,provider,provider_user_id)
SELECT sqlc.arg('project_id')::uuid,sqlc.arg('chat_id')::uuid,m.id,'fixture','EXAMPLE' || n::text
FROM messages m CROSS JOIN generate_series(0,sqlc.arg('participant_count')::integer-1) n;
