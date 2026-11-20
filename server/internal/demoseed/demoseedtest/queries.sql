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
-- A ready seed binding supplies the scoped parents for visitor fixtures.
FROM remote_session_clients c
JOIN remote_session_ema_bindings seed
 ON seed.remote_session_client_id = c.id AND seed.remote_session_issuer_id = c.remote_session_issuer_id
 AND seed.project_id = c.project_id AND seed.organization_id = c.organization_id
 AND seed.state = 'ready' AND seed.resource NOT LIKE 'https://visitor.example.com/%'
JOIN user_session_issuers u ON u.id = seed.user_session_issuer_id AND NOT u.deleted
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
JOIN remote_session_ema_bindings seed
 ON seed.remote_session_client_id = c.id AND seed.remote_session_issuer_id = c.remote_session_issuer_id
 AND seed.project_id = c.project_id AND seed.organization_id = c.organization_id
 AND seed.state = 'ready' AND seed.resource NOT LIKE 'https://visitor.example.com/%'
JOIN user_session_issuers u ON u.id = seed.user_session_issuer_id AND NOT u.deleted
WHERE c.organization_id = sqlc.arg(organization_id) AND c.project_id = sqlc.arg(project_id)::text::uuid
ORDER BY c.id, u.id LIMIT 1;

-- name: HasSeedPersonalOffer :one
SELECT EXISTS (
 SELECT 1
 FROM mcp_server_auth_clients offer
 JOIN mcp_auth_clients client
   ON client.id = offer.auth_client_id
  AND client.organization_id = offer.organization_id
 JOIN remote_session_clients registration ON registration.id = client.remote_session_client_id
 WHERE offer.organization_id = sqlc.arg(org_id)::text
   AND offer.mcp_server_id = sqlc.arg(server_id)::text::uuid
   AND offer.deleted IS FALSE
   AND client.deleted IS FALSE
   AND ((client.client_type = 'remote_session' AND registration.credential_owner = 'subject')
        OR (client.client_type = 'ema' AND EXISTS (
            SELECT 1 FROM mcp_auth_client_emas details
            WHERE details.auth_client_id = client.id
              AND details.organization_id = client.organization_id
              AND details.deleted IS FALSE
        )))
   AND registration.id = sqlc.arg(registration_id)::text::uuid
   AND registration.deleted IS FALSE
);
