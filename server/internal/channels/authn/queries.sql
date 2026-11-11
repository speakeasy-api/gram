-- name: RegisterClient :exec
INSERT INTO channels_oauth_clients (client_id, client_name, redirect_uris, secret_hash, token_endpoint_auth_method) VALUES ($1,$2,$3,$4,$5);
-- name: GetClient :one
SELECT * FROM channels_oauth_clients WHERE client_id=$1 AND revoked_at IS NULL;
-- name: CreateChallenge :exec
INSERT INTO channels_oauth_challenges (id,organization_id,project_id,principal,resource,browser_hash,csrf_hash,client_id,redirect_uri,state,code_challenge,expires_at) VALUES (@id,@organization_id,@project_id,@principal,@resource,@browser_hash,@csrf_hash,@client_id,@redirect_uri,@state,@code_challenge,@expires_at);
-- name: ConsumeChallenge :one
DELETE FROM channels_oauth_challenges WHERE id= @id AND organization_id= @organization_id AND project_id= @project_id AND browser_hash= @browser_hash AND csrf_hash= @csrf_hash AND expires_at>clock_timestamp() RETURNING *;
-- name: GetChallenge :one
SELECT * FROM channels_oauth_challenges WHERE id= @id AND organization_id= @organization_id AND project_id= @project_id AND browser_hash= @browser_hash AND expires_at>clock_timestamp();
-- name: CreateGrant :one
INSERT INTO channels_oauth_grants (id,client_id,organization_id,project_id,principal,resource,expires_at) VALUES (@id,@client_id,@organization_id,@project_id,@principal,@resource,@expires_at) RETURNING id;
-- name: CreateCredential :exec
INSERT INTO channels_oauth_credentials (token_hash,kind,organization_id,project_id,grant_id,redirect_uri,code_challenge,expires_at) SELECT @token_hash,@kind,g.organization_id,g.project_id,g.id,@redirect_uri,@code_challenge,@expires_at FROM channels_oauth_grants g WHERE g.id= @grant_id AND g.organization_id= @organization_id AND g.project_id= @project_id;
-- name: LockCredential :one
SELECT c.token_hash,c.kind,c.grant_id,c.redirect_uri,c.code_challenge,c.used_at,c.expires_at,g.client_id,g.organization_id,g.project_id,g.resource,g.principal,g.revoked_at,g.expires_at AS grant_expires_at FROM channels_oauth_credentials c JOIN channels_oauth_grants g ON g.id=c.grant_id AND g.organization_id=c.organization_id AND g.project_id=c.project_id WHERE c.token_hash= @token_hash AND c.organization_id= @organization_id AND c.project_id= @project_id FOR UPDATE OF c,g;
-- name: UseCredential :exec
UPDATE channels_oauth_credentials SET used_at=clock_timestamp(),updated_at=clock_timestamp() WHERE token_hash= @token_hash AND organization_id= @organization_id AND project_id= @project_id;
-- name: CreateAccessToken :exec
INSERT INTO channels_oauth_access_tokens (jti,organization_id,project_id,grant_id,expires_at) SELECT @jti,g.organization_id,g.project_id,g.id,@expires_at FROM channels_oauth_grants g WHERE g.id= @grant_id AND g.organization_id= @organization_id AND g.project_id= @project_id;
-- name: GetAccessIdentity :one
SELECT g.organization_id,g.project_id,g.resource,g.principal,g.client_id FROM channels_oauth_access_tokens a JOIN channels_oauth_grants g ON g.id=a.grant_id AND g.organization_id=a.organization_id AND g.project_id=a.project_id JOIN channels_oauth_clients c ON c.client_id=g.client_id WHERE a.jti= @jti AND a.organization_id= @organization_id AND a.project_id= @project_id AND a.expires_at>clock_timestamp() AND g.revoked_at IS NULL AND g.expires_at>clock_timestamp() AND c.revoked_at IS NULL;
-- name: RevokeGrant :exec
UPDATE channels_oauth_grants SET revoked_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id= @id AND organization_id= @organization_id AND project_id= @project_id;
-- name: RevokeCredential :exec
UPDATE channels_oauth_grants g SET revoked_at=clock_timestamp(),updated_at=clock_timestamp() WHERE g.client_id= @client_id AND g.organization_id= @organization_id AND g.project_id= @project_id AND g.id IN (SELECT c.grant_id FROM channels_oauth_credentials c WHERE c.organization_id=g.organization_id AND c.project_id=g.project_id AND c.token_hash= @token_hash AND c.kind='refresh');
-- name: RevokeAccess :exec
UPDATE channels_oauth_grants g SET revoked_at=clock_timestamp(),updated_at=clock_timestamp() WHERE g.client_id= @client_id AND g.organization_id= @organization_id AND g.project_id= @project_id AND g.id IN (SELECT a.grant_id FROM channels_oauth_access_tokens a WHERE a.organization_id=g.organization_id AND a.project_id=g.project_id AND a.jti= @jti);

-- name: UpsertMetadataClient :exec
INSERT INTO channels_oauth_clients (client_id,client_name,redirect_uris,token_endpoint_auth_method) VALUES ($1,$2,$3,'none') ON CONFLICT (client_id) DO UPDATE SET client_name=EXCLUDED.client_name,redirect_uris=EXCLUDED.redirect_uris,updated_at=clock_timestamp() WHERE channels_oauth_clients.revoked_at IS NULL;

-- name: LockIdentity :exec
SELECT pg_advisory_xact_lock(hashtextextended(@identity_key::text,0));
-- name: RevokePriorGrants :exec
UPDATE channels_oauth_grants SET revoked_at=clock_timestamp(),updated_at=clock_timestamp() WHERE organization_id= @organization_id AND project_id= @project_id AND principal= @principal AND client_id= @client_id AND revoked_at IS NULL;
-- name: CleanupChallenges :exec
DELETE FROM channels_oauth_challenges WHERE (organization_id,project_id,id) IN (SELECT organization_id,project_id,id FROM channels_oauth_challenges WHERE expires_at<clock_timestamp() ORDER BY expires_at LIMIT 100);
-- name: CleanupAccessTokens :exec
DELETE FROM channels_oauth_access_tokens WHERE (organization_id,project_id,jti) IN (SELECT organization_id,project_id,jti FROM channels_oauth_access_tokens WHERE expires_at<clock_timestamp() ORDER BY expires_at LIMIT 100);
-- name: CleanupGrants :exec
DELETE FROM channels_oauth_grants WHERE (organization_id,project_id,id) IN (SELECT g.organization_id,g.project_id,g.id FROM channels_oauth_grants g WHERE g.expires_at<clock_timestamp() AND NOT EXISTS (SELECT 1 FROM channels_oauth_credentials c WHERE c.grant_id=g.id AND c.organization_id=g.organization_id AND c.project_id=g.project_id) AND NOT EXISTS (SELECT 1 FROM channels_oauth_access_tokens a WHERE a.grant_id=g.id AND a.organization_id=g.organization_id AND a.project_id=g.project_id) ORDER BY g.expires_at LIMIT 100);
-- name: CleanupClients :exec
DELETE FROM channels_oauth_clients c WHERE c.client_id IN (SELECT old.client_id FROM channels_oauth_clients old WHERE old.updated_at<clock_timestamp()-interval '30 days' AND old.revoked_at IS NULL AND NOT EXISTS (SELECT 1 FROM channels_oauth_grants g WHERE g.client_id=old.client_id) AND NOT EXISTS (SELECT 1 FROM channels_oauth_challenges h WHERE h.client_id=old.client_id) ORDER BY old.updated_at LIMIT 100);

-- name: CleanupCredentials :exec
DELETE FROM channels_oauth_credentials WHERE (organization_id,project_id,token_hash) IN (SELECT organization_id,project_id,token_hash FROM channels_oauth_credentials WHERE expires_at<clock_timestamp() ORDER BY expires_at LIMIT 100);
