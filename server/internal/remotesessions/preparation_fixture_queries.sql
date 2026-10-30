-- Package-local fixtures for preparation publication and concurrency tests.

-- name: GetPreparationServerSourceFixture :one
SELECT remote_mcp_server_id FROM mcp_servers WHERE id=sqlc.arg(server_id);

-- name: GetPreparationTypedClientDeletedFixture :one
SELECT deleted FROM mcp_auth_clients WHERE id=sqlc.arg(client_id);

-- name: InsertPreparationSourceHeaderFixtureEnterprise :exec
INSERT INTO remote_mcp_server_headers(remote_mcp_server_id,name,is_secret,value) SELECT s.id,'X-Enterprise-Key',true,sqlc.arg(encrypted_value) FROM remote_mcp_servers s WHERE s.id = sqlc.arg(source_id) AND s.project_id = sqlc.arg(project_id);

-- name: ListPreparationTypedClientsFixture :many
SELECT c.id FROM mcp_auth_clients c
 JOIN mcp_auth_client_emas e ON e.auth_client_id = c.id
 WHERE c.organization_id = sqlc.arg(organization_id) AND c.remote_session_client_id = sqlc.arg(client_id)
 AND c.client_type = 'ema' AND NOT c.deleted AND NOT e.deleted
 AND rtrim(e.resource, '/') = rtrim(sqlc.arg(resource)::text, '/');

-- name: CountPreparationPrincipalGrantsFixture :one
SELECT count(*) FROM principal_grants WHERE organization_id = sqlc.arg(organization_id);

-- name: GetPreparationLiveLinkFixture :one
SELECT id FROM mcp_server_auth_clients WHERE organization_id = sqlc.arg(organization_id) AND mcp_server_id = sqlc.arg(server_id) AND auth_client_id = sqlc.arg(client_id) AND NOT deleted;

-- name: GetPreparationTypedLinkStateFixture :one
SELECT c.enabled, l.deleted FROM mcp_auth_clients c JOIN mcp_server_auth_clients l ON l.auth_client_id = c.id WHERE c.organization_id = sqlc.arg(organization_id) AND c.id = sqlc.arg(client_id) AND l.id = sqlc.arg(link_id);

-- name: CountPreparationLiveLinksFixture :one
SELECT count(*) FROM mcp_server_auth_clients WHERE organization_id = sqlc.arg(organization_id) AND mcp_server_id = sqlc.arg(server_id) AND auth_client_id = sqlc.arg(client_id) AND NOT deleted;

-- name: CountPreparationBindingsFixtureForUser :one
SELECT count(*) FROM remote_session_ema_bindings WHERE organization_id = sqlc.arg(organization_id) AND project_id = sqlc.arg(project_id) AND user_session_issuer_id = sqlc.arg(user_issuer_id);

-- name: GetPreparationClientGrantsFixture :one
SELECT grant_types FROM remote_session_clients WHERE id = sqlc.arg(client_id);

-- name: DisablePreparationTypedClientFixture :exec
UPDATE mcp_auth_clients SET enabled = false WHERE organization_id = sqlc.arg(organization_id) AND id = sqlc.arg(client_id) AND project_id = sqlc.arg(project_id);

-- name: DetachPreparationTypedLinkFixture :exec
UPDATE mcp_server_auth_clients SET source = 'admin', deleted_at = clock_timestamp() WHERE organization_id = sqlc.arg(organization_id) AND id = sqlc.arg(link_id) AND project_id = sqlc.arg(project_id);

-- name: CountPreparationBindingsFixtureForIssuer :one
SELECT count(*) FROM remote_session_ema_bindings WHERE organization_id = sqlc.arg(organization_id) AND remote_session_issuer_id = sqlc.arg(issuer_id);

-- name: CountPreparationBindingsFixtureReadyForIssuer :one
SELECT count(*) FROM remote_session_ema_bindings WHERE organization_id = sqlc.arg(organization_id) AND remote_session_issuer_id = sqlc.arg(issuer_id) AND state = 'ready';

-- name: CountPreparationLiveLinksFixtureAcrossServers :one
SELECT count(*) FROM mcp_server_auth_clients WHERE organization_id = sqlc.arg(organization_id) AND auth_client_id = sqlc.arg(client_id) AND NOT deleted AND mcp_server_id IN (sqlc.arg(first_server_id), sqlc.arg(second_server_id));

-- name: SetPreparationInheritedClientSecretFixture :exec
UPDATE remote_session_clients SET token_endpoint_auth_method = 'client_secret_basic', client_secret_encrypted = sqlc.arg(encrypted_secret), grant_types = sqlc.arg(grant_types) WHERE id = sqlc.arg(client_id) AND organization_id = sqlc.arg(organization_id) AND project_id IS NULL;

-- name: GetPreparationBindingStateFixture :one
SELECT state FROM remote_session_ema_bindings WHERE id = sqlc.arg(binding_id) AND project_id = sqlc.arg(project_id);

-- name: GetPreparationBindingCompletionFixture :one
SELECT state, remote_session_client_id FROM remote_session_ema_bindings WHERE organization_id = sqlc.arg(organization_id) AND project_id = sqlc.arg(project_id) AND user_session_issuer_id = sqlc.arg(user_issuer_id);

-- name: CountPreparationRollbackClientsFixture :one
SELECT count(*) FROM remote_session_clients WHERE project_id = sqlc.arg(project_id) AND client_id = 'rollback-client';

-- name: DeletePreparationTypedClientFixture :exec
DELETE FROM mcp_auth_clients WHERE organization_id = sqlc.arg(organization_id) AND id = sqlc.arg(client_id) AND project_id = sqlc.arg(project_id);

-- name: ClearPreparationIssuerMetadataErrorFixture :exec
UPDATE remote_session_issuers SET metadata_last_error = NULL, metadata_last_error_at = NULL, metadata_last_error_url = NULL WHERE id = sqlc.arg(issuer_id) AND project_id = sqlc.arg(project_id);

-- name: CountPreparationBindingsFixtureForOrganization :one
SELECT count(*) FROM remote_session_ema_bindings WHERE organization_id=sqlc.arg(organization_id);

-- name: CountPreparationHeaderCopiesFixture :one
SELECT count(*) FROM mcp_auth_client_headers WHERE auth_client_id=sqlc.arg(client_id) AND organization_id=sqlc.arg(organization_id) AND NOT deleted;

-- name: GetPreparationGatewayDisabledFixture :one
SELECT NOT deleted FROM organization_features WHERE organization_id=sqlc.arg(organization_id) AND feature_name='organization_gateway_disabled';

-- name: InsertPreparationSourceHeaderFixtureGateway :exec
INSERT INTO remote_mcp_server_headers(remote_mcp_server_id,name,is_secret,value) SELECT s.remote_mcp_server_id,'X-Gateway-Test',true,'encrypted-test-value' FROM mcp_servers s WHERE s.id=sqlc.arg(server_id) AND s.project_id = sqlc.arg(project_id);

-- name: InsertPreparationSourceHeaderFixtureLock :exec
INSERT INTO remote_mcp_server_headers (remote_mcp_server_id,name,is_secret,value) SELECT s.id,'X-Lock-Test',true,'encrypted-test-value' FROM remote_mcp_servers s WHERE s.id = sqlc.arg(source_id) AND s.project_id = sqlc.arg(project_id);

-- name: CountPreparationRemoteServersFixture :one
SELECT count(*) FROM remote_mcp_servers WHERE project_id=sqlc.arg(project_id);

-- name: GetPreparationTypedClientEnabledFixture :one
SELECT enabled FROM mcp_auth_clients WHERE organization_id=sqlc.arg(organization_id) AND id=sqlc.arg(client_id);
