-- name: ListSweepCandidates :many
SELECT id, rule_id, match, chat_message_id FROM risk_results
WHERE organization_id = sqlc.arg(organization_id) AND project_id = sqlc.arg(project_id)
  AND (sqlc.narg(policy_id)::uuid IS NULL OR risk_policy_id = sqlc.narg(policy_id))
  AND found IS TRUE AND excluded_at IS NULL AND false_positive_at IS NULL
  AND rule_id = ANY(sqlc.arg(rule_ids)::text[])
  AND id > sqlc.arg(cursor) AND id < sqlc.arg(upper_bound)
ORDER BY id LIMIT sqlc.arg(page_size);

-- name: ListMessageTexts :many
-- Include both body and tool calls: context rules suppress only in the absence
-- of a signal. Bound each payload well above the scanner's accepted size.
SELECT id, (left(content, 262144) || ' ' || left(coalesce(tool_calls::text, ''), 262144))::text AS message_text
FROM chat_messages
WHERE project_id = sqlc.arg(project_id) AND id = ANY(sqlc.arg(message_ids)::uuid[]);

-- name: MarkSweepBatch :execrows
UPDATE risk_results r SET false_positive_at = now(), false_positive_reason = t.reason
FROM (SELECT unnest(sqlc.arg(ids)::uuid[]) AS id, unnest(sqlc.arg(reasons)::text[]) AS reason) t
WHERE r.id = t.id AND r.project_id = sqlc.arg(project_id) AND r.organization_id = sqlc.arg(organization_id)
  AND r.false_positive_at IS NULL;
