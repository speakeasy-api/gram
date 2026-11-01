-- Test-only fixtures for the team memory digest job.

-- name: SetChatMemorySummaryFixture :exec
UPDATE chats
SET summary = @summary,
    summary_generated_at = @summary_generated_at,
    summary_redacted = @summary_redacted,
    memory_excluded_at = sqlc.narg(memory_excluded_at)
WHERE id = @id;

-- name: InsertMemoFixture :one
INSERT INTO memory_memos (organization_id, project_id, chat_id, content, created_at)
VALUES (@organization_id, @project_id, sqlc.narg(chat_id), @content, @created_at)
RETURNING id;

-- name: GetDigestSourcesFixture :one
SELECT chat_ids, memo_ids, child_digest_ids
FROM memory_digests
WHERE project_id = @project_id
  AND level = @level
  AND period_start = @period_start;
