-- name: CreateRiskPolicy :one
INSERT INTO risk_policies (
    id
  , project_id
  , organization_id
  , name
  , sources
  , presidio_entities
  , enabled
  , action
  , auto_name
  , user_message
  , version
)
VALUES (
    @id
  , @project_id
  , @organization_id
  , @name
  , @sources
  , @presidio_entities
  , @enabled
  , @action
  , @auto_name
  , @user_message
  , 1
)
RETURNING *;

-- name: GetRiskPolicy :one
SELECT *
FROM risk_policies
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: ListRiskPolicies :many
SELECT *
FROM risk_policies
WHERE project_id = @project_id
  AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: ListEnabledRiskPoliciesByProject :many
SELECT *
FROM risk_policies
WHERE project_id = @project_id
  AND enabled IS TRUE
  AND deleted IS FALSE;

-- name: UpdateRiskPolicy :one
UPDATE risk_policies
SET name = @name
  , sources = @sources
  , presidio_entities = @presidio_entities
  , enabled = @enabled
  , action = @action
  , auto_name = @auto_name
  , user_message = @user_message
  , version = CASE
      WHEN sources IS DISTINCT FROM @sources
        OR presidio_entities IS DISTINCT FROM @presidio_entities
        OR enabled IS DISTINCT FROM @enabled
        OR action IS DISTINCT FROM @action
      THEN version + 1
      ELSE version
    END
  , updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: BumpRiskPolicyVersion :one
UPDATE risk_policies
SET version = version + 1
  , updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: DeleteRiskPolicy :exec
UPDATE risk_policies
SET deleted_at = clock_timestamp()
  , updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: CountTotalMessages :one
SELECT COUNT(*)::BIGINT
FROM chat_messages cm
WHERE cm.project_id = @project_id;

-- name: CountAnalyzedMessages :one
SELECT COUNT(DISTINCT rr.chat_message_id)::BIGINT
FROM risk_results rr
WHERE rr.project_id = @project_id
  AND rr.risk_policy_id = @risk_policy_id
  AND rr.risk_policy_version = @risk_policy_version;

-- name: CountFindingsByPolicy :one
SELECT COUNT(*)::BIGINT
FROM risk_results
WHERE project_id = @project_id
  AND risk_policy_id = @risk_policy_id
  AND risk_policy_version = @risk_policy_version
  AND found IS TRUE;

-- name: CountAllFindings :one
SELECT COUNT(*)::BIGINT
FROM risk_results rr
JOIN risk_policies rp ON rp.id = rr.risk_policy_id AND rp.deleted IS FALSE AND rp.enabled IS TRUE
WHERE rr.project_id = @project_id
  AND rr.found IS TRUE;

-- name: FetchUnanalyzedMessageIDs :many
SELECT cm.id
FROM chat_messages cm
WHERE cm.project_id = @project_id
  AND NOT EXISTS (
    SELECT 1
    FROM risk_results rr
    WHERE rr.chat_message_id = cm.id
      AND rr.project_id = @project_id
      AND rr.risk_policy_id = @risk_policy_id
      AND rr.risk_policy_version = @risk_policy_version
    LIMIT 1
  )
LIMIT @batch_limit;

-- name: GetMessageContentBatch :many
SELECT id, content, tool_calls
FROM chat_messages
WHERE id = ANY(@ids::uuid[])
  AND project_id = @project_id;

-- name: InsertRiskResults :copyfrom
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
  , start_pos
  , end_pos
  , confidence
  , tags
)
VALUES (
    @id
  , @project_id
  , @organization_id
  , @risk_policy_id
  , @risk_policy_version
  , @chat_message_id
  , @source
  , @found
  , @rule_id
  , @description
  , @match
  , @start_pos
  , @end_pos
  , @confidence
  , @tags
);

-- name: DeleteRiskResultsForMessages :exec
DELETE FROM risk_results
WHERE risk_policy_id = @risk_policy_id
  AND project_id = @project_id
  AND chat_message_id = ANY(@message_ids::uuid[]);

-- name: ListRiskResultsByProjectFound :many
SELECT rr.*, cm.chat_id, c.title AS chat_title, c.external_user_id AS chat_user_id
FROM risk_results rr
JOIN chat_messages cm ON cm.id = rr.chat_message_id
LEFT JOIN chats c ON c.id = cm.chat_id AND c.deleted IS FALSE
JOIN risk_policies rp ON rp.id = rr.risk_policy_id AND rp.deleted IS FALSE AND rp.enabled IS TRUE
WHERE rr.project_id = @project_id
  AND rr.found IS TRUE
  AND (sqlc.narg(cursor)::uuid IS NULL OR rr.id <= sqlc.narg(cursor)::uuid)
ORDER BY rr.id DESC
LIMIT @page_limit;

-- name: ListRiskResultsByProjectAndPolicy :many
SELECT rr.*, cm.chat_id, c.title AS chat_title, c.external_user_id AS chat_user_id
FROM risk_results rr
JOIN chat_messages cm ON cm.id = rr.chat_message_id
LEFT JOIN chats c ON c.id = cm.chat_id AND c.deleted IS FALSE
JOIN risk_policies rp ON rp.id = rr.risk_policy_id AND rp.deleted IS FALSE AND rp.enabled IS TRUE
WHERE rr.project_id = @project_id
  AND rr.risk_policy_id = @risk_policy_id
  AND rr.found IS TRUE
  AND (sqlc.narg(cursor)::uuid IS NULL OR rr.id <= sqlc.narg(cursor)::uuid)
ORDER BY rr.id DESC
LIMIT @page_limit;

-- name: ListRiskResultsByChatFound :many
SELECT rr.*, cm.chat_id, c.title AS chat_title, c.external_user_id AS chat_user_id
FROM risk_results rr
JOIN chat_messages cm ON cm.id = rr.chat_message_id
LEFT JOIN chats c ON c.id = cm.chat_id AND c.deleted IS FALSE
JOIN risk_policies rp ON rp.id = rr.risk_policy_id AND rp.deleted IS FALSE AND rp.enabled IS TRUE
WHERE cm.chat_id = @chat_id
  AND rr.project_id = @project_id
  AND rr.found IS TRUE
  AND (sqlc.narg(cursor)::uuid IS NULL OR rr.id <= sqlc.narg(cursor)::uuid)
ORDER BY rr.id DESC
LIMIT @page_limit;

-- name: ListRiskResultsGroupedByChat :many
SELECT
    cm.chat_id
  , c.title AS chat_title
  , c.external_user_id AS chat_user_id
  , COUNT(*)::BIGINT AS findings_count
  , MAX(rr.created_at)::TIMESTAMPTZ AS latest_detected
FROM risk_results rr
JOIN chat_messages cm ON cm.id = rr.chat_message_id
LEFT JOIN chats c ON c.id = cm.chat_id AND c.deleted IS FALSE
JOIN risk_policies rp ON rp.id = rr.risk_policy_id AND rp.deleted IS FALSE AND rp.enabled IS TRUE
WHERE rr.project_id = @project_id
  AND rr.found IS TRUE
  AND (sqlc.narg(cursor)::uuid IS NULL OR cm.chat_id <= sqlc.narg(cursor)::uuid)
GROUP BY cm.chat_id, c.title, c.external_user_id
ORDER BY cm.chat_id DESC
LIMIT @page_limit;

-- name: ListEnabledEnforcingPoliciesByProject :many
SELECT *
FROM risk_policies
WHERE project_id = @project_id
  AND enabled IS TRUE
  AND action = 'block'
  AND deleted IS FALSE;

-- name: ListEnabledShadowMCPPoliciesByProject :many
SELECT *
FROM risk_policies
WHERE project_id = @project_id
  AND enabled IS TRUE
  AND deleted IS FALSE
  AND 'shadow_mcp' = ANY(sources)
ORDER BY id;

-- name: ListEnabledToolIdentityPoliciesByProject :many
SELECT *
FROM risk_policies
WHERE project_id = @project_id
  AND enabled IS TRUE
  AND deleted IS FALSE
  AND (
    'shadow_mcp' = ANY(sources)
    OR 'destructive_tool' = ANY(sources)
  )
ORDER BY id;
