-- The team memory eval corpus: two synthetic projects with their chats,
-- messages, and the personal accounts that mark private sessions. Platform
-- admins run the memory eval against these projects.

-- name: MemoryEvalInsertProject :exec
-- created_at is the seed's transaction time, the instant the corpus anchor
-- comes from, so a runner rebuilding from it gets the same corpus.
INSERT INTO projects (id, name, slug, organization_id, created_at)
VALUES (@project_id::uuid, @name::text, @slug::text, @org_id::text, @created_at::timestamptz);

-- name: MemoryEvalListPersonalAccounts :many
-- The roster's existing personal Anthropic accounts for one user, so a
-- private corpus session, a Claude Code session, reuses the account the demo
-- already shows for its owner.
SELECT id FROM user_accounts
WHERE organization_id = @organization_id::text AND user_id = @user_id::text
  AND account_type = 'personal' AND provider = 'anthropic'
ORDER BY created_at, id;

-- name: MemoryEvalInsertPersonalAccount :one
-- A personal AI account for the owner of a private corpus session. Sessions
-- attributed to a personal account stay out of team memory.
INSERT INTO user_accounts
  (organization_id, user_id, provider, external_org_id, external_account_uuid,
   external_account_id, email, account_type, billing_mode)
VALUES (@organization_id::text, @user_id::text, 'anthropic', 'demo-ext-org-personal',
        @external_account_uuid::text, @external_account_id::text, @email::text,
        'personal', 'flat_rate')
RETURNING id;

-- name: MemoryEvalCopyChats :copyfrom
INSERT INTO chats (id, project_id, organization_id, user_id, external_user_id,
                   external_chat_id, title, user_account_id, created_at, updated_at)
VALUES (@id, @project_id, @organization_id, @user_id, @external_user_id,
        @external_chat_id, @title, @user_account_id, @created_at, @updated_at);

-- name: MemoryEvalCopyChatMessages :copyfrom
INSERT INTO chat_messages (id, chat_id, project_id, role, content, user_id,
                           source, created_at, risk_analyzed_at)
VALUES (@id, @chat_id, @project_id, @role, @content, @user_id,
        @source, @created_at, @risk_analyzed_at);

-- name: MemoryEvalCountChats :one
SELECT count(*) FROM chats
WHERE organization_id = @org_id::text AND project_id = ANY(@project_ids::uuid[]);

-- name: MemoryEvalListChatIDs :many
SELECT id FROM chats
WHERE organization_id = @org_id::text AND project_id = @project_id::uuid
ORDER BY id;

-- name: MemoryEvalCountChatMessages :one
SELECT count(*) FROM chat_messages
WHERE project_id = @project_id::uuid;
