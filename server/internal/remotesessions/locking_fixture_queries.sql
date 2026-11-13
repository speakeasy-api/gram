-- Test fixtures and NOWAIT probes for auth writer lock ordering.

-- name: LockingFixtureLockServer :exec
SELECT id FROM mcp_servers WHERE project_id=sqlc.arg(project_id)::uuid AND id=sqlc.arg(server_id)::uuid FOR UPDATE NOWAIT;

-- name: LockingFixtureCountIssuerBindings :one
SELECT count(*) FROM remote_session_client_user_session_issuers AS binding
                 JOIN remote_session_clients AS client ON client.id=binding.remote_session_client_id
                 WHERE client.project_id=sqlc.arg(project_id)::uuid AND binding.user_session_issuer_id=sqlc.arg(user_issuer_id)::uuid;

-- name: LockingFixtureLockUserIssuer :exec
SELECT id FROM user_session_issuers WHERE id=sqlc.arg(user_issuer_id)::uuid AND project_id=sqlc.arg(project_id)::uuid FOR UPDATE NOWAIT;

-- name: LockingFixtureLockRegistration :exec
SELECT id FROM remote_session_clients WHERE project_id=sqlc.arg(project_id)::uuid AND id=sqlc.arg(client_id)::uuid FOR UPDATE NOWAIT;

-- name: LockingFixtureFindOAuthClient :one
SELECT id FROM mcp_auth_clients WHERE organization_id=sqlc.arg(organization_id)::text AND remote_session_client_id=sqlc.arg(client_id)::uuid AND client_type='remote_session' AND NOT deleted;

-- name: LockingFixtureFindRemoteSource :one
SELECT remote_mcp_server_id FROM mcp_servers WHERE project_id=sqlc.arg(project_id)::uuid AND id=sqlc.arg(server_id)::uuid;

-- name: LockingFixtureCountLiveChoices :one
SELECT count(*) FROM mcp_server_auth_clients WHERE project_id=sqlc.arg(project_id)::uuid AND mcp_server_id=sqlc.arg(server_id)::uuid AND auth_client_id=ANY(sqlc.arg(auth_client_ids)::uuid[]) AND NOT deleted;

-- name: LockingFixtureTombstoneChoice :one
UPDATE mcp_server_auth_clients SET source='admin', deleted_at=clock_timestamp() WHERE project_id=sqlc.arg(project_id)::uuid AND mcp_server_id=sqlc.arg(server_id)::uuid AND auth_client_id=sqlc.arg(auth_client_id)::uuid AND NOT deleted RETURNING id;

-- name: LockingFixtureInsertGrant :exec
INSERT INTO principal_grants (organization_id, principal_urn, scope, selectors) SELECT sqlc.arg(organization_id)::text,'user:mixed-writer-test','mcp:connect',jsonb_build_object('resource_kind','mcp_server','resource_id',sqlc.arg(server_id)::text) FROM mcp_servers WHERE project_id=sqlc.arg(project_id)::uuid AND id=sqlc.arg(server_id)::text::uuid;

-- name: LockingFixtureSnapshotGrants :one
SELECT coalesce(jsonb_agg(to_jsonb(g) ORDER BY id),'[]'::jsonb)::text FROM principal_grants g WHERE organization_id=sqlc.arg(organization_id)::text;

-- name: LockingFixtureInsertSourceHeader :exec
INSERT INTO remote_mcp_server_headers (remote_mcp_server_id,name,is_secret,value) SELECT id,'X-Mixed-Lock',true,'test-encrypted-value' FROM remote_mcp_servers WHERE id=sqlc.arg(source_id)::uuid AND project_id=sqlc.arg(project_id)::uuid;

-- name: LockingFixtureCountHeaderCopies :one
SELECT count(*) FROM mcp_auth_client_headers WHERE organization_id=sqlc.arg(organization_id)::text AND auth_client_id=ANY(sqlc.arg(auth_client_ids)::uuid[]) AND name='X-Mixed-Lock' AND NOT deleted;

-- name: LockingFixtureClearUserIssuer :exec
UPDATE mcp_servers SET user_session_issuer_id=NULL WHERE project_id=sqlc.arg(project_id)::uuid AND id=sqlc.arg(server_id)::uuid;

-- name: LockingFixtureDeleteLegacyChoice :execrows
DELETE FROM mcp_server_remote_session_clients WHERE project_id=sqlc.arg(project_id)::uuid AND mcp_server_id=sqlc.arg(server_id)::uuid AND remote_session_client_id=sqlc.arg(client_id)::uuid;

-- name: LockingFixtureClearRemoteIssuer :exec
UPDATE mcp_servers SET remote_session_issuer_id=NULL WHERE project_id=sqlc.arg(project_id)::uuid AND id=sqlc.arg(server_id)::uuid;

-- name: LockingFixtureIsRemoteIssuerNull :one
SELECT (remote_session_issuer_id IS NULL)::boolean AS issuer_is_null FROM mcp_servers WHERE project_id=sqlc.arg(project_id)::uuid AND id=sqlc.arg(server_id)::uuid;

-- name: LockingFixtureLockAuthClients :exec
SELECT id FROM mcp_auth_clients WHERE organization_id=sqlc.arg(organization_id)::text AND id=ANY(sqlc.arg(auth_client_ids)::uuid[]) FOR UPDATE NOWAIT;

-- name: LockingFixtureLockAuthHeaders :exec
SELECT id FROM mcp_auth_client_headers WHERE organization_id=sqlc.arg(organization_id)::text AND auth_client_id=ANY(sqlc.arg(auth_client_ids)::uuid[]) FOR UPDATE NOWAIT;

-- name: LockingFixtureUpdateSourceHeader :exec
UPDATE remote_mcp_server_headers SET value='updated-test-encrypted-value' WHERE remote_mcp_server_id=sqlc.arg(source_id)::uuid AND name='X-Mixed-Lock' AND remote_mcp_server_id IN (SELECT id FROM remote_mcp_servers WHERE project_id=sqlc.arg(project_id)::uuid);

-- name: LockingFixtureCountLegacyChoice :one
SELECT count(*) FROM mcp_server_remote_session_clients WHERE project_id=sqlc.arg(project_id)::uuid AND mcp_server_id=sqlc.arg(server_id)::uuid AND remote_session_client_id=sqlc.arg(client_id)::uuid AND NOT deleted;

-- name: LockingFixtureReadTombstone :one
SELECT deleted FROM mcp_server_auth_clients WHERE project_id=sqlc.arg(project_id)::uuid AND id=sqlc.arg(id)::uuid AND source='admin';

-- name: LockingFixtureCountLiveChoice :one
SELECT count(*) FROM mcp_server_auth_clients WHERE project_id=sqlc.arg(project_id)::uuid AND mcp_server_id=sqlc.arg(server_id)::uuid AND auth_client_id=sqlc.arg(auth_client_id)::uuid AND NOT deleted;

-- name: LockingFixtureFindIssuerServer :one
SELECT id FROM mcp_servers
					WHERE project_id=sqlc.arg(project_id)::uuid AND user_session_issuer_id=sqlc.arg(user_issuer_id)::uuid AND NOT deleted;

-- name: LockingFixtureCountRegistrations :one
SELECT count(*) FROM remote_session_clients
                  WHERE project_id=sqlc.arg(project_id)::uuid AND client_id=sqlc.arg(external_id)::text AND NOT deleted;

-- name: LockingFixtureCountExternalBindings :one
SELECT count(*) FROM remote_session_client_user_session_issuers AS binding
                  JOIN remote_session_clients AS client ON client.id=binding.remote_session_client_id
                  WHERE client.project_id=sqlc.arg(project_id)::uuid AND client.client_id=sqlc.arg(external_id)::text AND binding.user_session_issuer_id=sqlc.arg(user_issuer_id)::uuid;

-- name: LockingFixtureCountServerLegacyChoices :one
SELECT count(*) FROM mcp_server_remote_session_clients
                  WHERE project_id=sqlc.arg(project_id)::uuid AND organization_id=sqlc.arg(organization_id)::text AND mcp_server_id=sqlc.arg(server_id)::uuid AND NOT deleted;

-- name: LockingFixtureCountServerTypedChoices :one
SELECT count(*) FROM mcp_server_auth_clients
                  WHERE project_id=sqlc.arg(project_id)::uuid AND organization_id=sqlc.arg(organization_id)::text AND mcp_server_id=sqlc.arg(server_id)::uuid AND NOT deleted;

-- name: LockingFixtureCountRegistrationLegacyChoices :one
SELECT count(*) FROM mcp_server_remote_session_clients
					WHERE project_id=sqlc.arg(project_id)::uuid AND organization_id=sqlc.arg(organization_id)::text AND mcp_server_id=sqlc.arg(server_id)::uuid
					AND remote_session_client_id=sqlc.arg(client_id)::uuid AND NOT deleted;

-- name: LockingFixtureCountEnabledOAuthChoices :one
SELECT count(*)
					FROM mcp_server_auth_clients AS link
					JOIN mcp_auth_clients AS client ON client.id=link.auth_client_id
					WHERE link.project_id=sqlc.arg(project_id)::uuid AND link.organization_id=sqlc.arg(organization_id)::text AND link.mcp_server_id=sqlc.arg(server_id)::uuid
					AND client.organization_id=sqlc.arg(organization_id)::text AND client.remote_session_client_id=sqlc.arg(client_id)::uuid
					AND NOT link.deleted AND NOT client.deleted AND client.enabled
					AND client.client_type='remote_session';
