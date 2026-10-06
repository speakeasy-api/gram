-- name: ListSweepCandidates :many
SELECT id, source, rule_id, match FROM risk_results
WHERE organization_id = sqlc.arg(organization_id) AND project_id = sqlc.arg(project_id)
  AND (sqlc.narg(policy_id)::uuid IS NULL OR risk_policy_id = sqlc.narg(policy_id))
  AND found IS TRUE AND excluded_at IS NULL AND false_positive_at IS NULL
  AND (rule_id = ANY(sqlc.arg(rule_ids)::text[]) OR rule_id LIKE ANY(sqlc.arg(rule_globs)::text[]))
  AND id > sqlc.arg(cursor) AND id < sqlc.arg(upper_bound)
ORDER BY id LIMIT sqlc.arg(page_size);

-- name: MarkSweepBatch :execrows
UPDATE risk_results r SET false_positive_at = now(), false_positive_reason = t.reason
FROM (SELECT unnest(sqlc.arg(ids)::uuid[]) AS id, unnest(sqlc.arg(reasons)::text[]) AS reason) t
WHERE r.id = t.id AND r.project_id = sqlc.arg(project_id) AND r.organization_id = sqlc.arg(organization_id)
  AND r.false_positive_at IS NULL;
