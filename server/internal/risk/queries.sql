-- name: CreateRiskPolicy :one
INSERT INTO risk_policies (
    id
  , project_id
  , organization_id
  , name
  , sources
  , enabled
  , version
)
VALUES (
    @id
  , @project_id
  , @organization_id
  , @name
  , @sources
  , @enabled
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
  , enabled = @enabled
  , version = CASE
      WHEN sources IS DISTINCT FROM @sources OR enabled IS DISTINCT FROM @enabled
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

-- name: CountUnanalyzedMessages :one
SELECT COUNT(*)::BIGINT
FROM chat_messages cm
WHERE cm.project_id = @project_id
  AND NOT EXISTS (
    SELECT 1
    FROM risk_results rr
    WHERE rr.chat_message_id = cm.id
      AND rr.risk_policy_id = @risk_policy_id
      AND rr.policy_version = @policy_version
  );

-- name: CountTotalMessages :one
SELECT COUNT(*)::BIGINT
FROM chat_messages cm
WHERE cm.project_id = @project_id;

-- name: CountAnalyzedMessages :one
SELECT COUNT(DISTINCT rr.chat_message_id)::BIGINT
FROM risk_results rr
WHERE rr.project_id = @project_id
  AND rr.risk_policy_id = @risk_policy_id
  AND rr.policy_version = @policy_version;

-- name: CountFindingsByPolicy :one
SELECT COUNT(*)::BIGINT
FROM risk_results
WHERE project_id = @project_id
  AND risk_policy_id = @risk_policy_id
  AND policy_version = @policy_version
  AND found IS TRUE;

-- name: FetchUnanalyzedMessageIDs :many
SELECT cm.id
FROM chat_messages cm
WHERE cm.project_id = @project_id
  AND NOT EXISTS (
    SELECT 1
    FROM risk_results rr
    WHERE rr.chat_message_id = cm.id
      AND rr.risk_policy_id = @risk_policy_id
      AND rr.policy_version = @policy_version
  )
ORDER BY cm.seq ASC
LIMIT @batch_limit;

-- name: GetMessageContentBatch :many
SELECT id, content
FROM chat_messages
WHERE id = ANY(@ids::uuid[])
  AND project_id = @project_id;

-- name: InsertRiskResults :copyfrom
INSERT INTO risk_results (
    id
  , project_id
  , risk_policy_id
  , policy_version
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
  , @risk_policy_id
  , @policy_version
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

-- name: DeleteStaleRiskResults :exec
DELETE FROM risk_results
WHERE risk_policy_id = @risk_policy_id
  AND project_id = @project_id
  AND policy_version < @policy_version;

-- name: DeleteAllRiskResultsForPolicy :exec
DELETE FROM risk_results
WHERE risk_policy_id = @risk_policy_id;

-- name: DeleteRiskResultsForMessages :exec
DELETE FROM risk_results
WHERE risk_policy_id = @risk_policy_id
  AND chat_message_id = ANY(@message_ids::uuid[]);

-- name: ListRiskResultsByProject :many
SELECT *
FROM risk_results
WHERE project_id = @project_id
ORDER BY created_at DESC
LIMIT @result_limit;

-- name: ListRiskResultsByProjectFound :many
SELECT rr.*, cm.chat_id, c.title AS chat_title
FROM risk_results rr
JOIN chat_messages cm ON cm.id = rr.chat_message_id
LEFT JOIN chats c ON c.id = cm.chat_id
WHERE rr.project_id = @project_id
  AND rr.found IS TRUE
ORDER BY rr.created_at DESC
LIMIT @result_limit;

-- name: ListRiskResultsByProjectAndPolicy :many
SELECT rr.*, cm.chat_id, c.title AS chat_title
FROM risk_results rr
JOIN chat_messages cm ON cm.id = rr.chat_message_id
LEFT JOIN chats c ON c.id = cm.chat_id
WHERE rr.project_id = @project_id
  AND rr.risk_policy_id = @risk_policy_id
  AND rr.found IS TRUE
ORDER BY rr.created_at DESC
LIMIT @result_limit;

-- name: ListRiskResultsByChatFound :many
SELECT rr.*, cm.chat_id, c.title AS chat_title
FROM risk_results rr
JOIN chat_messages cm ON cm.id = rr.chat_message_id
LEFT JOIN chats c ON c.id = cm.chat_id
WHERE cm.chat_id = @chat_id
  AND rr.project_id = @project_id
  AND rr.found IS TRUE
ORDER BY rr.created_at DESC
LIMIT @result_limit;

-- name: ListRiskResultsByMessage :many
SELECT *
FROM risk_results
WHERE chat_message_id = @chat_message_id
  AND project_id = @project_id
ORDER BY created_at DESC;
