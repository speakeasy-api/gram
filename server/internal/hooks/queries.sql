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
  , user_account_id
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
    sqlc.narg(user_account_id),
    @title,
    NOW(),
    NOW()
)
ON CONFLICT (id) DO UPDATE SET
    updated_at = NOW()
  , user_account_id = COALESCE(EXCLUDED.user_account_id, chats.user_account_id)
RETURNING id;

-- name: UpdateClaudeCodeSessionTimestamp :exec
UPDATE chats SET updated_at = NOW() WHERE id = @id AND project_id = @project_id;

-- name: UpsertUserAccount :one
-- Records the external AI provider account observed for a session, keyed by the
-- provider's stable per-account id. COALESCE on conflict keeps a previously
-- learned owner/email/account_id rather than clobbering it with a null from a
-- later session that lacked that field. The conflict target carries the partial
-- index predicate so it matches user_accounts_org_provider_external_account_uuid_key.
INSERT INTO user_accounts (
    organization_id
  , provider
  , external_account_uuid
  , user_id
  , external_org_id
  , external_account_id
  , email
  , account_type
) VALUES (
    @organization_id
  , @provider
  , @external_account_uuid
  , sqlc.narg(user_id)
  , sqlc.narg(external_org_id)
  , sqlc.narg(external_account_id)
  , sqlc.narg(email)
  , sqlc.narg(account_type)
)
ON CONFLICT (organization_id, provider, external_account_uuid) WHERE deleted_at IS NULL
DO UPDATE SET
    user_id             = COALESCE(EXCLUDED.user_id, user_accounts.user_id)
  , external_org_id     = COALESCE(EXCLUDED.external_org_id, user_accounts.external_org_id)
  , external_account_id = COALESCE(EXCLUDED.external_account_id, user_accounts.external_account_id)
  , email               = COALESCE(EXCLUDED.email, user_accounts.email)
  , account_type        = COALESCE(EXCLUDED.account_type, user_accounts.account_type)
  , last_seen_at        = clock_timestamp()
  , updated_at          = clock_timestamp()
RETURNING id;

-- name: CountEmployeesForExternalOrg :one
-- Distinct employees (resolved Gram users) ever seen under a provider org. An
-- enterprise org is shared by many employees; a personal org maps to exactly one.
-- Used to tell an enterprise account from a personal account on a work email.
SELECT COUNT(DISTINCT user_id)::bigint
FROM user_accounts
WHERE organization_id = @organization_id
  AND provider = @provider
  AND external_org_id = @external_org_id
  AND user_id IS NOT NULL
  AND deleted_at IS NULL;

-- name: EmployeeHasSharedExternalOrg :one
-- Whether this employee also appears under a DIFFERENT provider org that is
-- shared by >= 2 employees (i.e. the company's real enterprise org). If so, a
-- solo provider org for the same employee is almost certainly a personal account
-- signed in with their work email, and should not be classified team.
SELECT EXISTS (
  SELECT 1
  FROM user_accounts mine
  WHERE mine.organization_id = @organization_id
    AND mine.provider = @provider
    AND mine.user_id = @user_id
    AND mine.deleted_at IS NULL
    AND mine.external_org_id IS NOT NULL
    AND mine.external_org_id <> @external_org_id
    AND (
      SELECT COUNT(DISTINCT peers.user_id)
      FROM user_accounts peers
      WHERE peers.organization_id = mine.organization_id
        AND peers.provider = mine.provider
        AND peers.external_org_id = mine.external_org_id
        AND peers.user_id IS NOT NULL
        AND peers.deleted_at IS NULL
    ) >= 2
)::boolean;

-- name: GetUserAccount :one
SELECT * FROM user_accounts
WHERE organization_id = @organization_id
  AND provider = @provider
  AND external_account_uuid = @external_account_uuid
  AND deleted_at IS NULL;

-- name: GetDeviceOwner :one
SELECT * FROM device_owners
WHERE organization_id = @organization_id
  AND provider = @provider
  AND device_id = @device_id
  AND deleted_at IS NULL;

-- name: UpsertDeviceOwner :one
-- Records (and over time, links to an employee) a per-device anonymous id so a
-- personal account seen on the same device can be attributed to the employee
-- learned from a team session. COALESCE keeps a known owner if a later session
-- arrives without one. Returns the linked employee (NULL until one is learned).
INSERT INTO device_owners (
    organization_id
  , provider
  , device_id
  , linked_user_id
) VALUES (
    @organization_id
  , @provider
  , @device_id
  , sqlc.narg(linked_user_id)
)
ON CONFLICT (organization_id, provider, device_id) WHERE deleted_at IS NULL
DO UPDATE SET
    linked_user_id = COALESCE(EXCLUDED.linked_user_id, device_owners.linked_user_id)
  , last_seen_at   = clock_timestamp()
  , updated_at     = clock_timestamp()
RETURNING linked_user_id;

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

-- name: InsertShadowMCPBlockResult :exec
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
VALUES (
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
