-- name: PlantVisitorEMABinding :execrows
INSERT INTO remote_session_ema_bindings (
 organization_id, project_id, user_session_issuer_id,
 remote_session_issuer_id, remote_session_client_id, resource,
 state, claim_id, claimed_at
)
SELECT c.organization_id, c.project_id, u.id,
 c.remote_session_issuer_id,
 CASE WHEN sqlc.arg(state)::text = 'ready' THEN c.id END,
 'https://visitor.example.com/' || sqlc.arg(state)::text,
 sqlc.arg(state)::text,
 CASE WHEN sqlc.arg(state)::text = 'in_progress' THEN generate_uuidv7() END,
 CASE WHEN sqlc.arg(state)::text = 'in_progress' THEN clock_timestamp() END
-- The attachment is the issuer provenance; ordering only selects a valid pair.
FROM remote_session_clients c
JOIN remote_session_client_user_session_issuers link ON link.remote_session_client_id = c.id
JOIN user_session_issuers u
 ON u.id = link.user_session_issuer_id AND u.project_id = c.project_id AND u.organization_id = c.organization_id
WHERE c.organization_id = sqlc.arg(organization_id) AND c.project_id = sqlc.arg(project_id)::text::uuid
ORDER BY c.id, u.id
LIMIT 1;

-- name: CountVisitorEMABindings :one
SELECT count(*) FROM remote_session_ema_bindings
WHERE organization_id = sqlc.arg(organization_id)
  AND project_id = sqlc.arg(project_id)::text::uuid;

-- name: GetVisitorEMAFixtureParents :one
SELECT c.id AS client_id, c.remote_session_issuer_id, u.id AS user_session_issuer_id
FROM remote_session_clients c
JOIN remote_session_client_user_session_issuers link ON link.remote_session_client_id = c.id
JOIN user_session_issuers u ON u.id = link.user_session_issuer_id
  AND u.organization_id = c.organization_id AND u.project_id = c.project_id
WHERE c.organization_id = sqlc.arg(organization_id) AND c.project_id = sqlc.arg(project_id)::text::uuid
ORDER BY c.id, u.id LIMIT 1;

-- name: CountVisitorEMABindingsWithoutAttachment :one
SELECT count(*) FROM remote_session_ema_bindings b
WHERE b.organization_id = sqlc.arg(organization_id) AND b.project_id = sqlc.arg(project_id)::text::uuid
AND NOT EXISTS (
 SELECT 1 FROM remote_session_client_user_session_issuers link
 JOIN remote_session_clients c ON c.id = link.remote_session_client_id
 WHERE link.user_session_issuer_id = b.user_session_issuer_id
 AND c.remote_session_issuer_id = b.remote_session_issuer_id
 AND c.organization_id = b.organization_id AND c.project_id = b.project_id
);
