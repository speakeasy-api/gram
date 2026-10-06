-- name: ListSourcePage :many
-- The UUID cursor is only a resume key; created_at independently bounds time.
-- Preserve false-positive marks and migrate only findings emitted live.
-- Attribution joins enforce the same project/chat ownership as the live writer;
-- missing anchors retain empty attribution and the finding's timestamp.
SELECT r.id, r.created_at, r.organization_id, r.project_id, r.risk_policy_id,
       r.risk_policy_version, r.chat_message_id, r.chat_content_part_id, r.source, r.found,
       r.rule_id, r.description, r.match, r.start_pos, r.end_pos, r.confidence, r.tags,
       r.spans, r.dead_letter_reason, r.excluded_at, r.excluded_exclusion_id, r.false_positive_at,
       COALESCE(cm.chat_id::text, ccp.chat_id::text, '')::text AS chat_id,
       COALESCE(NULLIF(cm.user_id, ''), NULLIF(pcm.user_id, ''), NULLIF(c.user_id, ''), '')::text AS user_id,
       COALESCE(NULLIF(cm.external_user_id, ''), NULLIF(pcm.external_user_id, ''), NULLIF(c.external_user_id, ''), '')::text AS external_user_id,
       COALESCE(cm.created_at, pcm.created_at, r.created_at)::timestamptz AS message_created_at,
       COALESCE(thread.assistant_id::text, '')::text AS assistant_id
FROM risk_results r
LEFT JOIN chat_messages cm ON cm.id = r.chat_message_id
  AND EXISTS (SELECT 1 FROM chats mc WHERE mc.id = cm.chat_id AND mc.project_id = r.project_id)
LEFT JOIN chat_content_parts ccp ON ccp.id = r.chat_content_part_id
  AND ccp.deleted IS FALSE AND ccp.project_id = r.project_id
  AND EXISTS (SELECT 1 FROM chats pc WHERE pc.id = ccp.chat_id AND pc.project_id = ccp.project_id)
LEFT JOIN chat_messages pcm ON pcm.id = ccp.parent_chat_message_id AND pcm.chat_id = ccp.chat_id
LEFT JOIN chats c ON c.id = COALESCE(cm.chat_id, ccp.chat_id) AND c.deleted IS FALSE
LEFT JOIN LATERAL (
  SELECT at.assistant_id FROM assistant_threads at
  WHERE at.chat_id = COALESCE(cm.chat_id, ccp.chat_id) AND at.deleted IS FALSE
  ORDER BY at.created_at DESC LIMIT 1
) thread ON TRUE
WHERE (sqlc.narg(organization_id)::text IS NULL OR r.organization_id = sqlc.narg(organization_id))
  AND (sqlc.narg(project_id)::uuid IS NULL OR r.project_id = sqlc.narg(project_id))
  AND (sqlc.narg(policy_id)::uuid IS NULL OR r.risk_policy_id = sqlc.narg(policy_id))
  AND (sqlc.narg(from_time)::timestamptz IS NULL OR r.created_at >= sqlc.narg(from_time))
  AND (sqlc.narg(to_time)::timestamptz IS NULL OR r.created_at < sqlc.narg(to_time))
  AND r.id > sqlc.arg(cursor) AND r.found IS TRUE AND r.rule_id IS NOT NULL
ORDER BY r.id LIMIT sqlc.arg(page_size);
