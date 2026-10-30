-- Team memory (search_team_memory, remember). Every read and write carries
-- organization and project in its row filter, so a caller can never reach
-- another project's memory through a guessed id.

-- name: GetTeamMemoryChat :one
-- Confirms a chat a memo cites lives in this project and is not deleted.
SELECT c.id
FROM chats c
JOIN projects p ON p.id = c.project_id AND p.organization_id = @organization_id AND p.deleted IS FALSE
WHERE c.id = @chat_id AND c.project_id = @project_id AND c.organization_id = @organization_id
  AND c.deleted IS FALSE;

-- name: InsertMemoryMemo :one
INSERT INTO memory_memos (organization_id, project_id, chat_id, author_user_id, content)
VALUES (@organization_id, @project_id, sqlc.narg('chat_id'), @author_user_id, @content)
RETURNING id, created_at;
