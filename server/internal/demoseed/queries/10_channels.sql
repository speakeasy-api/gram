-- Channels: the organization-scoped conversation surface. Its rows hang off
-- the organization's canonical default project and ride that project's delete
-- cascade; the deletes below are explicit anyway to keep the reseed readable.
-- channels_state needs its own for a real reason: its project foreign key is
-- composite and project_id is nullable, so a row that never recorded a project
-- escapes the cascade (MATCH SIMPLE does not enforce a partially NULL key).

-- name: ChannelsCountStrayState :one
-- The superseded Channels document is NOT the seed's to destroy. A developer
-- may still be holding proof-of-concept conversations in it, and a reseed is
-- not consent to delete them. The caller refuses the run when this is
-- positive, and names the command that carries them forward.
SELECT count(*) FROM channels_state
WHERE organization_id = @org_id::text
  AND (
    jsonb_array_length(COALESCE(state -> 'messages', '[]'::jsonb)) > 0
    OR EXISTS (
      SELECT 1
      FROM (VALUES ('instances'), ('threads'), ('preferences'), ('grants')) AS maps(key)
      WHERE jsonb_typeof(state -> maps.key) = 'object'
        AND state -> maps.key <> '{}'::jsonb
    )
  );

-- name: ChannelsDeleteMessages :exec
DELETE FROM channels_messages WHERE organization_id = @org_id::text;

-- name: ChannelsDeleteConversationEndpoints :exec
DELETE FROM channels_conversation_endpoints WHERE organization_id = @org_id::text;

-- name: ChannelsDeleteConversationParticipants :exec
DELETE FROM channels_conversation_participants WHERE organization_id = @org_id::text;

-- name: ChannelsDeleteThreadPreferences :exec
DELETE FROM channels_thread_preferences WHERE organization_id = @org_id::text;

-- name: ChannelsDeleteConversations :exec
DELETE FROM channels_conversations WHERE organization_id = @org_id::text;

-- name: ChannelsDeleteInstances :exec
DELETE FROM channels_instances WHERE organization_id = @org_id::text;

-- name: ChannelsDeleteAccessExceptions :exec
DELETE FROM channels_access_exceptions WHERE organization_id = @org_id::text;

-- name: ChannelsDeleteSettings :exec
DELETE FROM channels_settings WHERE organization_id = @org_id::text;

-- name: ChannelsDeleteState :exec
-- Only an empty placeholder document is removed; ChannelsCountStrayState has
-- already refused anything with content in it. A row whose project_id is NULL
-- is not reached by the projects cascade, so it would otherwise survive.
DELETE FROM channels_state WHERE organization_id = @org_id::text;

-- name: ChannelsInsertSettings :exec
INSERT INTO channels_settings (organization_id, project_id, enabled, message_seq, announce_seq)
VALUES (@org_id::text, @project_id::uuid, TRUE, @message_seq::bigint, @announce_seq::bigint);

-- name: ChannelsInsertInstance :exec
-- Synthetic instances reuse existing parent identities. They are leased
-- endpoints, not new accounts, and they do not represent live processes.
INSERT INTO channels_instances
  (id, organization_id, project_id, subject_id, label, note, origin, announce_seq, expires_at)
VALUES
  (@id::uuid, @org_id::text, @project_id::uuid, @subject_id::text, @label::text,
   @note::text, '', @announce_seq::bigint, @expires_at::timestamptz);

-- name: ChannelsInsertConversation :exec
INSERT INTO channels_conversations
  (id, organization_id, project_id, title, private, endpoint_pair_key,
   created_by_subject_id, announce_seq, last_message_seq, message_count)
VALUES
  (@id::uuid, @org_id::text, @project_id::uuid, @title::text, @private::boolean,
   sqlc.narg('endpoint_pair_key')::text, @created_by_subject_id::text,
   @announce_seq::bigint, @last_message_seq::bigint, @message_count::bigint);

-- name: ChannelsInsertConversationParticipant :exec
INSERT INTO channels_conversation_participants
  (organization_id, project_id, conversation_id, subject_id)
VALUES (@org_id::text, @project_id::uuid, @conversation_id::uuid, @subject_id::text);

-- name: ChannelsInsertConversationEndpoint :exec
INSERT INTO channels_conversation_endpoints
  (organization_id, project_id, conversation_id, instance_id)
VALUES (@org_id::text, @project_id::uuid, @conversation_id::uuid, @instance_id::uuid);

-- name: ChannelsInsertMessage :exec
INSERT INTO channels_messages
  (id, organization_id, project_id, conversation_id, instance_id, subject_id,
   seq, body, idempotency_key, created_at)
VALUES
  (@id::uuid, @org_id::text, @project_id::uuid, @conversation_id::uuid,
   @instance_id::uuid, @subject_id::text, @seq::bigint, @body::text,
   @idempotency_key::text, @created_at::timestamptz);

-- name: ChannelsInsertThreadPreference :exec
-- A parent that follows a thread is what makes its messages notify an agent
-- that also subscribes to channels://messages.
INSERT INTO channels_thread_preferences
  (organization_id, project_id, subject_id, conversation_id, followed, muted)
VALUES (@org_id::text, @project_id::uuid, @subject_id::text, @conversation_id::uuid,
        @followed::boolean, @muted::boolean);

-- name: PostflightCountChannelsEnabledSettings :one
SELECT count(*) FROM channels_settings
WHERE organization_id = @org_id::text AND enabled IS TRUE;

-- name: PostflightCountChannelsMessages :one
SELECT count(*) FROM channels_messages
WHERE organization_id = @org_id::text AND deleted IS FALSE;

-- name: PostflightCountChannelsPrivateConversations :one
SELECT count(*) FROM channels_conversations
WHERE organization_id = @org_id::text AND private IS TRUE AND deleted IS FALSE;

-- name: PostflightCountChannelsMessagesOutsideDefaultProject :one
-- Channels rows must sit in the organization's canonical default project.
SELECT count(*)
FROM channels_messages m JOIN projects p ON p.id = m.project_id
WHERE m.organization_id = @org_id::text AND p.slug <> 'default';
