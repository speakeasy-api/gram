-- Channels queries.
--
-- Channels is organization-scoped: every query filters organization_id, which
-- is the tenancy key callers cannot influence. project_id is storage ownership
-- only (the organization's canonical default project) and is written, never
-- used to select rows, so a caller-selected project can neither split nor
-- redirect an organization's Channels data.

-- name: EnsureChannelsSettings :one
-- Creates the organization's Channels settings row under its canonical default
-- project, or returns the existing one. Returns no rows when the organization
-- has no project at all, which the caller reports as unavailable storage.
WITH owner AS (
    SELECT project.id
    FROM projects project
    WHERE project.organization_id = @organization_id
      AND project.deleted IS FALSE
    ORDER BY (project.slug = 'default') DESC, project.created_at ASC, project.id ASC
    LIMIT 1
), created AS (
    INSERT INTO channels_settings (organization_id, project_id)
    SELECT @organization_id, owner.id FROM owner
    ON CONFLICT (organization_id) DO NOTHING
    RETURNING *
)
SELECT created.* FROM created
UNION ALL
SELECT existing.* FROM channels_settings existing
WHERE existing.organization_id = @organization_id
  AND NOT EXISTS (SELECT 1 FROM created);

-- name: GetChannelsSettings :one
SELECT * FROM channels_settings WHERE organization_id = @organization_id;

-- name: SetChannelsEnabled :one
UPDATE channels_settings
SET enabled = @enabled, updated_at = clock_timestamp()
WHERE organization_id = @organization_id
RETURNING *;

-- name: AllocateMessageSeq :one
-- Allocates the next cursor position. The row write lock this takes is held
-- until the posting transaction commits, so sequence order and commit order
-- agree and a reader paging on `seq >` can never skip a late commit. Call it
-- as the last step of the transaction to keep the serialized window short.
UPDATE channels_settings
SET message_seq = message_seq + 1, updated_at = clock_timestamp()
WHERE organization_id = @organization_id
RETURNING message_seq;

-- name: AllocateAnnounceSeq :one
UPDATE channels_settings
SET announce_seq = announce_seq + 1, updated_at = clock_timestamp()
WHERE organization_id = @organization_id
RETURNING announce_seq;

-- name: RaiseRetentionFloor :one
UPDATE channels_settings
SET retention_floor_seq = GREATEST(retention_floor_seq, @floor_seq::bigint),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
RETURNING retention_floor_seq;

-- name: ListChannelsOrganizationsNeedingRetention :many
-- Keyset page over the organizations that actually have something to retire,
-- so a sweep costs one indexed query instead of two per organization that has
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
WHERE settings.organization_id > @after_organization_id
  AND (
    EXISTS (
      SELECT 1 FROM channels_messages m
      WHERE m.organization_id = settings.organization_id
        AND m.deleted IS FALSE
        AND m.created_at < sweep.at - make_interval(0, 0, 0, settings.message_retention_days)
    )
    OR EXISTS (
      SELECT 1 FROM channels_instances i
      WHERE i.organization_id = settings.organization_id
        AND i.deleted IS FALSE
        AND i.expires_at < sweep.at - make_interval(0, 0, 0, settings.instance_retention_days)
    )
  )
ORDER BY settings.organization_id
LIMIT sqlc.arg('result_limit')::int;

-- name: GetChannelsAccessException :one
SELECT * FROM channels_access_exceptions
WHERE organization_id = @organization_id
  AND subject_id = @subject_id
  AND deleted IS FALSE;

-- name: ListChannelsAccessExceptions :many
SELECT * FROM channels_access_exceptions
WHERE organization_id = @organization_id
  AND deleted IS FALSE
ORDER BY subject_id
LIMIT sqlc.arg('result_limit')::int;

-- name: UpsertChannelsAccessException :one
INSERT INTO channels_access_exceptions (organization_id, project_id, subject_id, can_connect, can_read_all)
VALUES (@organization_id, @project_id, @subject_id, @can_connect, @can_read_all)
ON CONFLICT (organization_id, subject_id) WHERE deleted IS FALSE
DO UPDATE SET can_connect = EXCLUDED.can_connect,
              can_read_all = EXCLUDED.can_read_all,
              updated_at = clock_timestamp()
RETURNING *;

-- name: DeleteChannelsAccessException :exec
UPDATE channels_access_exceptions
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND subject_id = @subject_id
  AND deleted IS FALSE;

-- name: CreateInstance :one
INSERT INTO channels_instances (organization_id, project_id, subject_id, label, note, origin, resume_hash, announce_seq, expires_at)
VALUES (@organization_id, @project_id, @subject_id, @label, @note, @origin, sqlc.narg('resume_hash'), @announce_seq, @expires_at)
RETURNING *;

-- name: UpsertDashboardInstance :one
-- Atomic reclaim-or-create of this account's single browser session. The
-- partial unique index makes simultaneous first sends and reloads converge on
-- one row without locking the organization. announce_seq is deliberately left
-- untouched on reclaim: re-entering the dashboard is not a new announcement.
INSERT INTO channels_instances (organization_id, project_id, subject_id, label, note, origin, announce_seq, expires_at)
VALUES (@organization_id, @project_id, @subject_id, @label, @note, 'dashboard', @announce_seq, @expires_at)
ON CONFLICT (organization_id, subject_id) WHERE origin = 'dashboard' AND deleted IS FALSE
DO UPDATE SET label = EXCLUDED.label,
              note = CASE WHEN @replace_note::boolean THEN EXCLUDED.note ELSE channels_instances.note END,
              expires_at = EXCLUDED.expires_at,
              updated_at = clock_timestamp()
RETURNING *;

-- name: GetInstance :one
SELECT * FROM channels_instances
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE;

-- name: RenewInstance :one
-- Renewal keeps announce_seq, so resuming never repeats an introduction.
UPDATE channels_instances
SET expires_at = @expires_at,
    note = CASE WHEN @replace_note::boolean THEN @note ELSE note END,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND subject_id = @subject_id
  AND deleted IS FALSE
RETURNING *;

-- name: ListLiveInstances :many
SELECT * FROM channels_instances
WHERE organization_id = @organization_id
  AND deleted IS FALSE
  AND expires_at > clock_timestamp()
ORDER BY expires_at DESC, id
LIMIT sqlc.arg('result_limit')::int;

-- name: ListInstancesByIDs :many
-- Attribution for instances whose lease has already expired. Returns display
-- identity only; the caller never exposes resume hashes.
SELECT * FROM channels_instances
WHERE organization_id = @organization_id
  AND id = ANY(@ids::uuid[])
  AND deleted IS FALSE;

-- name: CountLiveInstances :one
SELECT COUNT(*)::bigint FROM channels_instances
WHERE organization_id = @organization_id
  AND deleted IS FALSE
  AND expires_at > clock_timestamp();

-- name: GetPresenceDigest :one
-- Bounded reconciliation aggregate. It changes when an instance appears and
-- when one drops out of the live set, without loading presence itself.
SELECT COUNT(*)::bigint AS live_count,
       COALESCE(MAX(announce_seq), 0)::bigint AS announce_seq
FROM channels_instances
WHERE organization_id = @organization_id
  AND deleted IS FALSE
  AND expires_at > clock_timestamp();

-- name: CreateConversation :one
INSERT INTO channels_conversations (organization_id, project_id, title, parent_id, private, endpoint_pair_key, created_by_subject_id, announce_seq)
VALUES (@organization_id, @project_id, @title, sqlc.narg('parent_id'), @private, sqlc.narg('endpoint_pair_key'), @created_by_subject_id, @announce_seq)
ON CONFLICT (organization_id, endpoint_pair_key) WHERE endpoint_pair_key IS NOT NULL AND deleted IS FALSE
DO NOTHING
RETURNING *;

-- name: GetConversation :one
SELECT * FROM channels_conversations
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE;

-- name: GetConversationByEndpointPair :one
SELECT * FROM channels_conversations
WHERE organization_id = @organization_id
  AND endpoint_pair_key = @endpoint_pair_key
  AND deleted IS FALSE;

-- name: TouchConversationActivity :exec
-- Called once per accepted message, never on an idempotent replay, so the
-- maintained count stays exact.
UPDATE channels_conversations
SET last_message_seq = GREATEST(last_message_seq, @last_message_seq::bigint),
    message_count = message_count + 1,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
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
WHERE c.organization_id = @organization_id
  AND c.id = removed.id;

-- name: ConversationHasRemovedHistory :one
-- Whether retention has actually removed messages from THIS conversation
-- below a boundary. Retention soft-deletes, so the removed rows are the
-- evidence; the organization-wide retention floor is not, because a
-- conversation created after a sweep has lost nothing.
SELECT EXISTS (
    SELECT 1 FROM channels_messages
    WHERE organization_id = @organization_id
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
    WHERE m.organization_id = c.organization_id
      AND m.conversation_id = c.id
      AND m.deleted IS FALSE
    ORDER BY m.seq DESC
    LIMIT 1
) last_message ON TRUE
WHERE c.organization_id = @organization_id
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
      WHERE p.organization_id = c.organization_id
        AND p.conversation_id = c.id
        AND p.subject_id = @subject_id
    )
  )
ORDER BY c.last_message_seq DESC, c.id
LIMIT sqlc.arg('result_limit')::int;

-- name: GetConversationsDigest :one
SELECT COUNT(*)::bigint AS conversation_count,
       COALESCE(MAX(c.announce_seq), 0)::bigint AS announce_seq
FROM channels_conversations c
WHERE c.organization_id = @organization_id
  AND c.deleted IS FALSE
  AND (
    c.private IS FALSE
    OR @read_all::boolean
    OR EXISTS (
      SELECT 1 FROM channels_conversation_participants p
      WHERE p.organization_id = c.organization_id
        AND p.conversation_id = c.id
        AND p.subject_id = @subject_id
    )
  );

-- name: AddConversationParticipant :exec
INSERT INTO channels_conversation_participants (organization_id, project_id, conversation_id, subject_id)
VALUES (@organization_id, @project_id, @conversation_id, @subject_id)
ON CONFLICT (conversation_id, subject_id) DO NOTHING;

-- name: AddConversationEndpoint :exec
INSERT INTO channels_conversation_endpoints (organization_id, project_id, conversation_id, instance_id)
VALUES (@organization_id, @project_id, @conversation_id, @instance_id)
ON CONFLICT (conversation_id, instance_id) DO NOTHING;

-- name: ListParticipantsForConversations :many
SELECT conversation_id, subject_id
FROM channels_conversation_participants
WHERE organization_id = @organization_id
  AND conversation_id = ANY(@conversation_ids::uuid[])
ORDER BY conversation_id, subject_id;

-- name: ListEndpointsForConversations :many
SELECT conversation_id, instance_id
FROM channels_conversation_endpoints
WHERE organization_id = @organization_id
  AND conversation_id = ANY(@conversation_ids::uuid[])
ORDER BY conversation_id, instance_id;

-- name: IsConversationParticipant :one
SELECT EXISTS (
    SELECT 1 FROM channels_conversation_participants
    WHERE organization_id = @organization_id
      AND conversation_id = @conversation_id
      AND subject_id = @subject_id
)::boolean;

-- name: GetMessageByIdempotencyKey :one
SELECT * FROM channels_messages
WHERE organization_id = @organization_id
  AND subject_id = @subject_id
  AND idempotency_key = @idempotency_key
  AND deleted IS FALSE;

-- name: InsertMessage :one
-- Returns no rows when a concurrent retry already claimed the key; the caller
-- then re-reads the winner and compares its payload rather than returning an
-- unrelated message.
INSERT INTO channels_messages (organization_id, project_id, conversation_id, instance_id, subject_id, seq, body, idempotency_key)
VALUES (@organization_id, @project_id, @conversation_id, @instance_id, @subject_id, @seq, @body, @idempotency_key)
ON CONFLICT (organization_id, subject_id, idempotency_key) WHERE deleted IS FALSE
DO NOTHING
RETURNING *;

-- name: ListMessagesForward :many
-- Durable catch-up. Callers request one more row than the page size to learn
-- whether more is immediately available.
SELECT m.*
FROM channels_messages m
JOIN channels_conversations c
  ON c.organization_id = m.organization_id AND c.id = m.conversation_id
WHERE m.organization_id = @organization_id
  AND m.deleted IS FALSE
  AND c.deleted IS FALSE
  AND m.seq > @after_seq
  AND (sqlc.narg('conversation_id')::uuid IS NULL OR m.conversation_id = sqlc.narg('conversation_id')::uuid)
  AND (
    c.private IS FALSE
    OR @read_all::boolean
    OR EXISTS (
      SELECT 1 FROM channels_conversation_participants p
      WHERE p.organization_id = c.organization_id
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
  ON c.organization_id = m.organization_id AND c.id = m.conversation_id
WHERE m.organization_id = @organization_id
  AND m.conversation_id = @conversation_id
  AND m.deleted IS FALSE
  AND c.deleted IS FALSE
  AND (@before_seq::bigint = 0 OR m.seq < @before_seq::bigint)
  AND (
    c.private IS FALSE
    OR @read_all::boolean
    OR EXISTS (
      SELECT 1 FROM channels_conversation_participants p
      WHERE p.organization_id = c.organization_id
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
  ON c.organization_id = m.organization_id AND c.id = m.conversation_id
WHERE m.organization_id = @organization_id
  AND m.deleted IS FALSE
  AND c.deleted IS FALSE
  AND (
    c.private IS FALSE
    OR @read_all::boolean
    OR EXISTS (
      SELECT 1 FROM channels_conversation_participants p
      WHERE p.organization_id = c.organization_id
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
  ON c.organization_id = m.organization_id AND c.id = m.conversation_id
LEFT JOIN channels_thread_preferences pref
  ON pref.organization_id = m.organization_id
 AND pref.conversation_id = m.conversation_id
 AND pref.subject_id = @subject_id
WHERE m.organization_id = @organization_id
  AND m.deleted IS FALSE
  AND c.deleted IS FALSE
  AND m.seq > @after_seq
  AND COALESCE(pref.muted, FALSE) IS FALSE
  AND (
    CASE WHEN c.private THEN
      @read_all::boolean
      OR EXISTS (
        SELECT 1 FROM channels_conversation_participants p
        WHERE p.organization_id = c.organization_id
          AND p.conversation_id = c.id
          AND p.subject_id = @subject_id
      )
    ELSE COALESCE(pref.followed, FALSE) END
  );

-- name: GetMaxVisibleMessageSeq :one
SELECT COALESCE(MAX(m.seq), 0)::bigint
FROM channels_messages m
JOIN channels_conversations c
  ON c.organization_id = m.organization_id AND c.id = m.conversation_id
WHERE m.organization_id = @organization_id
  AND m.deleted IS FALSE
  AND c.deleted IS FALSE
  AND (
    c.private IS FALSE
    OR @read_all::boolean
    OR EXISTS (
      SELECT 1 FROM channels_conversation_participants p
      WHERE p.organization_id = c.organization_id
        AND p.conversation_id = c.id
        AND p.subject_id = @subject_id
    )
  );

-- name: UpsertThreadPreference :one
INSERT INTO channels_thread_preferences (organization_id, project_id, subject_id, conversation_id, followed, muted)
VALUES (@organization_id, @project_id, @subject_id, @conversation_id, @followed, @muted)
ON CONFLICT (organization_id, subject_id, conversation_id)
DO UPDATE SET followed = CASE WHEN @set_followed::boolean THEN EXCLUDED.followed ELSE channels_thread_preferences.followed END,
              muted = CASE WHEN @set_muted::boolean THEN EXCLUDED.muted ELSE channels_thread_preferences.muted END,
              updated_at = clock_timestamp()
RETURNING *;

-- name: ListThreadPreferences :many
SELECT * FROM channels_thread_preferences
WHERE organization_id = @organization_id
  AND subject_id = @subject_id
  AND (followed IS TRUE OR muted IS TRUE)
ORDER BY conversation_id
LIMIT sqlc.arg('result_limit')::int;

-- name: ClearThreadPreferences :exec
UPDATE channels_thread_preferences
SET followed = FALSE, muted = FALSE, updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND subject_id = @subject_id
  AND (followed IS TRUE OR muted IS TRUE);

-- name: SoftDeleteExpiredMessages :many
-- Bounded retention batch for one organization, oldest first.
WITH doomed AS (
    SELECT expiring.id, expiring.seq FROM channels_messages expiring
    WHERE expiring.organization_id = @organization_id
      AND expiring.deleted IS FALSE
      AND expiring.created_at < @cutoff
    ORDER BY expiring.seq ASC
    LIMIT sqlc.arg('result_limit')::int
)
UPDATE channels_messages m
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
FROM doomed
WHERE m.id = doomed.id
  AND m.organization_id = @organization_id
RETURNING m.seq, m.conversation_id;

-- name: SoftDeleteStaleInstances :many
-- Retires long-expired instances that no longer carry attribution. An instance
-- still referenced by a message or a private conversation endpoint is kept so
-- history never loses the account it belongs to.
WITH doomed AS (
    SELECT stale.id FROM channels_instances stale
    WHERE stale.organization_id = @organization_id
      AND stale.deleted IS FALSE
      AND stale.expires_at < @cutoff
      AND NOT EXISTS (
        SELECT 1 FROM channels_messages m
        WHERE m.organization_id = stale.organization_id AND m.instance_id = stale.id AND m.deleted IS FALSE
      )
      AND NOT EXISTS (
        SELECT 1 FROM channels_conversation_endpoints e
        WHERE e.organization_id = stale.organization_id AND e.instance_id = stale.id
      )
    ORDER BY stale.expires_at ASC
    LIMIT sqlc.arg('result_limit')::int
)
UPDATE channels_instances i
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
FROM doomed
WHERE i.id = doomed.id
  AND i.organization_id = @organization_id
RETURNING i.id;

-- name: PublishChannelsEvent :exec
-- Delivery hint. Postgres releases it at commit, so history is always durable
-- before anyone is told to re-read it. The payload carries routing identifiers
-- and revisions only, never message content.
SELECT pg_notify('channels_events', @payload::text);

-- name: GetDashboardInstance :one
SELECT * FROM channels_instances
WHERE organization_id = @organization_id
  AND subject_id = @subject_id
  AND origin = 'dashboard'
  AND deleted IS FALSE;

-- name: ListLegacyChannelsStates :many
-- Reads the superseded proof-of-concept document so a development database can
-- be converted into the normalized tables without losing its contents. An
-- empty organization filter means every organization, which is why the command
-- in front of this makes the operator ask for that explicitly.
SELECT document.organization_id, document.project_id, document.state
FROM channels_state document
WHERE (@organization_id::text = '' OR document.organization_id = @organization_id::text)
ORDER BY document.organization_id
LIMIT sqlc.arg('result_limit')::int;

-- name: CountForeignChannelsRows :one
-- Counts normalized Channels rows in an organization that a conversion did
-- NOT write. A conversion is idempotent against its own output (the
-- identifiers are derived from the document) but must never merge into
-- somebody else's data: legacy sequence numbers would collide with sequences
-- the live service has already handed out.
-- The tenant is bound once in a CTE and every table is aliased: the query
-- generator mangles a named parameter repeated across correlated subqueries.
WITH target AS (
    SELECT @organization_id::text AS organization_id
)
SELECT (
    (SELECT count(*) FROM channels_messages m, target
     WHERE m.organization_id = target.organization_id AND m.id <> ALL(@message_ids::uuid[]))
  + (SELECT count(*) FROM channels_conversations c, target
     WHERE c.organization_id = target.organization_id AND c.id <> ALL(@conversation_ids::uuid[]))
  + (SELECT count(*) FROM channels_instances i, target
     WHERE i.organization_id = target.organization_id AND i.id <> ALL(@instance_ids::uuid[]))
)::bigint;

-- name: ImportInstance :exec
-- Conversion-only insert with an explicit identifier so a repeated conversion
-- is a no-op instead of a duplicate.
INSERT INTO channels_instances (id, organization_id, project_id, subject_id, label, note, origin, resume_hash, announce_seq, expires_at)
VALUES (@id, @organization_id, @project_id, @subject_id, @label, @note, @origin, sqlc.narg('resume_hash'), @announce_seq, @expires_at)
ON CONFLICT (id) DO NOTHING;

-- name: ImportConversation :exec
INSERT INTO channels_conversations (id, organization_id, project_id, title, parent_id, private, endpoint_pair_key, created_by_subject_id, announce_seq, last_message_seq, message_count)
VALUES (@id, @organization_id, @project_id, @title, sqlc.narg('parent_id'), @private, sqlc.narg('endpoint_pair_key'), @created_by_subject_id, @announce_seq, @last_message_seq, @message_count)
ON CONFLICT (id) DO NOTHING;

-- name: ImportMessage :exec
INSERT INTO channels_messages (id, organization_id, project_id, conversation_id, instance_id, subject_id, seq, body, idempotency_key, created_at)
VALUES (@id, @organization_id, @project_id, @conversation_id, @instance_id, @subject_id, @seq, @body, @idempotency_key, @created_at)
ON CONFLICT (id) DO NOTHING;

-- name: SetChannelsCounters :exec
-- Lifts the allocators above everything a conversion imported.
UPDATE channels_settings
SET enabled = @enabled,
    message_seq = GREATEST(message_seq, @message_seq::bigint),
    announce_seq = GREATEST(announce_seq, @announce_seq::bigint),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id;
