-- Channels data is isolated by the authenticated organization and project.

-- name: EnsureChannelsSettings :one
-- Creates settings only for the exact authenticated organization/project pair.
INSERT INTO channels_settings (organization_id, project_id)
SELECT @organization_id, project.id FROM projects project
WHERE project.organization_id = @organization_id
  AND project.id = @project_id::uuid
  AND project.deleted IS FALSE
ON CONFLICT (organization_id, project_id) DO NOTHING
RETURNING *;

-- name: GetChannelsSettings :one
SELECT * FROM channels_settings WHERE organization_id = @organization_id AND project_id = @project_id::uuid;

-- name: LockChannelsSettings :one
-- Serializes the project's live-lease admission: the row lock is held
-- until the calling transaction ends.
SELECT organization_id FROM channels_settings
WHERE organization_id = @organization_id AND project_id = @project_id::uuid
FOR UPDATE;

-- name: SetChannelsEnabled :one
UPDATE channels_settings
SET enabled = @enabled, updated_at = clock_timestamp()
WHERE organization_id = @organization_id AND project_id = @project_id::uuid
RETURNING *;

-- name: AllocateMessageSeq :one
-- Allocates the next cursor position. The row write lock this takes is held
-- until the posting transaction commits, so sequence order and commit order
-- agree and a reader paging on `seq >` can never skip a late commit. Call it
-- as the last step of the transaction to keep the serialized window short.
UPDATE channels_settings
SET message_seq = message_seq + 1, updated_at = clock_timestamp()
WHERE organization_id = @organization_id AND project_id = @project_id::uuid
RETURNING message_seq;

-- name: AllocateAnnounceSeq :one
UPDATE channels_settings
SET announce_seq = announce_seq + 1, updated_at = clock_timestamp()
WHERE organization_id = @organization_id AND project_id = @project_id::uuid
RETURNING announce_seq;

-- name: RaiseRetentionFloor :one
UPDATE channels_settings
SET retention_floor_seq = GREATEST(retention_floor_seq, @floor_seq::bigint),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id AND project_id = @project_id::uuid
RETURNING retention_floor_seq;

-- name: ListChannelsOrganizationsNeedingRetention :many
-- Keyset page over the organization/project scopes that actually have something to retire,
-- so a sweep costs one indexed query instead of two per project that has
-- ever used Channels. Both EXISTS clauses ride the (organization_id,
-- created_at) and (organization_id, expires_at) indexes. The sweep time is
-- bound once in a CTE: the query generator mangles a named parameter that
-- appears more than once inside correlated subqueries.
WITH sweep AS (
    SELECT @now::timestamptz AS at
)
SELECT settings.organization_id,
       settings.project_id,
       settings.message_retention_days,
       settings.instance_retention_days
FROM channels_settings settings
CROSS JOIN sweep
WHERE (settings.organization_id, settings.project_id) > (@after_organization_id::text, @after_project_id::uuid)
  AND (
    EXISTS (
      SELECT 1 FROM channels_messages m
      WHERE m.organization_id = settings.organization_id AND m.project_id = settings.project_id
        AND m.deleted IS FALSE
        AND m.created_at < sweep.at - make_interval(0, 0, 0, settings.message_retention_days)
    )
    OR EXISTS (
      SELECT 1 FROM channels_instances i
      WHERE i.organization_id = settings.organization_id AND i.project_id = settings.project_id
        AND i.deleted IS FALSE
        AND i.expires_at < sweep.at - make_interval(0, 0, 0, settings.instance_retention_days)
    )
  )
ORDER BY settings.organization_id, settings.project_id
LIMIT sqlc.arg('result_limit')::int;

-- name: GetChannelsAccessException :one
SELECT * FROM channels_access_exceptions
WHERE organization_id = @organization_id AND project_id = @project_id::uuid
  AND subject_id = @subject_id
  AND deleted IS FALSE;

-- name: ListChannelsAccessExceptions :many
SELECT * FROM channels_access_exceptions
WHERE organization_id = @organization_id AND project_id = @project_id::uuid
  AND deleted IS FALSE
ORDER BY subject_id
LIMIT sqlc.arg('result_limit')::int;

-- name: CountChannelsAccessExceptions :one
SELECT COUNT(*)::bigint FROM channels_access_exceptions
WHERE organization_id = @organization_id AND project_id = @project_id::uuid
  AND deleted IS FALSE;

-- name: UpsertChannelsAccessException :one
INSERT INTO channels_access_exceptions (organization_id, project_id, subject_id, can_connect, can_read_all)
VALUES (@organization_id, @project_id, @subject_id, @can_connect, @can_read_all)
ON CONFLICT (organization_id, project_id, subject_id) WHERE deleted IS FALSE
DO UPDATE SET can_connect = EXCLUDED.can_connect,
              can_read_all = EXCLUDED.can_read_all,
              updated_at = clock_timestamp()
RETURNING *;

-- name: DeleteChannelsAccessException :exec
UPDATE channels_access_exceptions
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE organization_id = @organization_id AND project_id = @project_id::uuid
  AND subject_id = @subject_id
  AND deleted IS FALSE;

-- name: CreateInstance :one
INSERT INTO channels_instances (organization_id, project_id, subject_id, label, note, origin, resume_hash, announce_seq, expires_at)
VALUES (@organization_id, @project_id, @subject_id, @label, @note, @origin, sqlc.narg('resume_hash'), @announce_seq, @expires_at)
RETURNING *;

-- name: UpsertDashboardInstance :one
-- Atomic reclaim-or-create of this account's single browser session. The
-- partial unique index makes simultaneous first sends and reloads converge on
-- one row without locking the project. announce_seq is deliberately left
-- untouched on reclaim: re-entering the dashboard is not a new announcement.
INSERT INTO channels_instances (organization_id, project_id, subject_id, label, note, origin, announce_seq, expires_at)
VALUES (@organization_id, @project_id, @subject_id, @label, @note, 'dashboard', @announce_seq, @expires_at)
ON CONFLICT (organization_id, project_id, subject_id) WHERE origin = 'dashboard' AND deleted IS FALSE
DO UPDATE SET label = EXCLUDED.label,
              note = CASE WHEN @replace_note::boolean THEN EXCLUDED.note ELSE channels_instances.note END,
              expires_at = EXCLUDED.expires_at,
              updated_at = clock_timestamp()
RETURNING *;

-- name: GetInstance :one
SELECT * FROM channels_instances
WHERE organization_id = @organization_id AND project_id = @project_id::uuid
  AND id = @id
  AND deleted IS FALSE;

-- name: RenewInstance :one
-- Renewal keeps announce_seq, so resuming never repeats an introduction.
UPDATE channels_instances
SET expires_at = @expires_at,
    note = CASE WHEN @replace_note::boolean THEN @note ELSE note END,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id AND project_id = @project_id::uuid
  AND id = @id
  AND subject_id = @subject_id
  AND deleted IS FALSE
RETURNING *;

-- name: ListLiveInstances :many
SELECT * FROM channels_instances
WHERE organization_id = @organization_id AND project_id = @project_id::uuid
  AND deleted IS FALSE
  AND expires_at > clock_timestamp()
ORDER BY expires_at DESC, id
LIMIT sqlc.arg('result_limit')::int;

-- name: ListInstancesByIDs :many
-- Attribution for instances whose lease has already expired. Returns display
-- identity only; the caller never exposes resume hashes.
SELECT * FROM channels_instances
WHERE organization_id = @organization_id AND project_id = @project_id::uuid
  AND id = ANY(@ids::uuid[])
  AND deleted IS FALSE;

-- name: CountLiveInstances :one
SELECT COUNT(*)::bigint FROM channels_instances
WHERE organization_id = @organization_id AND project_id = @project_id::uuid
  AND deleted IS FALSE
  AND expires_at > clock_timestamp();

-- name: GetPresenceDigest :one
-- Reconciliation aggregate changes with live membership, labels, and notes.
-- Excluding expiration timestamps avoids revisions on heartbeat-only renewals.
SELECT COUNT(*)::bigint AS live_count,
       COALESCE(MAX(announce_seq), 0)::bigint AS announce_seq,
       md5(COALESCE(jsonb_agg(jsonb_build_array(id, label, note) ORDER BY id)::text, '[]'))::text AS revision
FROM channels_instances
WHERE organization_id = @organization_id AND project_id = @project_id::uuid
  AND deleted IS FALSE
  AND expires_at > clock_timestamp();

-- name: CreateConversation :one
INSERT INTO channels_conversations (organization_id, project_id, title, parent_id, private, endpoint_pair_key, created_by_subject_id, announce_seq)
VALUES (@organization_id, @project_id, @title, sqlc.narg('parent_id'), @private, sqlc.narg('endpoint_pair_key'), @created_by_subject_id, @announce_seq)
ON CONFLICT (organization_id, project_id, endpoint_pair_key) WHERE endpoint_pair_key IS NOT NULL AND deleted IS FALSE
DO NOTHING
RETURNING *;

-- name: GetConversation :one
SELECT * FROM channels_conversations
WHERE organization_id = @organization_id AND project_id = @project_id::uuid
  AND id = @id
  AND deleted IS FALSE;

-- name: GetConversationByEndpointPair :one
SELECT * FROM channels_conversations
WHERE organization_id = @organization_id AND project_id = @project_id::uuid
  AND endpoint_pair_key = @endpoint_pair_key
  AND deleted IS FALSE;

-- name: TouchConversationActivity :exec
-- Called once per accepted message, never on an idempotent replay, so the
-- maintained count stays exact.
UPDATE channels_conversations
SET last_message_seq = GREATEST(last_message_seq, @last_message_seq::bigint),
    message_count = message_count + 1,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id AND project_id = @project_id::uuid
  AND id = @id;

-- name: DecrementConversationMessageCounts :exec
-- Retention's half of the maintained count. GREATEST keeps the column sane if
-- a count ever drifts below what was removed.
UPDATE channels_conversations c
SET message_count = GREATEST(c.message_count - removed.total, 0),
    updated_at = clock_timestamp()
FROM (
    SELECT unnest(@conversation_ids::uuid[]) AS id,
           unnest(@totals::bigint[]) AS total
) removed
WHERE c.organization_id = @organization_id AND c.project_id = @project_id::uuid
  AND c.id = removed.id;

-- name: ConversationHasRemovedHistory :one
-- Whether retention has actually removed messages from THIS conversation
-- below a boundary. Retention soft-deletes, so the removed rows are the
-- evidence; the project-wide retention floor is not, because a
-- conversation created after a sweep has lost nothing.
SELECT EXISTS (
    SELECT 1 FROM channels_messages
    WHERE organization_id = @organization_id AND project_id = @project_id::uuid
      AND conversation_id = @conversation_id
      AND deleted IS TRUE
      AND seq < @before_seq::bigint
)::boolean;

-- name: ListVisibleConversations :many
-- One keyset page of conversations the subject may read, newest activity
-- first, each with its newest readable message so a conversation list needs no
-- per-conversation history call. Page with the last row's (last_message_seq,
-- id); the ordering matches channels_conversations_activity_idx exactly.
--
-- message_count is read from the maintained column. Counting on read would
-- make this query scale with history length, which is the one thing a bounded
-- list must not do.
SELECT sqlc.embed(c),
       COALESCE(last_message.seq, 0)::bigint AS last_message_seq_value,
       COALESCE(last_message.instance_id, '00000000-0000-0000-0000-000000000000'::uuid)::uuid AS last_message_instance_id,
       COALESCE(last_message.subject_id, '')::text AS last_message_subject_id,
       COALESCE(last_message.body, '')::text AS last_message_body,
       COALESCE(last_message.idempotency_key, '')::text AS last_message_idempotency_key,
       COALESCE(last_message.created_at, to_timestamp(0))::timestamptz AS last_message_created_at
FROM channels_conversations c
LEFT JOIN LATERAL (
    SELECT m.seq, m.instance_id, m.subject_id, m.body, m.idempotency_key, m.created_at
    FROM channels_messages m
    WHERE m.organization_id = c.organization_id AND m.project_id = c.project_id
      AND m.conversation_id = c.id
      AND m.deleted IS FALSE
    ORDER BY m.seq DESC
    LIMIT 1
) last_message ON TRUE
WHERE c.organization_id = @organization_id AND c.project_id = @project_id::uuid
  AND c.deleted IS FALSE
  AND (
    @from_start::boolean
    OR c.last_message_seq < @after_seq::bigint
    OR (c.last_message_seq = @after_seq::bigint AND c.id > @after_id::uuid)
  )
  AND (
    c.private IS FALSE
    OR @read_all::boolean
    OR EXISTS (
      SELECT 1 FROM channels_conversation_participants p
      WHERE p.organization_id = c.organization_id AND p.project_id = c.project_id
        AND p.conversation_id = c.id
        AND p.subject_id = @subject_id
    )
  )
ORDER BY c.last_message_seq DESC, c.id
LIMIT sqlc.arg('result_limit')::int;

-- name: GetConversationsDigest :one
-- activity_seq is the newest message sequence across the visible
-- conversations. Every post raises it, so a threads subscriber learns that a
-- conversation's last message and count changed.
SELECT COUNT(*)::bigint AS conversation_count,
       COALESCE(MAX(c.announce_seq), 0)::bigint AS announce_seq,
       COALESCE(MAX(c.last_message_seq), 0)::bigint AS activity_seq
FROM channels_conversations c
WHERE c.organization_id = @organization_id AND c.project_id = @project_id::uuid
  AND c.deleted IS FALSE
  AND (
    c.private IS FALSE
    OR @read_all::boolean
    OR EXISTS (
      SELECT 1 FROM channels_conversation_participants p
      WHERE p.organization_id = c.organization_id AND p.project_id = c.project_id
        AND p.conversation_id = c.id
        AND p.subject_id = @subject_id
    )
  );

-- name: AddConversationParticipant :exec
INSERT INTO channels_conversation_participants (organization_id, project_id, conversation_id, subject_id)
VALUES (@organization_id, @project_id, @conversation_id, @subject_id)
ON CONFLICT (organization_id, project_id, conversation_id, subject_id) DO NOTHING;

-- name: AddConversationEndpoint :exec
INSERT INTO channels_conversation_endpoints (organization_id, project_id, conversation_id, instance_id)
VALUES (@organization_id, @project_id, @conversation_id, @instance_id)
ON CONFLICT (organization_id, project_id, conversation_id, instance_id) DO NOTHING;

-- name: ListParticipantsForConversations :many
SELECT conversation_id, subject_id
FROM channels_conversation_participants
WHERE organization_id = @organization_id AND project_id = @project_id::uuid
  AND conversation_id = ANY(@conversation_ids::uuid[])
ORDER BY conversation_id, subject_id;

-- name: ListEndpointsForConversations :many
SELECT conversation_id, instance_id
FROM channels_conversation_endpoints
WHERE organization_id = @organization_id AND project_id = @project_id::uuid
  AND conversation_id = ANY(@conversation_ids::uuid[])
ORDER BY conversation_id, instance_id;

-- name: IsConversationParticipant :one
SELECT EXISTS (
    SELECT 1 FROM channels_conversation_participants
    WHERE organization_id = @organization_id AND project_id = @project_id::uuid
      AND conversation_id = @conversation_id
      AND subject_id = @subject_id
)::boolean;

-- name: GetMessageByIdempotencyKey :one
SELECT * FROM channels_messages
WHERE organization_id = @organization_id AND project_id = @project_id::uuid
  AND subject_id = @subject_id
  AND idempotency_key = @idempotency_key
  AND deleted IS FALSE;

-- name: InsertMessage :one
-- Returns no rows when a concurrent retry already claimed the key; the caller
-- then re-reads the winner and compares its payload rather than returning an
-- unrelated message.
INSERT INTO channels_messages (organization_id, project_id, conversation_id, instance_id, subject_id, seq, body, idempotency_key)
VALUES (@organization_id, @project_id, @conversation_id, @instance_id, @subject_id, @seq, @body, @idempotency_key)
ON CONFLICT (organization_id, project_id, subject_id, idempotency_key) WHERE deleted IS FALSE
DO NOTHING
RETURNING *;

-- name: ListMessagesForward :many
-- Durable catch-up. Callers request one more row than the page size to learn
-- whether more is immediately available.
SELECT m.*
FROM channels_messages m
JOIN channels_conversations c
  ON c.organization_id = m.organization_id AND c.project_id = m.project_id AND c.id = m.conversation_id
WHERE m.organization_id = @organization_id AND m.project_id = @project_id::uuid
  AND m.deleted IS FALSE
  AND c.deleted IS FALSE
  AND m.seq > @after_seq
  AND (sqlc.narg('conversation_id')::uuid IS NULL OR m.conversation_id = sqlc.narg('conversation_id')::uuid)
  AND (
    c.private IS FALSE
    OR @read_all::boolean
    OR EXISTS (
      SELECT 1 FROM channels_conversation_participants p
      WHERE p.organization_id = c.organization_id AND p.project_id = c.project_id
        AND p.conversation_id = c.id
        AND p.subject_id = @subject_id
    )
  )
ORDER BY m.seq ASC
LIMIT sqlc.arg('result_limit')::int;

-- name: ListMessagesBackward :many
-- One page of older history for a single conversation, newest first. The
-- caller reverses it so clients always receive ascending messages.
SELECT m.*
FROM channels_messages m
JOIN channels_conversations c
  ON c.organization_id = m.organization_id AND c.project_id = m.project_id AND c.id = m.conversation_id
WHERE m.organization_id = @organization_id AND m.project_id = @project_id::uuid
  AND m.conversation_id = @conversation_id
  AND m.deleted IS FALSE
  AND c.deleted IS FALSE
  AND (@before_seq::bigint = 0 OR m.seq < @before_seq::bigint)
  AND (
    c.private IS FALSE
    OR @read_all::boolean
    OR EXISTS (
      SELECT 1 FROM channels_conversation_participants p
      WHERE p.organization_id = c.organization_id AND p.project_id = c.project_id
        AND p.conversation_id = c.id
        AND p.subject_id = @subject_id
    )
  )
ORDER BY m.seq DESC
LIMIT sqlc.arg('result_limit')::int;

-- name: ListRecentVisibleMessages :many
-- Bounded bootstrap tail, newest first; the caller reverses it.
SELECT m.*
FROM channels_messages m
JOIN channels_conversations c
  ON c.organization_id = m.organization_id AND c.project_id = m.project_id AND c.id = m.conversation_id
WHERE m.organization_id = @organization_id AND m.project_id = @project_id::uuid
  AND m.deleted IS FALSE
  AND c.deleted IS FALSE
  AND (
    c.private IS FALSE
    OR @read_all::boolean
    OR EXISTS (
      SELECT 1 FROM channels_conversation_participants p
      WHERE p.organization_id = c.organization_id AND p.project_id = c.project_id
        AND p.conversation_id = c.id
        AND p.subject_id = @subject_id
    )
  )
ORDER BY m.seq DESC
LIMIT sqlc.arg('result_limit')::int;

-- name: GetNotifiableMessageSeq :one
-- Highest sequence this subject should be told about: readable, not muted, and
-- for a public thread also explicitly followed. One indexed aggregate, so a
-- delivery hint costs a bounded query instead of a state reload.
SELECT COALESCE(MAX(m.seq), 0)::bigint
FROM channels_messages m
JOIN channels_conversations c
  ON c.organization_id = m.organization_id AND c.project_id = m.project_id AND c.id = m.conversation_id
LEFT JOIN channels_thread_preferences pref
  ON pref.organization_id = m.organization_id AND pref.project_id = m.project_id
 AND pref.conversation_id = m.conversation_id
 AND pref.subject_id = @subject_id
WHERE m.organization_id = @organization_id AND m.project_id = @project_id::uuid
  AND m.deleted IS FALSE
  AND c.deleted IS FALSE
  AND m.seq > @after_seq
  AND COALESCE(pref.muted, FALSE) IS FALSE
  AND (
    CASE WHEN c.private THEN
      @read_all::boolean
      OR EXISTS (
        SELECT 1 FROM channels_conversation_participants p
        WHERE p.organization_id = c.organization_id AND p.project_id = c.project_id
          AND p.conversation_id = c.id
          AND p.subject_id = @subject_id
      )
    ELSE COALESCE(pref.followed, FALSE) END
  );

-- name: GetMaxVisibleMessageSeq :one
SELECT COALESCE(MAX(m.seq), 0)::bigint
FROM channels_messages m
JOIN channels_conversations c
  ON c.organization_id = m.organization_id AND c.project_id = m.project_id AND c.id = m.conversation_id
WHERE m.organization_id = @organization_id AND m.project_id = @project_id::uuid
  AND m.deleted IS FALSE
  AND c.deleted IS FALSE
  AND (
    c.private IS FALSE
    OR @read_all::boolean
    OR EXISTS (
      SELECT 1 FROM channels_conversation_participants p
      WHERE p.organization_id = c.organization_id AND p.project_id = c.project_id
        AND p.conversation_id = c.id
        AND p.subject_id = @subject_id
    )
  );

-- name: UpsertThreadPreference :one
INSERT INTO channels_thread_preferences (organization_id, project_id, subject_id, conversation_id, followed, muted)
VALUES (@organization_id, @project_id, @subject_id, @conversation_id, @followed, @muted)
ON CONFLICT (organization_id, project_id, subject_id, conversation_id)
DO UPDATE SET followed = CASE WHEN @set_followed::boolean THEN EXCLUDED.followed ELSE channels_thread_preferences.followed END,
              muted = CASE WHEN @set_muted::boolean THEN EXCLUDED.muted ELSE channels_thread_preferences.muted END,
              updated_at = clock_timestamp()
RETURNING *;

-- name: ListThreadPreferences :many
SELECT * FROM channels_thread_preferences
WHERE organization_id = @organization_id AND project_id = @project_id::uuid
  AND subject_id = @subject_id
  AND (followed IS TRUE OR muted IS TRUE)
ORDER BY conversation_id
LIMIT sqlc.arg('result_limit')::int;

-- name: CountThreadPreferences :one
SELECT COUNT(*)::bigint FROM channels_thread_preferences
WHERE organization_id = @organization_id AND project_id = @project_id::uuid
  AND subject_id = @subject_id
  AND (followed IS TRUE OR muted IS TRUE);

-- name: ClearThreadPreferences :exec
UPDATE channels_thread_preferences
SET followed = FALSE, muted = FALSE, updated_at = clock_timestamp()
WHERE organization_id = @organization_id AND project_id = @project_id::uuid
  AND subject_id = @subject_id
  AND (followed IS TRUE OR muted IS TRUE);

-- name: SoftDeleteExpiredMessages :many
-- Bounded retention batch for one project, oldest first.
WITH doomed AS (
    SELECT expiring.id, expiring.seq FROM channels_messages expiring
    WHERE expiring.organization_id = @organization_id AND expiring.project_id = @project_id::uuid
      AND expiring.deleted IS FALSE
      AND expiring.created_at < @cutoff
    ORDER BY expiring.seq ASC
    LIMIT sqlc.arg('result_limit')::int
)
UPDATE channels_messages m
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
FROM doomed
WHERE m.id = doomed.id
  AND m.organization_id = @organization_id AND m.project_id = @project_id::uuid
RETURNING m.seq, m.conversation_id;

-- name: SoftDeleteStaleInstances :many
-- Retires long-expired instances that no longer carry attribution. An instance
-- still referenced by a message or a private conversation endpoint is kept so
-- history never loses the account it belongs to.
WITH doomed AS (
    SELECT stale.id FROM channels_instances stale
    WHERE stale.organization_id = @organization_id AND stale.project_id = @project_id::uuid
      AND stale.deleted IS FALSE
      AND stale.expires_at < @cutoff
      AND NOT EXISTS (
        SELECT 1 FROM channels_messages m
        WHERE m.organization_id = stale.organization_id AND m.project_id = stale.project_id AND m.instance_id = stale.id AND m.deleted IS FALSE
      )
      AND NOT EXISTS (
        SELECT 1 FROM channels_conversation_endpoints e
        WHERE e.organization_id = stale.organization_id AND e.project_id = stale.project_id AND e.instance_id = stale.id
      )
    ORDER BY stale.expires_at ASC
    LIMIT sqlc.arg('result_limit')::int
)
UPDATE channels_instances i
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
FROM doomed
WHERE i.id = doomed.id
  AND i.organization_id = @organization_id AND i.project_id = @project_id::uuid
  AND i.deleted IS FALSE
  -- Recheck the current row after waiting for a concurrent renewal's lock.
  AND i.expires_at < @cutoff
RETURNING i.id;

-- name: PublishChannelsEvent :exec
-- Delivery hint. Postgres releases it at commit, so history is always durable
-- before anyone is told to re-read it. The payload carries routing identifiers
-- and revisions only, never message content.
SELECT pg_notify('channels_events', @payload::text);

-- name: GetDashboardInstance :one
SELECT * FROM channels_instances
WHERE organization_id = @organization_id AND project_id = @project_id::uuid
  AND subject_id = @subject_id
  AND origin = 'dashboard'
  AND deleted IS FALSE;

-- Test fixtures. Nothing in the service calls these: they age rows past a
-- retention window and read storage invariants that have no production read path.

-- name: FixtureAgeMessages :exec
UPDATE channels_messages
SET created_at = clock_timestamp() - make_interval(days => @age_days::int)
WHERE organization_id = @organization_id AND project_id = @project_id::uuid
  AND seq <= @through_seq::bigint;

-- name: FixtureAgeInstances :exec
UPDATE channels_instances
SET expires_at = clock_timestamp() - make_interval(days => @age_days::int)
WHERE organization_id = @organization_id AND project_id = @project_id::uuid;

-- name: FixtureSetMessageRetentionDays :exec
UPDATE channels_settings
SET message_retention_days = @message_retention_days
WHERE organization_id = @organization_id AND project_id = @project_id::uuid;

-- name: FixtureCountRows :one
-- Every normalized Channels table's row count for one project.
WITH target AS (
    SELECT @organization_id::text AS organization_id, @project_id::uuid AS project_id
)
SELECT
    (SELECT count(*) FROM channels_instances i, target WHERE i.organization_id = target.organization_id AND i.project_id = target.project_id)::bigint AS instances,
    (SELECT count(*) FROM channels_conversations c, target WHERE c.organization_id = target.organization_id AND c.project_id = target.project_id)::bigint AS conversations,
    (SELECT count(*) FROM channels_messages m, target WHERE m.organization_id = target.organization_id AND m.project_id = target.project_id)::bigint AS messages,
    (SELECT count(*) FROM channels_conversation_participants p, target WHERE p.organization_id = target.organization_id AND p.project_id = target.project_id)::bigint AS participants,
    (SELECT count(*) FROM channels_conversation_endpoints e, target WHERE e.organization_id = target.organization_id AND e.project_id = target.project_id)::bigint AS endpoints,
    (SELECT count(*) FROM channels_thread_preferences t, target WHERE t.organization_id = target.organization_id AND t.project_id = target.project_id)::bigint AS preferences,
    (SELECT count(*) FROM channels_access_exceptions x, target WHERE x.organization_id = target.organization_id AND x.project_id = target.project_id)::bigint AS access_exceptions;

-- name: FixtureCountRowsOutsideDefaultProject :one
-- Counts rows outside the explicitly selected project; no default resolution.
WITH target AS (
 SELECT @organization_id::text AS organization_id, @project_id::uuid AS project_id
)
SELECT (
(SELECT count(*) FROM channels_settings t, target WHERE t.organization_id = target.organization_id AND t.project_id <> target.project_id)
 + (SELECT count(*) FROM channels_instances t, target WHERE t.organization_id = target.organization_id AND t.project_id <> target.project_id)
 + (SELECT count(*) FROM channels_conversations t, target WHERE t.organization_id = target.organization_id AND t.project_id <> target.project_id)
 + (SELECT count(*) FROM channels_messages t, target WHERE t.organization_id = target.organization_id AND t.project_id <> target.project_id)
 + (SELECT count(*) FROM channels_conversation_participants t, target WHERE t.organization_id = target.organization_id AND t.project_id <> target.project_id)
 + (SELECT count(*) FROM channels_conversation_endpoints t, target WHERE t.organization_id = target.organization_id AND t.project_id <> target.project_id)
 + (SELECT count(*) FROM channels_thread_preferences t, target WHERE t.organization_id = target.organization_id AND t.project_id <> target.project_id)
 + (SELECT count(*) FROM channels_access_exceptions t, target WHERE t.organization_id = target.organization_id AND t.project_id <> target.project_id)
)::bigint;

-- name: FixtureMessageSeqStats :one
SELECT count(DISTINCT seq)::bigint AS distinct_seqs,
       count(*)::bigint AS total,
       COALESCE(min(seq), 0)::bigint AS lowest,
       COALESCE(max(seq), 0)::bigint AS highest
FROM channels_messages
WHERE organization_id = @organization_id AND project_id = @project_id::uuid;

-- name: FixtureCountMessagesByIdempotencyKey :one
SELECT count(*)::bigint FROM channels_messages
WHERE organization_id = @organization_id AND project_id = @project_id::uuid
  AND idempotency_key = @idempotency_key;

-- name: FixtureCountDashboardInstances :one
SELECT count(*)::bigint FROM channels_instances
WHERE organization_id = @organization_id AND project_id = @project_id::uuid
  AND subject_id = @subject_id
  AND origin = 'dashboard'
  AND deleted IS FALSE;

-- name: FixtureExpireInstance :exec
UPDATE channels_instances
SET expires_at = clock_timestamp() - interval '1 minute'
WHERE organization_id = @organization_id AND project_id = @project_id::uuid AND id = @id;

-- name: FixtureSwapLiveInstances :exec
UPDATE channels_instances
SET expires_at = CASE WHEN id = @returning_id::uuid THEN clock_timestamp() + interval '10 minutes'
                      ELSE clock_timestamp() - interval '1 minute' END
WHERE organization_id = @organization_id AND project_id = @project_id::uuid AND id IN (@returning_id::uuid, @departing_id::uuid);
