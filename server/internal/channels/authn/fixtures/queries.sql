-- name: SeedOrganizations :exec
INSERT INTO organization_metadata (id,name,slug) VALUES ('org_test','Test organization','test-org'), ('org_other','Other organization','other-org');

-- name: SeedDefaultProjects :exec
INSERT INTO projects (name,slug,organization_id) VALUES ('Default','default','org_test'), ('Default','default','org_other'), ('Second','second','org_test');

-- name: ExpireCodes :exec
UPDATE channels_oauth_credentials SET expires_at=clock_timestamp()-interval '1 hour' WHERE kind='code';

-- name: ExpireChallenges :exec
UPDATE channels_oauth_challenges SET expires_at=clock_timestamp()-interval '1 hour';

-- name: ExpireAccessTokens :exec
UPDATE channels_oauth_access_tokens SET expires_at=clock_timestamp()-interval '1 hour';

-- name: AgeClients :exec
UPDATE channels_oauth_clients SET updated_at=clock_timestamp()-interval '31 days';

-- name: ExpireGrants :exec
UPDATE channels_oauth_grants SET expires_at=clock_timestamp()-interval '1 hour';

-- name: ExpireCredentials :exec
UPDATE channels_oauth_credentials SET expires_at=clock_timestamp()-interval '1 hour';

-- name: SeedExpiredRefreshBatch :exec
INSERT INTO channels_oauth_credentials (token_hash,kind,organization_id,project_id,grant_id,expires_at) SELECT 'expired-refresh-'||n,'refresh',g.organization_id,g.project_id,g.id,clock_timestamp()-interval '1 hour' FROM channels_oauth_grants g CROSS JOIN generate_series(1,105) n;

-- name: SeedExpiredAccessBatch :exec
INSERT INTO channels_oauth_access_tokens (jti,organization_id,project_id,grant_id,expires_at) SELECT 'expired-access-'||n,g.organization_id,g.project_id,g.id,clock_timestamp()-interval '1 hour' FROM channels_oauth_grants g CROSS JOIN generate_series(1,105) n;

-- name: SeedExpiredChallengeBatch :exec
INSERT INTO channels_oauth_challenges (id,organization_id,project_id,principal,resource,browser_hash,csrf_hash,client_id,redirect_uri,state,code_challenge,expires_at) SELECT 'expired-'||n,p.organization_id,p.id,'urn:principal:user:user_test','https://channels.example.com/channels/test-org/default/mcp','proof','csrf',$1,$2,'state','pkce',clock_timestamp()-interval '1 hour' FROM generate_series(1,105) n CROSS JOIN projects p WHERE p.organization_id='org_test' AND p.slug='default';

-- name: RevokeClient :exec
UPDATE channels_oauth_clients SET revoked_at=clock_timestamp() WHERE client_id=$1;

-- name: SeedMetadataClient :exec
INSERT INTO channels_oauth_clients (client_id,client_name,redirect_uris,token_endpoint_auth_method) VALUES ('https://client.example.com/preserved.json','Metadata client',$1::text[],'none');

-- name: DeleteClientGrants :exec
WITH credentials AS (DELETE FROM channels_oauth_credentials c USING channels_oauth_grants g WHERE g.organization_id=c.organization_id AND g.project_id=c.project_id AND g.id=c.grant_id AND g.client_id=$1), access AS (DELETE FROM channels_oauth_access_tokens a USING channels_oauth_grants g WHERE g.organization_id=a.organization_id AND g.project_id=a.project_id AND g.id=a.grant_id AND g.client_id=$1)
DELETE FROM channels_oauth_grants g WHERE g.client_id=$1;

-- name: AgeRevokedClient :exec
UPDATE channels_oauth_clients SET revoked_at=clock_timestamp(),updated_at=clock_timestamp()-interval '31 days' WHERE client_id=$1;

-- name: CountRows :one
SELECT CASE $1::text WHEN 'channels_oauth_challenges' THEN (SELECT count(*) FROM channels_oauth_challenges) WHEN 'channels_oauth_access_tokens' THEN (SELECT count(*) FROM channels_oauth_access_tokens) WHEN 'channels_oauth_clients' THEN (SELECT count(*) FROM channels_oauth_clients) WHEN 'channels_oauth_grants' THEN (SELECT count(*) FROM channels_oauth_grants) WHEN 'channels_oauth_credentials' THEN (SELECT count(*) FROM channels_oauth_credentials) ELSE -1 END::integer;

-- name: CountCodes :one
SELECT count(*) FROM channels_oauth_credentials WHERE kind='code';

-- name: CredentialUsed :one
SELECT (used_at IS NOT NULL)::boolean FROM channels_oauth_credentials WHERE token_hash=$1;

-- name: CountChallenges :one
SELECT count(*) FROM channels_oauth_challenges;

-- name: ClientExists :one
SELECT EXISTS(SELECT 1 FROM channels_oauth_clients WHERE client_id=$1);

-- name: ClientAuthMethod :one
SELECT token_endpoint_auth_method FROM channels_oauth_clients WHERE client_id=$1;

-- name: ClientRevoked :one
SELECT (revoked_at IS NOT NULL)::boolean FROM channels_oauth_clients WHERE client_id=$1;

-- name: CountClient :one
SELECT count(*) FROM channels_oauth_clients WHERE client_id=$1;

-- name: SoftDeleteProject :exec
UPDATE projects SET deleted_at=clock_timestamp() WHERE organization_id= @organization_id AND id= @project_id;

-- name: RecreateDefaultProject :one
INSERT INTO projects (name,slug,organization_id) VALUES ('Recreated','default',@organization_id) RETURNING id;
