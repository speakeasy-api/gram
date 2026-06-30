-- name: ListHooksServerNameOverrides :many
SELECT id, raw_server_name, display_name, created_at, updated_at
FROM hooks_server_name_overrides
WHERE project_id = $1
ORDER BY display_name, raw_server_name;

-- name: UpsertHooksServerNameOverride :one
INSERT INTO hooks_server_name_overrides (project_id, raw_server_name, display_name)
VALUES ($1, $2, $3)
ON CONFLICT (project_id, raw_server_name)
DO UPDATE SET display_name = EXCLUDED.display_name, updated_at = clock_timestamp()
RETURNING *;

-- name: DeleteHooksServerNameOverride :exec
DELETE FROM hooks_server_name_overrides
WHERE id = $1 AND project_id = $2;

-- name: UpsertClaudeCodeSession :one
INSERT INTO chats (
    id
  , project_id
  , organization_id
  , user_id
  , external_user_id
  , title
  , created_at
  , updated_at
)
VALUES (
    @id,
    @project_id,
    @organization_id,
    @user_id,
    @external_user_id,
    @title,
    NOW(),
    NOW()
)
ON CONFLICT (id) DO UPDATE SET
    updated_at = NOW()
  , user_id = COALESCE(NULLIF(EXCLUDED.user_id, ''), chats.user_id)
  , external_user_id = COALESCE(NULLIF(EXCLUDED.external_user_id, ''), chats.external_user_id)
RETURNING id;

-- name: UpdateClaudeCodeSessionTimestamp :exec
UPDATE chats SET updated_at = NOW() WHERE id = @id AND project_id = @project_id;

-- name: FindAssistantToolCallMessageID :one
SELECT id
FROM chat_messages
WHERE project_id = sqlc.arg(project_id)
  AND chat_id = sqlc.arg(chat_id)
  AND role = 'assistant'
  AND tool_calls IS NOT NULL
  AND EXISTS (
    SELECT 1
    FROM jsonb_array_elements(tool_calls) tc
    WHERE tc->>'id' = sqlc.arg(tool_call_id)::text
  )
ORDER BY created_at DESC
LIMIT 1;

-- name: BackfillLatestClaudeUserMessagePromptID :execrows
WITH latest_user_message AS (
  SELECT chat_messages.id
  FROM chat_messages
  WHERE chat_messages.chat_id = sqlc.arg(chat_id)
    AND (chat_messages.project_id IS NULL OR chat_messages.project_id = sqlc.arg(project_id)::uuid)
    AND chat_messages.role = 'user'
  ORDER BY chat_messages.seq DESC
  LIMIT 1
)
UPDATE chat_messages
SET message_id = sqlc.arg(message_id)
WHERE chat_messages.id = (SELECT latest_user_message.id FROM latest_user_message)
  AND sqlc.arg(message_id)::text <> ''
  AND (chat_messages.message_id IS NULL OR chat_messages.message_id = '' OR chat_messages.message_id != sqlc.arg(message_id)::text);

-- name: AcquireShadowMCPDedupeLock :exec
-- Transaction-level advisory lock keyed on the shadow-MCP dedupe tuple. Held with
-- InsertShadowMCPBlockResult in one transaction, it serializes concurrent
-- duplicate-install deliveries so the NOT EXISTS check and the insert are atomic
-- without a unique constraint (risk_results has only a plain index on the tuple).
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(dedupe_key)::text, 0));

-- name: InsertShadowMCPBlockResult :exec
-- Dedupe live hook-time block findings across duplicate plugin installs: each
-- install delivers its own block with a distinct id and idempotency token, but
-- they resolve to the same (project, policy, version, chat_message). Run under
-- AcquireShadowMCPDedupeLock in the same transaction, the NOT EXISTS guard
-- collapses them atomically.
INSERT INTO risk_results (
    id
  , project_id
  , organization_id
  , risk_policy_id
  , risk_policy_version
  , chat_message_id
  , source
  , found
  , rule_id
  , description
  , match
  , confidence
)
SELECT
    sqlc.arg(id)
  , sqlc.arg(project_id)
  , sqlc.arg(organization_id)
  , sqlc.arg(risk_policy_id)
  , sqlc.arg(risk_policy_version)
  , sqlc.arg(chat_message_id)
  , 'shadow_mcp'
  , TRUE
  , 'shadow_mcp.unverified_call'
  , sqlc.arg(description)
  , sqlc.arg(match)
  , sqlc.arg(confidence)
WHERE NOT EXISTS (
  SELECT 1
  FROM risk_results existing
  WHERE existing.project_id = sqlc.arg(project_id)
    AND existing.risk_policy_id = sqlc.arg(risk_policy_id)
    AND existing.risk_policy_version = sqlc.arg(risk_policy_version)
    AND existing.chat_message_id = sqlc.arg(chat_message_id)
    AND existing.source = 'shadow_mcp'
);

-- name: InsertToolCallBlock :exec
-- Records a durable block row at hook-time deny. The reason is captured verbatim
-- so the block page renders from this row alone; the risk_result_id / chat
-- foreign keys are optional enrichment set when those rows are known synchronously.
-- user_id is the Gram user whose agent was blocked (empty string when unresolved)
-- and is used to authorize the block page.
INSERT INTO tool_call_blocks (
    id
  , organization_id
  , project_id
  , provider
  , reason
  , tool_name
  , risk_policy_id
  , risk_result_id
  , chat_id
  , chat_message_id
  , user_id
) VALUES (
    sqlc.arg(id)
  , sqlc.arg(organization_id)
  , sqlc.arg(project_id)
  , sqlc.arg(provider)
  , sqlc.arg(reason)
  , sqlc.narg(tool_name)
  , sqlc.narg(risk_policy_id)
  , sqlc.narg(risk_result_id)
  , sqlc.narg(chat_id)
  , sqlc.narg(chat_message_id)
  , sqlc.arg(user_id)
);
