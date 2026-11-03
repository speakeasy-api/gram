-- Organizational Memory: masked session summaries, memos and the day, week
-- and month digests the nightly rollup would have built from them. Every row
-- hangs off the demo project, so the projects delete in the preflight
-- cascades it away before each reseed.

-- Marks a bulk chat as summarized from its masked transcript, the state
-- memory reads. excluded keeps it out of memory the way the session sheet's
-- switch does.
-- name: MemorySetChatSummary :exec
UPDATE chats
SET summary = @summary::text,
    summary_redacted = TRUE,
    summary_generated_at = @generated_at::timestamptz,
    memory_excluded_at = CASE WHEN @excluded::boolean THEN @generated_at::timestamptz ELSE NULL END
WHERE id = @id AND project_id = @project_id;

-- name: MemoryInsertMemo :exec
INSERT INTO memory_memos (id, organization_id, project_id, chat_id, author_user_id, content, created_at, updated_at)
VALUES (@id, @organization_id, @project_id, @chat_id, @author_user_id::text, @content,
        @created_at::timestamptz, @created_at::timestamptz);

-- name: MemoryInsertDigest :exec
INSERT INTO memory_digests (id, organization_id, project_id, level, period_start, period_end, content,
                            chat_ids, memo_ids, child_digest_ids, model, generated_at, created_at, updated_at)
VALUES (@id, @organization_id, @project_id, @level, @period_start::date, @period_end::date, @content,
        coalesce(@chat_ids::uuid[], '{}'), coalesce(@memo_ids::uuid[], '{}'),
        coalesce(@child_digest_ids::uuid[], '{}'), 'demo-model',
        @generated_at::timestamptz, @generated_at::timestamptz, @generated_at::timestamptz);

-- name: MemoryCountRemembered :one
SELECT count(*) FROM chats
WHERE project_id = @project_id AND summary_redacted IS TRUE AND summary IS NOT NULL
  AND memory_excluded_at IS NULL AND deleted IS FALSE;

-- name: MemoryCountDigests :one
SELECT count(*) FROM memory_digests WHERE project_id = @project_id;

-- name: MemoryCountMemos :one
SELECT count(*) FROM memory_memos WHERE project_id = @project_id;
