-- name: ListSourcePage :many
-- The cursor is only a resume key; exact created_at bounds still apply.
-- Only findings emitted to ClickHouse are enriched. Missing messages fall
-- back to the finding timestamp, matching the ClickHouse column default.
SELECT r.id, r.created_at,
       COALESCE(cm.created_at, r.created_at)::timestamptz AS message_created_at,
       COALESCE(at.assistant_id::text, '')::text AS assistant_id
FROM risk_results r
LEFT JOIN chat_messages cm ON cm.id = r.chat_message_id
LEFT JOIN LATERAL (
    SELECT t.assistant_id FROM assistant_threads t
    WHERE t.chat_id = cm.chat_id AND t.deleted IS FALSE
    ORDER BY t.id LIMIT 1
) at ON TRUE
WHERE (sqlc.narg(organization_id)::text IS NULL OR r.organization_id = sqlc.narg(organization_id))
  AND (sqlc.narg(project_id)::uuid IS NULL OR r.project_id = sqlc.narg(project_id))
  AND (sqlc.narg(from_time)::timestamptz IS NULL OR r.created_at >= sqlc.narg(from_time))
  AND (sqlc.narg(to_time)::timestamptz IS NULL OR r.created_at < sqlc.narg(to_time))
  AND r.id > sqlc.arg(cursor) AND r.found IS TRUE AND r.rule_id IS NOT NULL
ORDER BY r.id LIMIT sqlc.arg(page_size);
