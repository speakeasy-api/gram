-- Team memory fixtures: summary and exclusion states the background summary
-- job and the dashboard set in production, plus readback for memo writes.

-- name: GetMemoryMemoFixture :one
SELECT content, author_user_id, chat_id
FROM memory_memos
WHERE id = @id AND project_id = @project_id;

-- name: CountMemoryMemosFixture :one
SELECT count(*) FROM memory_memos;

-- name: SetChatMemorySummaryFixture :exec
-- Stands in for the summary job: a masked summary memory may read.
UPDATE chats
SET summary = @summary, summary_generated_at = @summary_generated_at, summary_redacted = true
WHERE id = @id;

-- name: SetChatSummaryUnredactedFixture :exec
UPDATE chats SET summary_redacted = false WHERE id = @id;

-- name: ExcludeChatFromMemoryFixture :exec
UPDATE chats SET memory_excluded_at = clock_timestamp() WHERE id = @id;

-- name: InsertMemoryDigestFixture :one
INSERT INTO memory_digests (organization_id, project_id, level, period_start, period_end, content)
VALUES (@organization_id, @project_id, @level, @period_start, @period_end, @content)
RETURNING id;
