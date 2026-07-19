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

-- name: InsertSkillObservation :exec
INSERT INTO skill_observations (
    project_id
  , idempotency_key
  , provider
  , user_id
  , user_email
  , hostname
  , session_id
  , skill_name
  , source_level
  , source_path
  , raw_sha256
  , seen_at
) VALUES (
    @project_id
  , sqlc.narg(idempotency_key)
  , @provider
  , sqlc.narg(user_id)
  , sqlc.narg(user_email)
  , sqlc.narg(hostname)
  , sqlc.narg(session_id)
  , @skill_name
  , sqlc.narg(source_level)
  , sqlc.narg(source_path)
  , sqlc.narg(raw_sha256)
  , @seen_at
)
ON CONFLICT (project_id, idempotency_key) WHERE idempotency_key IS NOT NULL
DO NOTHING;

-- name: RememberKnownSkillRawHash :one
WITH inserted AS (
  INSERT INTO skill_raw_hashes (project_id, raw_sha256, canonical_sha256)
  SELECT s.project_id, @raw_sha256, sv.canonical_sha256
  FROM skill_versions sv
  JOIN skills s ON s.id = sv.skill_id
  WHERE s.project_id = @project_id
    AND sv.raw_sha256 = @raw_sha256
  ORDER BY sv.created_at DESC, sv.id DESC
  LIMIT 1
  ON CONFLICT (project_id, raw_sha256) DO NOTHING
  RETURNING 1
)
SELECT (
  EXISTS (
    SELECT 1
    FROM skill_raw_hashes srh
    WHERE srh.project_id = @project_id
      AND srh.raw_sha256 = @raw_sha256
  ) OR EXISTS (SELECT 1 FROM inserted)
)::boolean AS known;

-- name: HasSkillObservationRawHash :one
SELECT EXISTS (
  SELECT 1
  FROM skill_observations so
  WHERE so.project_id = @project_id
    AND so.raw_sha256 = @raw_sha256
)::boolean;

-- name: ListSkillObservations :many
SELECT *
FROM skill_observations
WHERE project_id = @project_id
ORDER BY seen_at ASC, id ASC;

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

-- name: LinkChatUserAccount :execrows
-- Backfills the chat -> user_accounts link for a session whose chat row was
-- created before account attribution landed. A chat is created once, on the
-- session's first persisted message, so a first prompt that beats the first
-- OTEL export would otherwise leave the chat unlinked forever (and the
-- account-identity risk rules blind to it). Fill-once: never overwrites a
-- link that chat creation or an earlier backfill already set.
UPDATE chats
SET user_account_id = @user_account_id
  , updated_at = NOW()
WHERE id = @id
  AND project_id = @project_id
  AND user_account_id IS NULL
  AND deleted IS FALSE;

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
-- billing_mode is intentionally not written here: it is an admin/out-of-band
-- override, never set by ingest. Returning it lets attribution resolve the
-- account-level tier of the billing-mode cascade without a second round trip.
RETURNING id, billing_mode;

-- name: CountEmployeesForExternalOrg :one
-- Distinct employees (resolved Gram users) ever seen under a provider org. An
-- enterprise org is shared by many employees; a personal org maps to exactly one.
-- A count >= 2 marks the org as the company's enterprise org: accounts under it
-- classify team even when their own email has not resolved, and a resolved work
-- email under a solo org is checked against the work-email guard instead.
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

-- name: ListUserAccountsByUsers :many
-- Returns the linked AI accounts for a set of users within an org. Each
-- (provider, email) row is a distinct account, so a user may have several across
-- providers. Used to attach a per-user accounts breakdown to usage summaries on
-- the employees list. Ordered team-first, then by provider for stable display.
SELECT id, user_id, provider, email, account_type, external_org_id, last_seen_at
FROM user_accounts
WHERE organization_id = @organization_id
  AND user_id = ANY(@user_ids::text[])
  AND deleted_at IS NULL
ORDER BY user_id, account_type DESC, provider, last_seen_at DESC;

-- name: GetProviderOrgBillingMode :one
-- Resolves the org-level admin-declared billing mode for a provider org from the
-- org's AI integration config (the org-level tier of the billing-mode cascade).
-- A config scoped to a specific external_organization_id must match the session's
-- provider org; a config with none applies provider-wide. Exact-org matches are
-- preferred over provider-wide (NULLS LAST because the comparison is NULL for a
-- NULL-scoped row, and DESC would otherwise sort NULL ahead of an exact match).
-- Only one live config per (org, provider) can exist today, so the ordering is
-- defensive. Only configs with a non-null billing_mode are considered, so an
-- undeclared org returns no rows (treated as unknown upstream).
SELECT billing_mode
FROM ai_integration_configs
WHERE organization_id = @organization_id
  AND provider = @provider
  AND enabled = TRUE
  AND deleted IS FALSE
  AND billing_mode IS NOT NULL
  AND (
    external_organization_id IS NULL
    OR external_organization_id = ''
    OR external_organization_id = @external_org_id
  )
ORDER BY (external_organization_id = @external_org_id) DESC NULLS LAST
LIMIT 1;

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
  ORDER BY chat_messages.created_at DESC, chat_messages.seq DESC
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
