-- The fixture servers (fixture_servers.go). These point the seeded servers
-- the MCP fixture backs at the fixture the Speakeasy server hosts under
-- /rpc/demoseed (internal/demoseed/fixture), in every organization the seed
-- writes, so they work right after seeding.

-- name: FixtureUpsertEnvironment :exec
INSERT INTO environments (id, organization_id, project_id, name, slug, description)
VALUES (@id::uuid, @organization_id::text, @project_id::uuid, 'MCP fixture', 'mcp-fixture',
        'Points the demo API servers at the MCP fixture''s API stub.')
ON CONFLICT (project_id, slug) WHERE deleted IS FALSE
DO UPDATE SET name = EXCLUDED.name, description = EXCLUDED.description, updated_at = clock_timestamp();

-- name: FixtureUpsertEnvironmentEntry :execrows
INSERT INTO environment_entries (environment_id, name, value, is_secret)
SELECT e.id, @name::text, @value::text, FALSE
FROM environments e
WHERE e.id = @environment_id::uuid AND e.project_id = @project_id::uuid AND e.deleted IS FALSE
ON CONFLICT (environment_id, name)
DO UPDATE SET value = EXCLUDED.value, is_secret = FALSE, updated_at = clock_timestamp();

-- A toolset's environment is system-provided: it wins over any caller's own.
-- name: FixtureLinkToolsetEnvironments :execrows
INSERT INTO toolset_environments (toolset_id, project_id, environment_id)
SELECT t.id, t.project_id, @environment_id::uuid
FROM toolsets t
WHERE t.project_id = @project_id::uuid AND t.deleted IS FALSE
  AND t.id = ANY(@toolset_ids::uuid[])
ON CONFLICT (toolset_id)
DO UPDATE SET environment_id = EXCLUDED.environment_id, updated_at = clock_timestamp();

-- name: FixturePointRemoteServer :execrows
UPDATE remote_mcp_servers SET url = @url::text, updated_at = clock_timestamp()
WHERE id = @id::uuid AND project_id = @project_id::uuid AND deleted IS FALSE;

-- name: FixtureSetRemoteHeaderValue :execrows
UPDATE remote_mcp_server_headers h SET value = @value::text, is_secret = FALSE, updated_at = clock_timestamp()
FROM remote_mcp_servers r
WHERE h.id = @id::uuid AND h.remote_mcp_server_id = r.id
  AND r.project_id = @project_id::uuid AND h.deleted IS FALSE AND h.value IS NOT NULL;

-- Upserted by a fixed id, so a reseed rewrites it in place.
-- name: FixtureUpsertForwardedHeader :execrows
INSERT INTO remote_mcp_server_headers (id, remote_mcp_server_id, name, description, is_required, is_secret, value_from_request_header)
SELECT @id::uuid, r.id, @name::text, 'Your own upstream token, forwarded from this request header', TRUE, TRUE, @from_request_header::text
FROM remote_mcp_servers r
WHERE r.id = @remote_mcp_server_id::uuid AND r.project_id = @project_id::uuid AND r.deleted IS FALSE
ON CONFLICT (id) DO UPDATE
SET name = EXCLUDED.name, value_from_request_header = EXCLUDED.value_from_request_header,
    value = NULL, deleted_at = NULL, updated_at = clock_timestamp();

-- name: FixturePointIssuer :execrows
UPDATE remote_session_issuers
SET issuer = @issuer::text,
    authorization_endpoint = @issuer::text || '/oauth/authorize',
    token_endpoint = @issuer::text || '/oauth/token',
    registration_endpoint = @issuer::text || '/oauth/register',
    revocation_endpoint = @issuer::text || '/oauth/revoke',
    jwks_uri = NULL,
    scopes_supported = @scopes::text[],
    scope_override = NULL,
    grant_types_supported = ARRAY['authorization_code', 'refresh_token'],
    response_types_supported = ARRAY['code'],
    token_endpoint_auth_methods_supported = ARRAY['none'],
    code_challenge_methods_supported = ARRAY['S256'],
    client_id_metadata_document_supported = TRUE,
    name = @name::text,
    metadata_fetched_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id::uuid AND project_id = @project_id::uuid AND deleted IS FALSE;

-- name: FixturePointProtectedResource :execrows
UPDATE remote_protected_resources
SET resource_identifier = @resource::text,
    metadata_url = @metadata_url::text,
    authorization_servers = ARRAY[@issuer::text],
    scopes_supported = @scopes::text[],
    scope_override = @scopes::text[],
    resource_name = 'Speakeasy MCP fixture (per-user OAuth)',
    metadata_fetched_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE project_id = @project_id::uuid AND organization_id = @organization_id::text
  AND resource_identifier = @old_resource::text AND deleted IS FALSE;

-- A client ID metadata document client: Speakeasy serves the document at
-- client_id, and the fixture fetches it to learn the redirect URI.
-- name: FixturePointMetadataDocumentClient :execrows
UPDATE remote_session_clients
SET client_id = @client_id::text, client_id_metadata_uri = @client_id::text,
    callback_base_url = @callback_base_url::text, updated_at = clock_timestamp()
WHERE id = @id::uuid AND project_id = @project_id::uuid AND deleted IS FALSE;

-- name: FixtureSetHeaderValue :execrows
UPDATE mcp_auth_client_headers SET value = @value::text, is_secret = FALSE, updated_at = clock_timestamp()
WHERE id = @id::uuid AND organization_id = @organization_id::text;

-- The live seeded sign-ins on the given clients, with the email each signed in as.
-- name: FixtureListLiveSessions :many
SELECT rs.id, COALESCE(u.email, rs.upstream_email, '')::text AS email
FROM remote_sessions rs
JOIN remote_session_clients c
  ON c.id = rs.remote_session_client_id AND c.project_id = @project_id::uuid
LEFT JOIN users u ON rs.subject_urn = 'user:' || u.id
WHERE rs.remote_session_client_id = ANY(@client_ids::uuid[])
  AND rs.deleted IS FALSE
  AND rs.access_expires_at > clock_timestamp()
ORDER BY rs.id;

-- name: FixtureSetSessionTokens :execrows
UPDATE remote_sessions
SET access_token_encrypted = @access_token_encrypted::text,
    access_expires_at = @access_expires_at::timestamptz,
    refresh_token_encrypted = @refresh_token_encrypted::text,
    refresh_expires_at = @refresh_expires_at::timestamptz,
    scopes = @scopes::text[],
    auto_refresh = TRUE,
    upstream_email = @email::text,
    resource = @resource::text,
    -- Sessions are inserted with a long re-check lease and a dummy token;
    -- these hold real fixture tokens that must refresh, so the lease is
    -- released.
    last_refresh_attempt_at = NULL,
    updated_at = clock_timestamp()
WHERE id = @id::uuid AND remote_session_client_id = ANY(@client_ids::uuid[]) AND deleted IS FALSE
  AND EXISTS (
    SELECT 1 FROM remote_session_clients c
    WHERE c.id = remote_sessions.remote_session_client_id AND c.project_id = @project_id::uuid
  );

-- The External OAuth Server's authorization server: the fixture's.
-- name: FixturePointExternalOAuth :execrows
UPDATE external_oauth_server_metadata SET metadata = @metadata::jsonb, authorization_server_issuer = NULL, updated_at = clock_timestamp()
WHERE id = @id::uuid AND project_id = @project_id::uuid AND deleted IS FALSE;

-- A client the fixture's DCR-only authorization server registered, as
-- Auto-Configure's dynamic registration would: no metadata document.
-- name: FixtureInsertRegisteredClient :exec
INSERT INTO remote_session_clients
  (id, project_id, organization_id, remote_session_issuer_id, client_id,
   client_id_metadata_uri, client_id_issued_at, token_endpoint_auth_method,
   callback_base_url)
VALUES
  (@id::uuid, @project_id::uuid, @organization_id::text,
   @remote_session_issuer_id::uuid, @client_id::text, NULL,
   clock_timestamp(), 'none', @callback_base_url::text);
