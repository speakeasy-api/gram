-- name: GetAssistantThreadAssistantIDByChatID :one
SELECT t.assistant_id
FROM assistant_threads t
WHERE t.chat_id = @chat_id
  AND t.project_id = @project_id
  AND t.deleted IS FALSE
LIMIT 1;

-- name: AssistantExistsInProject :one
-- Reports whether an undeleted assistant with this id exists in the given
-- project. Gates setup-thread linking so a client-supplied assistant id that
-- belongs to another project can never create a cross-project
-- assistant_threads row (the FK alone only proves the assistant exists
-- *somewhere*, not that it belongs to the caller's project).
SELECT EXISTS (
  SELECT 1 FROM assistants
  WHERE id = @assistant_id
    AND project_id = @project_id
    AND deleted IS FALSE
) AS assistant_exists;

-- name: UpsertSetupAssistantThread :one
-- Links a client-side setup/onboarding chat to its assistant so the chat is
-- listable (chat.list?assistant_id=) and URL-addressable like runtime threads.
-- Mirrors the runtime UpsertAssistantThread idempotency (ON CONFLICT on
-- project_id/assistant_id/correlation_id). Fixed source_kind='setup' marks the
-- row as a client-driven onboarding thread: it enqueues no runtime events, so
-- the active-thread accounting excludes 'setup' and it never consumes
-- max_concurrency or a warm-pool slot. Unlike the runtime upsert this does NOT
-- refresh last_event_at on conflict, but that is NOT a second safety net: a
-- setup row's last_event_at defaults to clock_timestamp() at insert, so it is
-- recent and falls inside the warm window like any live thread. The
-- source_kind <> 'setup' predicate in CountActiveAssistantThreads is therefore
-- the SOLE guard keeping setup threads out of concurrency/warm accounting — if
-- that filter regressed, setup threads would be counted.
INSERT INTO assistant_threads (
  assistant_id,
  project_id,
  correlation_id,
  chat_id,
  source_kind
) VALUES (
  @assistant_id,
  @project_id,
  @correlation_id,
  @chat_id,
  'setup'
)
ON CONFLICT (project_id, assistant_id, correlation_id) WHERE deleted IS FALSE
DO UPDATE SET
  updated_at = clock_timestamp()
RETURNING id;

-- name: UpsertChat :one
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
-- On conflict, self-heal a soft-deleted chat that still backs a live assistant
-- thread: the runtime keeps writing to it, so it must not stay marked deleted (a
-- deleted chat wedges the thread — compaction-persist 404s on it). Scoped to
-- thread-backed chats so a write does NOT resurrect a plain chat a user
-- intentionally deleted. The first WHEN keeps the common (non-deleted) path a
-- no-op without touching assistant_threads, so the EXISTS stays off the
-- /chat/completions hot path and only runs for the rare already-deleted row. The
-- assistant join means a deleted assistant's leftover thread can't heal the
-- chat. The SET also guarantees RETURNING yields a row whether the chat was
-- newly inserted or already existed.
ON CONFLICT (id) DO UPDATE SET deleted_at = CASE
    WHEN chats.deleted_at IS NULL THEN NULL
    WHEN chats.project_id = EXCLUDED.project_id AND EXISTS (
        SELECT 1 FROM assistant_threads t
        JOIN assistants a ON a.id = t.assistant_id
        WHERE t.chat_id = chats.id AND t.project_id = chats.project_id
          AND t.deleted IS FALSE AND a.deleted IS FALSE
    ) THEN NULL
    ELSE chats.deleted_at
END
RETURNING id;

-- name: UpsertExternalChat :one
INSERT INTO chats (
    id
  , project_id
  , organization_id
  , user_id
  , external_user_id
  , external_chat_id
  , title
  , created_at
  , updated_at
)
VALUES (
    @id
  , @project_id
  , @organization_id
  , @user_id
  , @external_user_id
  , @external_chat_id
  , @title
  , @created_at
  , @updated_at
)
ON CONFLICT (organization_id, external_chat_id) WHERE external_chat_id IS NOT NULL
DO UPDATE SET
    project_id = EXCLUDED.project_id
  , user_id = COALESCE(EXCLUDED.user_id, chats.user_id)
  , external_user_id = COALESCE(EXCLUDED.external_user_id, chats.external_user_id)
  , title = COALESCE(EXCLUDED.title, chats.title)
  , updated_at = GREATEST(chats.updated_at, EXCLUDED.updated_at)
RETURNING id;

-- name: LinkAIIntegrationConfigChat :one
-- Links a chat to the AI integration config that imported it and returns the
-- chat's persisted message pagination cursor so imports resume where the last
-- successful page ended.
INSERT INTO ai_integration_config_chats (
    ai_integration_config_id
  , chat_id
)
VALUES (
    @ai_integration_config_id
  , @chat_id
)
ON CONFLICT (chat_id)
DO UPDATE SET
    ai_integration_config_id = EXCLUDED.ai_integration_config_id
  , updated_at = clock_timestamp()
RETURNING last_cursor_id;

-- name: UpdateAIIntegrationConfigChatCursor :exec
UPDATE ai_integration_config_chats
SET last_cursor_id = @last_cursor_id
  , updated_at = clock_timestamp()
WHERE chat_id = @chat_id;

-- name: CreateChatMessage :copyfrom
INSERT INTO chat_messages (
    chat_id
  , role
  , project_id
  , content
  , content_raw
  , content_asset_url
  , storage_error
  , model
  , message_id
  , tool_call_id
  , user_id
  , external_user_id
  , finish_reason
  , tool_calls
  , prompt_tokens
  , completion_tokens
  , total_tokens
  , origin
  , user_agent
  , ip_address
  , source
  , content_hash
  , generation
)
VALUES (
    @chat_id
  , @role
  , @project_id::uuid
  , @content
  , @content_raw
  , @content_asset_url
  , @storage_error
  , @model
  , @message_id
  , @tool_call_id
  , @user_id
  , @external_user_id
  , @finish_reason
  , @tool_calls
  , @prompt_tokens
  , @completion_tokens
  , @total_tokens
  , @origin
  , @user_agent
  , @ip_address
  , @source
  , @content_hash
  , @generation
);

-- name: CreateExternalChatMessage :execrows
INSERT INTO chat_messages (
    chat_id
  , role
  , project_id
  , content
  , content_raw
  , content_asset_url
  , storage_error
  , model
  , message_id
  , tool_call_id
  , user_id
  , external_user_id
  , external_message_id
  , finish_reason
  , tool_calls
  , prompt_tokens
  , completion_tokens
  , total_tokens
  , origin
  , user_agent
  , ip_address
  , source
  , content_hash
  , generation
  , created_at
)
VALUES (
    @chat_id
  , @role
  , @project_id::uuid
  , @content
  , @content_raw
  , @content_asset_url
  , @storage_error
  , @model
  , @message_id
  , @tool_call_id
  , @user_id
  , @external_user_id
  , @external_message_id
  , @finish_reason
  , @tool_calls
  , @prompt_tokens
  , @completion_tokens
  , @total_tokens
  , @origin
  , @user_agent
  , @ip_address
  , @source
  , @content_hash
  , @generation
  , @created_at
)
ON CONFLICT (chat_id, external_message_id) WHERE external_message_id IS NOT NULL
DO NOTHING;

-- name: CountChats :one
-- risk_counts pre-aggregates active findings per chat once for the whole
-- project (one pass over risk_results), so the risk presence + threshold
-- filters become a cheap join instead of a correlated subquery per chat.
WITH risk_counts AS (
  SELECT cm.chat_id, COUNT(*)::integer AS cnt
  FROM risk_results rr
  JOIN chat_messages cm ON cm.id = rr.chat_message_id
  -- A finding only counts while its policy is still enabled and not deleted, so
  -- disabling/deleting a policy retires its findings everywhere (keeps this
  -- count in sync with the risk.results.list detail view).
  JOIN risk_policies rp ON rp.id = rr.risk_policy_id AND rp.deleted IS FALSE AND rp.enabled IS TRUE
  WHERE rr.project_id = @project_id
    AND rr.found IS TRUE
    AND rr.excluded_at IS NULL
    AND rr.false_positive_at IS NULL
  GROUP BY cm.chat_id
),
candidate_chats AS (
  SELECT c.id, c.created_at
  FROM chats c
  LEFT JOIN risk_counts rc ON rc.chat_id = c.id
  LEFT JOIN user_accounts ua ON ua.id = c.user_account_id AND ua.organization_id = c.organization_id AND ua.deleted_at IS NULL
  WHERE c.project_id = @project_id
    AND c.deleted IS FALSE
    AND (@external_user_id = '' OR c.external_user_id = @external_user_id)
    AND (@user_id = '' OR c.user_id = @user_id)
    AND (
      @pinned::text = ''
      OR (@pinned::text = 'true' AND c.pinned_at IS NOT NULL)
      OR (@pinned::text = 'false' AND c.pinned_at IS NULL)
    )
    AND (
      @search = ''
      OR c.id::text ILIKE '%' || @search || '%'
      OR c.external_user_id ILIKE '%' || @search || '%'
      OR c.title ILIKE '%' || @search || '%'
    )
    AND (
      @assistant_id = ''
      OR EXISTS (
        SELECT 1 FROM assistant_threads at
        WHERE at.chat_id = c.id
          AND at.assistant_id = @assistant_id::uuid
          AND at.deleted IS FALSE
          -- Optional source-kind dimension so setup/onboarding and runtime
          -- threads for the same assistant don't pollute each other's listing.
          -- @source_kind keeps only threads of that kind (onboarding passes
          -- 'setup'); @exclude_source_kind drops threads of that kind (runtime
          -- views pass 'setup'). Empty string on either disables that side.
          AND (@source_kind::text = '' OR at.source_kind = @source_kind::text)
          AND (@exclude_source_kind::text = '' OR at.source_kind <> @exclude_source_kind::text)
      )
    )
    AND (
      @has_risk_filter::text = ''
      OR (@has_risk_filter::text = 'true' AND COALESCE(rc.cnt, 0) > 0)
      OR (@has_risk_filter::text = 'false' AND COALESCE(rc.cnt, 0) = 0)
    )
    AND (
      @account_type::text = ''
      OR ua.account_type = @account_type::text
      -- Rows without a classified account type are treated as 'team' so the
      -- team filter stays backwards-compatible with pre-classification chats.
      OR (
        @account_type::text = 'team'
        AND (ua.account_type IS NULL OR ua.account_type = '')
      )
    )
    AND (
      @min_risk_score::int < 0
      OR COALESCE(rc.cnt, 0) >= @min_risk_score::int
    )
    AND (
      coalesce(cardinality(@sources::text[]), 0) = 0
      OR (
        SELECT cmsrc.source
        FROM chat_messages cmsrc
        WHERE cmsrc.chat_id = c.id
          AND cmsrc.source IS NOT NULL
          AND cmsrc.source <> ''
        ORDER BY cmsrc.created_at DESC
        LIMIT 1
      ) = ANY (@sources::text[])
    )
),
chat_activity AS (
  SELECT
    cc.id,
    COALESCE(MAX(cm.created_at), cc.created_at) AS last_message_timestamp
  FROM candidate_chats cc
  LEFT JOIN chat_messages cm ON cm.chat_id = cc.id
  GROUP BY cc.id, cc.created_at
)
SELECT COUNT(*) AS total
FROM chat_activity ca
WHERE (@from_time::timestamptz IS NULL OR ca.last_message_timestamp >= @from_time)
  AND (@to_time::timestamptz IS NULL OR ca.last_message_timestamp <= @to_time);

-- name: ListChats :many
-- risk_counts pre-aggregates active findings per chat once for the whole
-- project (one pass over risk_results). It feeds both the risk presence +
-- threshold filters and the risk_findings_count column below, replacing what
-- were two correlated subqueries per candidate chat.
WITH risk_counts AS (
  SELECT cm.chat_id, COUNT(*)::integer AS cnt
  FROM risk_results rr
  JOIN chat_messages cm ON cm.id = rr.chat_message_id
  -- A finding only counts while its policy is still enabled and not deleted, so
  -- disabling/deleting a policy retires its findings everywhere (keeps this
  -- count in sync with the risk.results.list detail view).
  JOIN risk_policies rp ON rp.id = rr.risk_policy_id AND rp.deleted IS FALSE AND rp.enabled IS TRUE
  WHERE rr.project_id = @project_id
    AND rr.found IS TRUE
    AND rr.excluded_at IS NULL
    AND rr.false_positive_at IS NULL
  GROUP BY cm.chat_id
),
candidate_chats AS (
  SELECT
    c.id,
    c.title,
    c.user_id,
    c.external_user_id,
    c.created_at,
    c.updated_at,
    COALESCE(rc.cnt, 0) AS risk_findings_count,
    COALESCE(ua.account_type, '')::text AS account_type,
    COALESCE(ua.email, '')::text AS account_email
  FROM chats c
  LEFT JOIN risk_counts rc ON rc.chat_id = c.id
  -- Resolve the AI account that produced the chat (chats.user_account_id has no FK,
  -- matching chats.user_id) to expose its team/personal classification.
  LEFT JOIN user_accounts ua ON ua.id = c.user_account_id AND ua.organization_id = c.organization_id AND ua.deleted_at IS NULL
  WHERE c.project_id = @project_id
    AND c.deleted IS FALSE
    AND (@external_user_id = '' OR c.external_user_id = @external_user_id)
    AND (@user_id = '' OR c.user_id = @user_id)
    AND (
      @pinned::text = ''
      OR (@pinned::text = 'true' AND c.pinned_at IS NOT NULL)
      OR (@pinned::text = 'false' AND c.pinned_at IS NULL)
    )
    AND (
      @search = ''
      OR c.id::text ILIKE '%' || @search || '%'
      OR c.external_user_id ILIKE '%' || @search || '%'
      OR c.title ILIKE '%' || @search || '%'
    )
    AND (
      @assistant_id = ''
      OR EXISTS (
        SELECT 1 FROM assistant_threads at
        WHERE at.chat_id = c.id
          AND at.assistant_id = @assistant_id::uuid
          AND at.deleted IS FALSE
          -- Optional source-kind dimension so setup/onboarding and runtime
          -- threads for the same assistant don't pollute each other's listing.
          -- @source_kind keeps only threads of that kind (onboarding passes
          -- 'setup'); @exclude_source_kind drops threads of that kind (runtime
          -- views pass 'setup'). Empty string on either disables that side.
          AND (@source_kind::text = '' OR at.source_kind = @source_kind::text)
          AND (@exclude_source_kind::text = '' OR at.source_kind <> @exclude_source_kind::text)
      )
    )
    AND (
      @has_risk_filter::text = ''
      OR (@has_risk_filter::text = 'true' AND COALESCE(rc.cnt, 0) > 0)
      OR (@has_risk_filter::text = 'false' AND COALESCE(rc.cnt, 0) = 0)
    )
    AND (
      @account_type::text = ''
      OR ua.account_type = @account_type::text
      -- Rows without a classified account type are treated as 'team' so the
      -- team filter stays backwards-compatible with pre-classification chats.
      OR (
        @account_type::text = 'team'
        AND (ua.account_type IS NULL OR ua.account_type = '')
      )
    )
    AND (
      @min_risk_score::int < 0
      OR COALESCE(rc.cnt, 0) >= @min_risk_score::int
    )
    AND (
      coalesce(cardinality(@sources::text[]), 0) = 0
      OR (
        SELECT cmsrc.source
        FROM chat_messages cmsrc
        WHERE cmsrc.chat_id = c.id
          AND cmsrc.source IS NOT NULL
          AND cmsrc.source <> ''
        ORDER BY cmsrc.created_at DESC
        LIMIT 1
      ) = ANY (@sources::text[])
    )
),
chat_stats AS (
  SELECT
    cc.id,
    COUNT(cm.id)::integer AS num_messages,
    COALESCE(MAX(cm.created_at), cc.created_at) AS last_message_timestamp
  FROM candidate_chats cc
  LEFT JOIN chat_messages cm ON cm.chat_id = cc.id
  GROUP BY cc.id, cc.created_at
),
filtered_chats AS (
  SELECT
    cc.id,
    cc.title,
    cc.user_id,
    cc.external_user_id,
    cc.created_at,
    cc.updated_at,
    cs.num_messages,
    cs.last_message_timestamp,
    cc.risk_findings_count,
    cc.account_type,
    cc.account_email
  FROM candidate_chats cc
  JOIN chat_stats cs ON cs.id = cc.id
  WHERE (@from_time::timestamptz IS NULL OR cs.last_message_timestamp >= @from_time)
    AND (@to_time::timestamptz IS NULL OR cs.last_message_timestamp <= @to_time)
),
limited_chats AS (
  SELECT
    fc.id,
    fc.title,
    fc.user_id,
    fc.external_user_id,
    fc.created_at,
    fc.updated_at,
    fc.num_messages,
    (SELECT source FROM chat_messages WHERE chat_id = fc.id AND source IS NOT NULL AND source <> '' ORDER BY created_at DESC LIMIT 1) AS source,
    fc.last_message_timestamp,
    fc.risk_findings_count,
    fc.account_type,
    fc.account_email
  FROM filtered_chats fc
  ORDER BY
    CASE WHEN @sort_by = 'last_message_timestamp' AND @sort_order = 'desc' THEN fc.last_message_timestamp END DESC NULLS LAST,
    CASE WHEN @sort_by = 'last_message_timestamp' AND @sort_order = 'asc' THEN fc.last_message_timestamp END ASC NULLS LAST,
    CASE WHEN @sort_by = 'num_messages' AND @sort_order = 'desc' THEN fc.num_messages END DESC NULLS LAST,
    CASE WHEN @sort_by = 'num_messages' AND @sort_order = 'asc' THEN fc.num_messages END ASC NULLS LAST,
    fc.last_message_timestamp DESC,
    fc.id DESC
  LIMIT @page_limit
  OFFSET @page_offset
)
SELECT
  lc.id,
  lc.title,
  lc.user_id,
  lc.external_user_id,
  lc.source,
  lc.created_at,
  lc.updated_at,
  lc.num_messages,
  lc.last_message_timestamp,
  lc.risk_findings_count,
  lc.account_type,
  lc.account_email
FROM limited_chats lc;

-- name: ListChatSources :many
-- Distinct inferred source (the latest non-null message source) across the
-- project's chats, honoring the same visibility scoping as ListChats. Feeds the
-- agent-type filter options on the Agent Sessions page so the list reflects the
-- sources actually present in the data rather than a hardcoded catalog.
WITH latest_sources AS (
  SELECT DISTINCT ON (cm.chat_id) cm.source AS source
  FROM chats c
  JOIN chat_messages cm ON cm.chat_id = c.id
  WHERE c.project_id = @project_id
    AND c.deleted IS FALSE
    AND (@external_user_id::text = '' OR c.external_user_id = @external_user_id::text)
    AND (@user_id::text = '' OR c.user_id = @user_id::text)
    AND cm.source IS NOT NULL
    AND cm.source <> ''
  ORDER BY cm.chat_id, cm.created_at DESC
)
SELECT DISTINCT source
FROM latest_sources
WHERE source IS NOT NULL
ORDER BY source;

-- name: GetChat :one
-- Loads a chat plus the team/personal classification of the AI account that
-- produced it (chats.user_account_id has no FK), scoped by organization. Returns
-- '' for account_type/account_email when the chat has no linked account or it
-- is unclassified.
SELECT c.*, COALESCE(ua.account_type, '')::text AS account_type, COALESCE(ua.email, '')::text AS account_email
FROM chats c
LEFT JOIN user_accounts ua ON ua.id = c.user_account_id AND ua.organization_id = c.organization_id AND ua.deleted_at IS NULL
WHERE c.id = @id AND c.deleted IS FALSE;

-- name: GetChatTitlesByIDs :many
SELECT id, title FROM chats
WHERE id = ANY(@ids::uuid[])
  AND project_id = ANY(@project_ids::uuid[])
  AND deleted IS FALSE;

-- name: SumMessageTokenStatsByDay :many
-- Daily message-level token stats for the billing details table
-- (telemetry.queryTumDetails): tokens in messages carrying at least one
-- active risk finding (same active-finding semantics as ListChats'
-- risk_counts), and tokens in tool-call messages — tool results (role 'tool')
-- plus messages carrying a non-empty tool_calls array, mirroring the
-- getTraceEntryType classification in GetChatEntryTotals. Bucketed by the
-- message's UTC day so the series lines up with the ClickHouse daily
-- aggregates.
SELECT
  (date_trunc('day', cm.created_at AT TIME ZONE 'utc'))::timestamp AS day,
  COALESCE(SUM(cm.total_tokens) FILTER (WHERE rm.chat_message_id IS NOT NULL), 0)::bigint AS risky_message_tokens,
  COALESCE(SUM(cm.total_tokens) FILTER (WHERE
    cm.role = 'tool'
    OR CASE
      WHEN cm.tool_calls IS NULL THEN false
      WHEN jsonb_typeof(cm.tool_calls) = 'array'
        THEN jsonb_array_length(cm.tool_calls) > 0
      -- Some rows store tool_calls double-encoded (a JSON string holding the
      -- array); treat any non-empty/non-"[]" string as carrying tool calls.
      WHEN jsonb_typeof(cm.tool_calls) = 'string'
        THEN btrim(cm.tool_calls #>> '{}') NOT IN ('', '[]', 'null')
      ELSE false
    END
  ), 0)::bigint AS tool_message_tokens
FROM chat_messages cm
LEFT JOIN (
  SELECT DISTINCT rr.chat_message_id
  FROM risk_results rr
  JOIN risk_policies rp ON rp.id = rr.risk_policy_id AND rp.deleted IS FALSE AND rp.enabled IS TRUE
  WHERE rr.project_id = ANY(@project_ids::uuid[])
    AND rr.found IS TRUE
    AND rr.excluded_at IS NULL
    AND rr.false_positive_at IS NULL
) rm ON rm.chat_message_id = cm.id
WHERE cm.project_id = ANY(@project_ids::uuid[])
  AND cm.created_at >= @from_time
  AND cm.created_at < @to_time
GROUP BY 1
ORDER BY 1;

-- name: ListRiskyChatIDs :many
-- Distinct chats with at least one active risk finding created in the window,
-- for the token-by-risk breakdown (telemetry.queryRiskTokens). Mirrors the
-- risk_counts semantics used by ListChats: a finding counts only while found,
-- not excluded, not marked false-positive, and its policy is enabled and not
-- deleted.
SELECT DISTINCT cm.chat_id
FROM risk_results rr
JOIN chat_messages cm ON cm.id = rr.chat_message_id
JOIN risk_policies rp ON rp.id = rr.risk_policy_id AND rp.deleted IS FALSE AND rp.enabled IS TRUE
WHERE rr.project_id = ANY(@project_ids::uuid[])
  AND rr.found IS TRUE
  AND rr.excluded_at IS NULL
  AND rr.false_positive_at IS NULL
  AND rr.created_at >= @from_time
  AND rr.created_at < @to_time;

-- name: ListChatMessages :many
SELECT * FROM chat_messages 
WHERE chat_id = @chat_id AND (project_id IS NULL OR project_id = @project_id::uuid) 
ORDER BY seq ASC;

-- name: CountChatMessages :one
-- Must match ListChatMessages' project_id filter, otherwise count and the
-- list drift and the client hits "chat history mismatch" at
-- message_capture_strategy.go.
SELECT COUNT(*) FROM chat_messages
WHERE chat_id = @chat_id AND (project_id IS NULL OR project_id = @project_id::uuid);

-- name: GetChatMessageStats :one
-- Chat-wide aggregates (total message count + most recent message timestamp).
-- Used by loadChat so every paginated response can carry the chat's real
-- last_message_timestamp without depending on chats.updated_at, which is bumped
-- by metadata edits (e.g. title changes) and would drift from the actual last
-- message.
SELECT
  COUNT(*)::bigint AS total,
  MAX(created_at)::timestamptz AS last_message_at
FROM chat_messages
WHERE chat_id = @chat_id AND (project_id IS NULL OR project_id = @project_id::uuid);

-- name: GetChatEntryTotals :one
-- Per-generation trace-entry totals for the chat detail filter bar. The detail
-- sheet paginates messages, so counts derived from the loaded page understate
-- the chat; these totals describe the whole generation regardless of which page
-- is in view. Each message maps to exactly one entry, mirroring the client's
-- getTraceEntryType precedence: a message carrying a non-empty tool_calls array
-- is a tool call regardless of role, otherwise the role decides. risk_findings
-- counts messages with an active (found, non-suppressed) risk result.
WITH ordered AS (
  SELECT
    cm.id,
    cm.role,
    CASE
      WHEN cm.tool_calls IS NULL THEN false
      WHEN jsonb_typeof(cm.tool_calls) = 'array'
        THEN jsonb_array_length(cm.tool_calls) > 0
      -- Some rows store tool_calls double-encoded (a JSON string holding the
      -- array); treat any non-empty/non-"[]" string as carrying tool calls.
      WHEN jsonb_typeof(cm.tool_calls) = 'string'
        THEN btrim(cm.tool_calls #>> '{}') NOT IN ('', '[]', 'null')
      ELSE false
    END AS has_tool_calls
  FROM chat_messages cm
  WHERE cm.chat_id = @chat_id
    AND (cm.project_id IS NULL OR cm.project_id = @project_id::uuid)
    AND cm.generation = @generation::integer
)
SELECT
  COUNT(*) FILTER (WHERE has_tool_calls OR role IN ('user', 'assistant', 'tool'))::bigint AS total,
  COUNT(*) FILTER (WHERE NOT has_tool_calls AND role = 'user')::bigint AS user_messages,
  COUNT(*) FILTER (WHERE NOT has_tool_calls AND role = 'assistant')::bigint AS assistant_messages,
  COUNT(*) FILTER (WHERE has_tool_calls)::bigint AS tool_calls,
  COUNT(*) FILTER (WHERE NOT has_tool_calls AND role = 'tool')::bigint AS tool_results,
  (
    SELECT COUNT(*)::bigint FROM ordered o
    WHERE EXISTS (
      SELECT 1 FROM risk_results rr
      -- Only count a finding while its policy is enabled and not deleted (see
      -- the risk_counts CTE / risk.results.list for the same gate).
      JOIN risk_policies rp ON rp.id = rr.risk_policy_id AND rp.deleted IS FALSE AND rp.enabled IS TRUE
      WHERE rr.chat_message_id = o.id
        AND rr.project_id = @project_id::uuid
        AND rr.found IS TRUE
        AND rr.excluded_at IS NULL
        AND rr.false_positive_at IS NULL
    )
  )::bigint AS risk_findings
FROM ordered;

-- name: ListLatestGenerationChatMessages :many
-- Returns only the latest-generation rows; older generations are audit-only.
SELECT cm.* FROM chat_messages cm
WHERE cm.chat_id = @chat_id
  AND (cm.project_id IS NULL OR cm.project_id = @project_id::uuid)
  AND cm.generation = (SELECT MAX(generation) FROM chat_messages WHERE chat_id = @chat_id)
ORDER BY cm.seq ASC;

-- name: ListChatMessagesByGeneration :many
-- Returns rows for an explicit generation, used to pin a snapshot across
-- multiple reads (e.g. across Temporal activities) so indices stay stable
-- even if a new generation is appended mid-workflow.
SELECT cm.* FROM chat_messages cm
WHERE cm.chat_id = @chat_id
  AND (cm.project_id IS NULL OR cm.project_id = @project_id::uuid)
  AND cm.generation = @generation::integer
ORDER BY cm.seq ASC;

-- name: GetMaxGenerationForChat :one
SELECT COALESCE(MAX(generation), 0)::integer AS generation FROM chat_messages WHERE chat_id = @chat_id;

-- name: ListChatMessagesBeforePage :many
-- Keyset page within a generation, newest first. Returns messages with seq
-- strictly less than @before_seq, or the newest page when @before_seq is NULL.
-- Order DESC so LIMIT keeps the most recent rows; the caller reverses to
-- ascending for display. Fetch @lim = pageSize+1 to detect whether more older
-- rows remain.
SELECT cm.* FROM chat_messages cm
WHERE cm.chat_id = @chat_id
  AND (cm.project_id IS NULL OR cm.project_id = @project_id::uuid)
  AND cm.generation = @generation::integer
  AND (sqlc.narg('before_seq')::bigint IS NULL OR cm.seq < sqlc.narg('before_seq')::bigint)
ORDER BY cm.seq DESC
LIMIT @lim::integer;

-- name: ListChatMessagesAfterPage :many
-- Keyset page within a generation, oldest first. Returns messages with seq
-- strictly greater than @after_seq, or the oldest page (start of the thread)
-- when @after_seq is NULL. Fetch @lim = pageSize+1 to detect whether more newer
-- rows remain.
SELECT cm.* FROM chat_messages cm
WHERE cm.chat_id = @chat_id
  AND (cm.project_id IS NULL OR cm.project_id = @project_id::uuid)
  AND cm.generation = @generation::integer
  AND (sqlc.narg('after_seq')::bigint IS NULL OR cm.seq > sqlc.narg('after_seq')::bigint)
ORDER BY cm.seq ASC
LIMIT @lim::integer;

-- name: ListRiskWindowedMessages :many
-- Risk-only view: returns every message within +/- @context_size ordinal
-- positions of any active risk finding in the generation, ordered oldest to
-- newest. rn is the message's 1-based ordinal within the generation and total
-- is the generation's message count, so the caller can fold consecutive rn into
-- contiguous segments and decide whether earlier (rn > 1) or later (rn < total)
-- messages remain to be expanded. Overlapping windows merge naturally via set
-- membership. is_risk flags the seed rows (the flagged messages themselves) so
-- the caller can return the explicit risk seq list (context rows are
-- is_risk = false).
WITH ordered AS (
  SELECT
    cm.*,
    row_number() OVER (ORDER BY cm.seq) AS rn,
    count(*) OVER () AS total
  FROM chat_messages cm
  WHERE cm.chat_id = @chat_id
    AND (cm.project_id IS NULL OR cm.project_id = @project_id::uuid)
    AND cm.generation = @generation::integer
),
risk_rns AS (
  SELECT o.rn FROM ordered o
  WHERE EXISTS (
    SELECT 1 FROM risk_results rr
    -- is_risk only fires while the finding's policy is enabled and not deleted,
    -- so a since-disabled policy drops out of the "Risky only" view too (matches
    -- risk.results.list, which drives the highlight detail).
    JOIN risk_policies rp ON rp.id = rr.risk_policy_id AND rp.deleted IS FALSE AND rp.enabled IS TRUE
    WHERE rr.chat_message_id = o.id
      AND rr.project_id = @project_id::uuid
      AND rr.found IS TRUE
      AND rr.excluded_at IS NULL
      AND rr.false_positive_at IS NULL
  )
)
SELECT
  o.*,
  EXISTS (SELECT 1 FROM risk_rns r WHERE r.rn = o.rn) AS is_risk
FROM ordered o
WHERE EXISTS (
  SELECT 1 FROM risk_rns r
  WHERE o.rn BETWEEN r.rn - @context_size::bigint AND r.rn + @context_size::bigint
)
ORDER BY o.seq ASC;

-- name: ListSearchWindowedMessages :many
-- Query-search view: same windowing as ListRiskWindowedMessages, but the seed
-- rows are messages whose searchable text matches @query (case-insensitive
-- substring over the narrative content, the tool-call name/arguments JSON, and
-- any structured/multimodal content) instead of messages with a risk finding.
-- Each match is padded with +/- @context_size ordinal positions. rn/total drive
-- segment folding and the has_more flags; is_match flags the seed rows so the
-- caller can return the explicit jump-to-match seq list (context rows are
-- is_match = false). Seed matches are capped at @match_limit (earliest first by
-- ordinal) to bound the response on broad queries. Asset-offloaded content (too
-- large to store inline) is not searchable here.
WITH ordered AS (
  SELECT
    cm.*,
    row_number() OVER (ORDER BY cm.seq) AS rn,
    count(*) OVER () AS total
  FROM chat_messages cm
  WHERE cm.chat_id = @chat_id
    AND (cm.project_id IS NULL OR cm.project_id = @project_id::uuid)
    AND cm.generation = @generation::integer
),
match_rns AS (
  SELECT o.rn FROM ordered o
  WHERE o.content ILIKE '%' || @query::text || '%'
     OR (o.tool_calls IS NOT NULL AND o.tool_calls::text ILIKE '%' || @query::text || '%')
     OR (o.content_raw IS NOT NULL AND o.content_raw::text ILIKE '%' || @query::text || '%')
  ORDER BY o.rn
  LIMIT @match_limit::integer
)
SELECT
  o.*,
  EXISTS (SELECT 1 FROM match_rns m WHERE m.rn = o.rn) AS is_match
FROM ordered o
WHERE EXISTS (
  SELECT 1 FROM match_rns m
  WHERE o.rn BETWEEN m.rn - @context_size::bigint AND m.rn + @context_size::bigint
)
ORDER BY o.seq ASC;

-- name: ListChatMessagesForMatch :many
SELECT id, role, content, tool_call_id, tool_calls
FROM chat_messages
WHERE chat_id = @chat_id AND generation = @generation
ORDER BY seq ASC;

-- name: UpdateChatTitle :exec
-- Auto-generated title write. Guarded on title_manually_set so a manual rename
-- landing during title generation (between the activity's read and this write)
-- is never clobbered: the row no longer matches and the update no-ops.
UPDATE chats SET title = @title, updated_at = NOW()
WHERE id = @id AND title_manually_set IS FALSE;

-- name: RenameChat :exec
-- Set or clear a chat's title and record whether a human chose it. Project-scoped
-- so a manual rename can never touch another project's chat. A NULL title resets
-- to auto-naming (paired with title_manually_set = false).
UPDATE chats
SET title = sqlc.narg('title'),
    title_manually_set = @title_manually_set,
    updated_at = NOW()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE;

-- name: SetChatPinned :exec
-- Pin or unpin a chat. Project-scoped so a pin can never touch another
-- project's chat. COALESCE preserves the original pin time when re-pinning an
-- already-pinned chat; unpinning clears it.
UPDATE chats
SET pinned_at = CASE WHEN @pinned::boolean THEN COALESCE(pinned_at, NOW()) ELSE NULL END,
    updated_at = NOW()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE;

-- name: GetFirstUserChatMessage :one
SELECT content FROM chat_messages
WHERE chat_id = @chat_id
  AND role = 'user'
  AND content IS NOT NULL
  AND content != ''
ORDER BY created_at ASC
LIMIT 1;

-- name: GetToolCallMessages :many
SELECT * FROM chat_messages
WHERE chat_id = @chat_id
  AND role = 'tool'
ORDER BY created_at ASC;

-- name: UpdateToolCallOutcome :exec
UPDATE chat_messages
SET tool_outcome = @tool_outcome,
    tool_outcome_notes = @tool_outcome_notes
WHERE id = @id;

-- name: InsertChatResolution :one
INSERT INTO chat_resolutions (
    project_id,
    chat_id,
    user_goal,
    resolution,
    resolution_notes,
    score
) VALUES (
    @project_id,
    @chat_id,
    @user_goal,
    @resolution,
    @resolution_notes,
    @score
) RETURNING id;

-- name: InsertChatResolutionMessage :exec
INSERT INTO chat_resolution_messages (
    chat_resolution_id,
    message_id
) VALUES (
    @chat_resolution_id,
    @message_id
);

-- name: DeleteChatResolutions :exec
DELETE FROM chat_resolutions WHERE chat_id = @chat_id;

-- name: ListChatResolutions :many
SELECT * FROM chat_resolutions
WHERE chat_id = @chat_id
ORDER BY created_at DESC;

-- name: CountChatsWithResolutions :one
SELECT COUNT(DISTINCT c.id) as total
FROM chats c
WHERE c.project_id = @project_id
  AND c.deleted IS FALSE
  AND (@external_user_id = '' OR c.external_user_id = @external_user_id)
  AND (@from_time::timestamptz IS NULL OR c.created_at >= @from_time)
  AND (@to_time::timestamptz IS NULL OR c.created_at <= @to_time)
  AND (
    @search = ''
    OR c.id::text ILIKE '%' || @search || '%'
    OR c.external_user_id ILIKE '%' || @search || '%'
    OR c.title ILIKE '%' || @search || '%'
  )
  AND (
    @assistant_id = ''
    OR EXISTS (
      SELECT 1 FROM assistant_threads at
      WHERE at.chat_id = c.id
        AND at.assistant_id = @assistant_id::uuid
        AND at.deleted IS FALSE
    )
  )
  AND (
    @resolution_status = ''
    OR (
      @resolution_status = 'unresolved' AND NOT EXISTS (
        SELECT 1 FROM chat_resolutions WHERE chat_id = c.id
      )
    )
    OR (
      @resolution_status != 'unresolved' AND EXISTS (
        SELECT 1 FROM chat_resolutions WHERE chat_id = c.id AND resolution = @resolution_status
      )
    )
  )
  AND (
    @has_risk_filter::text = ''
    OR (
      @has_risk_filter::text = 'true' AND EXISTS (
        SELECT 1 FROM risk_results rr
        JOIN chat_messages cm ON cm.id = rr.chat_message_id
        -- Gate on the policy still being enabled and not deleted so the
        -- has-risk list filter agrees with the per-chat count and detail view.
        JOIN risk_policies rp ON rp.id = rr.risk_policy_id AND rp.deleted IS FALSE AND rp.enabled IS TRUE
        WHERE cm.chat_id = c.id
          AND rr.project_id = @project_id
          AND rr.found IS TRUE
          AND rr.excluded_at IS NULL
          AND rr.false_positive_at IS NULL
      )
    )
    OR (
      @has_risk_filter::text = 'false' AND NOT EXISTS (
        SELECT 1 FROM risk_results rr
        JOIN chat_messages cm ON cm.id = rr.chat_message_id
        -- Gate on the policy still being enabled and not deleted so the
        -- has-risk list filter agrees with the per-chat count and detail view.
        JOIN risk_policies rp ON rp.id = rr.risk_policy_id AND rp.deleted IS FALSE AND rp.enabled IS TRUE
        WHERE cm.chat_id = c.id
          AND rr.project_id = @project_id
          AND rr.found IS TRUE
          AND rr.excluded_at IS NULL
          AND rr.false_positive_at IS NULL
      )
    )
  );

-- name: ListUserFeedbackForChat :many
SELECT *
FROM chat_user_feedback
WHERE chat_id = @chat_id
ORDER BY created_at DESC;

-- name: DeleteChatResolutionsAfterMessage :exec
DELETE FROM chat_resolutions
WHERE id IN (
    SELECT DISTINCT cr.id
    FROM chat_resolutions cr
    JOIN chat_resolution_messages crm ON cr.id = crm.chat_resolution_id
    JOIN chat_messages cm ON crm.message_id = cm.id
    WHERE cr.chat_id = @chat_id
      AND cm.seq > (
        SELECT seq FROM chat_messages WHERE chat_messages.id = @after_message_id
      )
  );

-- name: InsertUserFeedback :one
INSERT INTO chat_user_feedback (
    project_id,
    chat_id,
    message_id,
    user_resolution,
    user_resolution_notes,
    chat_resolution_id
) VALUES (
    @project_id,
    @chat_id,
    @message_id,
    @user_resolution,
    @user_resolution_notes,
    @chat_resolution_id
) RETURNING id;

-- name: AddUserFeedbackChatResolution :exec
UPDATE chat_user_feedback
SET chat_resolution_id = @chat_resolution_id
WHERE id = @id;

-- name: SoftDeleteChat :one
-- Soft-delete a chat unless it backs a live assistant thread, and report the
-- disposition in a single statement so the caller never has to re-query (which
-- would race with concurrent thread create/delete). `guard` snapshots whether
-- the chat exists in-project, undeleted, and is thread-backed; `del` deletes it
-- only when not thread-backed. Both CTEs share one statement snapshot, so the
-- delete and the diagnosis are consistent: a live (existing, undeleted)
-- thread-backed chat always yields backs_live_thread=true (caller returns 409)
-- and is never silently reported as a successful no-op.
WITH guard AS (
  SELECT
    c.id,
    EXISTS (
      SELECT 1
      FROM assistant_threads t
      JOIN assistants a ON a.id = t.assistant_id
      WHERE t.chat_id = c.id
        AND t.project_id = @project_id
        AND t.deleted IS FALSE
        AND a.deleted IS FALSE
    ) AS backs_live_thread
  FROM chats c
  WHERE c.id = @id
    AND c.project_id = @project_id
    AND c.deleted IS FALSE
),
del AS (
  UPDATE chats
  SET deleted_at = clock_timestamp()
  WHERE id = @id
    AND project_id = @project_id
    AND deleted IS FALSE
    AND id IN (SELECT id FROM guard WHERE backs_live_thread IS FALSE)
  RETURNING id
)
SELECT
  EXISTS (SELECT 1 FROM del) AS deleted,
  COALESCE((SELECT backs_live_thread FROM guard), FALSE)::boolean AS backs_live_thread;

-- name: GetTopUsersByMessages :many
SELECT
  COALESCE(NULLIF(c.external_user_id, ''), c.user_id) as user_id,
  CASE WHEN c.external_user_id IS NOT NULL AND c.external_user_id != '' THEN 'external' ELSE 'internal' END as user_type,
  COUNT(DISTINCT m.id) as message_count
FROM chats c
INNER JOIN chat_messages m ON m.chat_id = c.id
WHERE c.project_id = @project_id
  AND c.deleted IS FALSE
  AND m.created_at >= @time_start
  AND m.created_at <= @time_end
  AND m.role IN ('user', 'assistant')
  AND COALESCE(NULLIF(c.external_user_id, ''), c.user_id) IS NOT NULL
GROUP BY COALESCE(NULLIF(c.external_user_id, ''), c.user_id), CASE WHEN c.external_user_id IS NOT NULL AND c.external_user_id != '' THEN 'external' ELSE 'internal' END
ORDER BY message_count DESC
LIMIT @result_limit;

-- name: GetLLMClientBreakdownByMessages :many
SELECT
  COALESCE(m.source, 'unknown') as client_name,
  COUNT(DISTINCT m.id) as message_count
FROM chats c
INNER JOIN chat_messages m ON m.chat_id = c.id
WHERE c.project_id = @project_id
  AND c.deleted IS FALSE
  AND m.created_at >= @time_start
  AND m.created_at <= @time_end
  AND m.role IN ('user', 'assistant')
GROUP BY client_name
ORDER BY message_count DESC;

-- name: GetActiveUserCountByMessages :one
SELECT
  COUNT(DISTINCT COALESCE(NULLIF(c.external_user_id, ''), c.user_id))::bigint as active_user_count
FROM chats c
INNER JOIN chat_messages m ON m.chat_id = c.id
WHERE c.project_id = @project_id
  AND c.deleted IS FALSE
  AND m.created_at >= @time_start
  AND m.created_at <= @time_end
  AND m.role IN ('user', 'assistant')
  AND COALESCE(NULLIF(c.external_user_id, ''), c.user_id) IS NOT NULL;

-- name: GetChatSessionCount :one
SELECT COUNT(DISTINCT c.id)::bigint as session_count
FROM chats c
INNER JOIN chat_messages m ON m.chat_id = c.id
WHERE c.project_id = @project_id
  AND c.deleted IS FALSE
  AND m.created_at >= @time_start
  AND m.created_at <= @time_end;

-- name: GetChatMetricsSummary :one
WITH chat_stats AS (
  SELECT
    c.id as chat_id,
    MIN(m.created_at) as first_message_at,
    MAX(m.created_at) as last_message_at,
    EXTRACT(EPOCH FROM (MAX(m.created_at) - MIN(m.created_at))) * 1000 as duration_ms,
    COALESCE(
      (SELECT resolution FROM chat_resolutions WHERE chat_id = c.id ORDER BY created_at DESC LIMIT 1),
      ''
    ) as resolution_status
  FROM chats c
  INNER JOIN chat_messages m ON m.chat_id = c.id
  WHERE c.project_id = @project_id
    AND c.deleted IS FALSE
    AND m.created_at >= @time_start
    AND m.created_at <= @time_end
  GROUP BY c.id
)
SELECT
  COUNT(*)::bigint as total_chats,
  COALESCE(SUM(CASE WHEN resolution_status = 'success' THEN 1 ELSE 0 END), 0)::bigint as resolved_chats,
  COALESCE(SUM(CASE WHEN resolution_status = 'failure' THEN 1 ELSE 0 END), 0)::bigint as failed_chats,
  COALESCE(AVG(duration_ms), 0)::double precision as avg_session_duration_ms,
  COALESCE(AVG(CASE WHEN resolution_status != '' THEN duration_ms END), 0)::double precision as avg_resolution_time_ms
FROM chat_stats;

-- name: CreateChatMessageWithToolCalls :exec
-- Inserts a single chat_messages row with optional tool_calls JSON,
-- tool_call_id, and generation, for callers seeding tool-turn history without
-- the full CreateChatMessage :copyfrom batch shape.
INSERT INTO chat_messages (chat_id, project_id, role, content, tool_calls, tool_call_id, generation)
VALUES (@chat_id, @project_id, @role, @content, @tool_calls, @tool_call_id, @generation);

-- name: SeedChatAtTime :one
-- Test fixture: insert a chat with a specific created_at for date range tests.
INSERT INTO chats (id, project_id, organization_id, user_id, external_user_id, title, created_at, updated_at)
VALUES (@id, @project_id, @organization_id, @user_id, @external_user_id, @title, @created_at, @created_at)
ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.id
RETURNING id;

-- name: SeedChatMessage :one
-- Test fixture: insert a minimal chat message and return its id. An optional
-- created_at lets ordering tests assign distinct, deterministic timestamps
-- instead of relying on wall-clock gaps between inserts.
INSERT INTO chat_messages (chat_id, project_id, role, content, created_at)
VALUES (@chat_id, @project_id, 'user', 'test message', COALESCE(sqlc.narg('created_at')::timestamptz, clock_timestamp()))
RETURNING id;

-- name: SeedChatMessageWithSource :one
-- Test fixture: insert a chat message carrying a specific source. The per-chat
-- inferred source (used by the agent-type filter and ListChatSources) is the
-- latest non-null message source, so source-filter tests seed messages this way.
INSERT INTO chat_messages (chat_id, project_id, role, content, source, created_at)
VALUES (@chat_id, @project_id, 'user', 'test message', @source, COALESCE(sqlc.narg('created_at')::timestamptz, clock_timestamp()))
RETURNING id;

-- name: SeedChatMessageContent :one
-- Test fixture: insert a user chat message with explicit content and return its
-- id, for exercising the text-search windowed view.
INSERT INTO chat_messages (chat_id, project_id, role, content)
VALUES (@chat_id, @project_id, 'user', @content)
RETURNING id;

-- name: SeedRiskPolicy :one
-- Test fixture: insert a minimal risk policy and return its id.
INSERT INTO risk_policies (project_id, organization_id, name, sources, enabled, action, auto_name, version)
VALUES (@project_id, @organization_id, 'test-policy', '{}', TRUE, 'flag', TRUE, 1)
RETURNING id;

-- name: SeedDisabledRiskPolicy :one
-- Test fixture: insert a disabled risk policy and return its id. Findings under
-- a disabled (or deleted) policy must drop out of every risk surface — the
-- per-chat count, the is_risk flag, and the risk.results.list detail alike.
INSERT INTO risk_policies (project_id, organization_id, name, sources, enabled, action, auto_name, version)
VALUES (@project_id, @organization_id, 'test-policy-disabled', '{}', FALSE, 'flag', TRUE, 1)
RETURNING id;

-- name: SeedRiskResult :exec
-- Test fixture: insert a risk result linking a chat message to a risk policy.
INSERT INTO risk_results (
    project_id, organization_id, risk_policy_id, risk_policy_version,
    chat_message_id, source, found
)
VALUES (
    @project_id, @organization_id, @risk_policy_id, 1,
    @chat_message_id, 'test', @found
);

-- name: SeedAssistant :one
-- Test fixture: insert a minimal assistant and return its id.
INSERT INTO assistants (project_id, organization_id, name, model, instructions)
VALUES (@project_id, @organization_id, @name, 'anthropic/claude-opus-4.8', 'be helpful')
RETURNING id;

-- name: SeedAssistantThread :exec
-- Test fixture: insert an active assistant thread backed by a chat.
INSERT INTO assistant_threads (assistant_id, project_id, correlation_id, chat_id, source_kind)
VALUES (@assistant_id, @project_id, @correlation_id, @chat_id, 'cron');

-- name: SeedSoftDeleteAssistant :exec
-- Test fixture: soft-delete an assistant (mirrors DeleteAssistant, which leaves
-- its threads behind).
UPDATE assistants SET deleted_at = clock_timestamp() WHERE id = @id;
