-- Local-only development fixtures (RunLocalFixtures). Never part of the tenant
-- seed phases.

-- name: LocalLinkWorkOSOrg :exec
-- Stamp the WorkOS link the auth callback would otherwise add on first login.
-- Setting it up front means org lookups by workos_id resolve before anyone has
-- logged in, and the callback finds an already-linked row to update rather
-- than a bare one. It is local-only because the shared demo org has no WorkOS
-- counterpart at all.
UPDATE organization_metadata
SET workos_id = @workos_org_id::text, updated_at = clock_timestamp()
WHERE id = @organization_id::text AND workos_id IS DISTINCT FROM @workos_org_id::text;

-- name: LocalUpsertDeveloper :one
-- You, as a real member.
--
-- Keyed on email, not id: a developer who logged in before seeding already has
-- a row whose id the auth callback derived from the dev-idp subject
-- (users.UserIDFromWorkOSID), which is not the id derived here from the email.
-- ON CONFLICT (id) would miss that row and collide on users_email_key instead,
-- so the statement returns the id that actually exists and the rest of the
-- fixtures use it. Login-first and seed-first therefore converge on one row:
-- the auth callback resolves an existing user by email too (see
-- identity.resolveGramUserID).
--
-- users.workos_id is the subject the dev-idp will present at login. It has to
-- be the WorkOS-shaped form, not a bare UUID — a mismatch there is invisible
-- until login mints a second user.
INSERT INTO users (id, email, display_name, workos_id, admin)
VALUES (@id::text, @email::text, @display_name::text, @workos_id::text, TRUE)
ON CONFLICT (email) DO UPDATE
  SET display_name = EXCLUDED.display_name,
      workos_id = EXCLUDED.workos_id,
      admin = TRUE, deleted_at = NULL, updated_at = clock_timestamp()
RETURNING id, workos_id;

-- name: LocalAdoptDeveloper :exec
-- Your membership and Admin role assignment, against the user id
-- LocalUpsertDeveloper resolved.
WITH membership AS (
  INSERT INTO organization_user_relationships
    (organization_id, user_id, workos_user_id, workos_membership_id)
  VALUES (@organization_id::text, @user_id::text, @workos_user_id::text, 'devidp_mem_' || @user_id::text)
  ON CONFLICT (organization_id, user_id) WHERE deleted IS FALSE
  DO UPDATE SET
    workos_user_id = EXCLUDED.workos_user_id,
    workos_membership_id = EXCLUDED.workos_membership_id,
    deleted_at = NULL, updated_at = clock_timestamp()
  RETURNING user_id, workos_user_id, workos_membership_id
),
admin_role AS (
  SELECT id FROM global_roles
  WHERE workos_slug = 'admin' AND deleted IS FALSE AND workos_deleted IS FALSE
  LIMIT 1
)
INSERT INTO organization_role_assignments
  (organization_id, workos_user_id, user_id, role_urn, workos_membership_id, workos_updated_at)
SELECT @organization_id::text, membership.workos_user_id, membership.user_id,
       'role:global:' || admin_role.id::text, membership.workos_membership_id, clock_timestamp()
FROM membership CROSS JOIN admin_role
ON CONFLICT (organization_id, workos_user_id, role_urn) WHERE deleted_at IS NULL
DO UPDATE SET deleted_at = NULL, updated_at = clock_timestamp();

-- name: LocalGrantSessionVisibility :exec
-- chat:read is deliberately absent from the Admin system role, so the seeded
-- chats (owned by the fictional teammates) would otherwise be invisible:
-- without it chatVisibilityScope falls back to own-sessions-only.
INSERT INTO principal_grants (organization_id, principal_urn, scope, selectors)
VALUES (@organization_id::text, 'user:' || @user_id::text, 'chat:read',
        jsonb_build_object('resource_kind', 'chat', 'resource_id', '*'))
ON CONFLICT (organization_id, principal_urn, scope, selectors) DO NOTHING;

-- name: LocalGrantToolLogVisibility :exec
-- log:read is likewise held by no system role, so without it the Tool Logs page
-- and the session Tool calls view would be closed to the local admin.
INSERT INTO principal_grants (organization_id, principal_urn, scope, effect, selectors)
VALUES (@organization_id::text, 'user:' || @user_id::text, 'log:read', NULL,
        jsonb_build_object('resource_kind', 'project', 'resource_id', '*'))
ON CONFLICT (organization_id, principal_urn, scope, COALESCE(effect, 'allow'), selectors) DO NOTHING;

-- name: LocalEnablePlatformMCPFeature :exec
-- platform_mcp is the default-on organization entitlement locally; the shared
-- demo org deliberately does not enable it (its Plugins pages stay empty).
INSERT INTO organization_features (organization_id, feature_name)
VALUES (@organization_id::text, 'platform_mcp')
ON CONFLICT (organization_id, feature_name) WHERE deleted IS FALSE DO NOTHING;

-- name: LocalEnableCodeModeFeature :exec
-- Code Mode is a staff-managed entitlement; enabled locally so the seeded Code
-- Mode gateway runs against the local runtime. The shared demo org never gets
-- it: once a deployment runs the code runtime, Code Mode spends runner
-- capacity on whoever explores the demo.
INSERT INTO organization_features (organization_id, feature_name)
VALUES (@organization_id::text, 'code_mode')
ON CONFLICT (organization_id, feature_name) WHERE deleted IS FALSE DO NOTHING;

-- name: LocalAddToolApprover :exec
-- The developer approves the seeded tool approval policy too, so the Tool
-- approvals tab offers Approve once and Deny locally. The shared demo org keeps
-- its demo approvers only.
UPDATE risk_policies
SET approval_config = jsonb_set(approval_config, '{approvers}', (approval_config -> 'approvers') || to_jsonb(@approver::text))
WHERE organization_id = @organization_id::text AND id = @policy_id::uuid AND NOT (approval_config -> 'approvers') ? @approver::text;

-- name: LocalEnableIdentityFeatures :exec
-- SSO and directory sync (scim) are paid entitlements that gate the WorkOS
-- admin portal; enabled locally so the identity setup flow works end to end.
INSERT INTO organization_features (organization_id, feature_name)
VALUES (@organization_id::text, 'sso'), (@organization_id::text, 'scim')
ON CONFLICT (organization_id, feature_name) WHERE deleted IS FALSE DO NOTHING;

-- name: LocalVerifyEmailDomain :exec
-- The developer's email domain is verified locally so a dev-idp SSO sign-in
-- passes the SSO identity check end to end.
UPDATE organization_metadata
SET verified_domains = array_append(COALESCE(verified_domains, '{}'), @domain::text)
WHERE id = @organization_id::text
  AND @domain::text <> ''
  AND NOT EXISTS (
      SELECT 1 FROM unnest(COALESCE(verified_domains, '{}')) AS d WHERE lower(d) = lower(@domain::text)
  );

-- name: LocalUpsertAPIKey :exec
-- A key with a well-known hash. The plaintext is never stored, so the row is
-- upserted by id and the value lives in mise.toml.
INSERT INTO api_keys (id, organization_id, project_id, created_by_user_id, name, key_prefix, key_hash, scopes)
VALUES (@id::uuid, @organization_id::text, @project_id::uuid, @created_by_user_id::text, @name::text, @key_prefix::text, @key_hash::text, ARRAY['producer', 'chat'])
ON CONFLICT (id) DO UPDATE
  SET organization_id = EXCLUDED.organization_id, project_id = EXCLUDED.project_id,
      created_by_user_id = EXCLUDED.created_by_user_id, name = EXCLUDED.name,
      key_prefix = EXCLUDED.key_prefix, key_hash = EXCLUDED.key_hash,
      scopes = EXCLUDED.scopes, deleted_at = NULL, updated_at = clock_timestamp();

-- name: LocalGrantAPIKeyPolicy :exec
-- LocalGrantAPIKeyPolicy gives the fixed key what its scopes reach today, so the
-- mandatory server check allows it.
INSERT INTO principal_grants (organization_id, principal_urn, scope, selectors)
VALUES (@organization_id::text, 'api_key:' || @api_key_id::text, 'mcp:connect',
        jsonb_build_object('resource_kind', 'mcp', 'resource_id', '*', 'project_id', @project_id::text))
ON CONFLICT DO NOTHING;

-- name: LocalUpsertEnvironment :exec
INSERT INTO environments (id, organization_id, project_id, name, slug)
VALUES (@id::uuid, @organization_id::text, @project_id::uuid, 'Default', 'default')
ON CONFLICT (project_id, slug) WHERE deleted IS FALSE
DO UPDATE SET name = EXCLUDED.name, updated_at = clock_timestamp();

-- name: LocalSetCodeModeGateway :exec
-- The shared seed stores this gateway as Progressive because the demo org has
-- no code runtime; local stacks can run one, so the gateway shows Code Mode.
UPDATE meta_mcp_servers SET discovery_mode = 'code_mode', updated_at = clock_timestamp()
WHERE organization_id = @organization_id::text AND project_id = @project_id::uuid AND name = 'Python Code Gateway' AND deleted IS FALSE;

-- name: LocalUpsertMCPAppAsset :exec
-- The assets row the archive is uploaded to.
INSERT INTO assets (id, project_id, organization_id, name, url, kind, content_type, content_length, sha256)
VALUES (@id::uuid, @project_id::uuid, @organization_id::text, 'playground-mcp-app.zip', @url::text, 'functions', 'application/zip', @content_length::bigint, @sha256::text)
ON CONFLICT (id) DO UPDATE
  SET url = EXCLUDED.url, content_length = EXCLUDED.content_length,
      sha256 = EXCLUDED.sha256, content_type = EXCLUDED.content_type,
      deleted_at = NULL, updated_at = clock_timestamp();

-- name: LocalUpsertMCPApp :exec
-- The function stack hangs off the deployment the seed already created, so the
-- seed's existing scoped delete of that deployment cascades all of it away on
-- the next run — no extra cleanup, and nothing new for the safety test to
-- police.
WITH fn AS (
  INSERT INTO deployments_functions (id, deployment_id, asset_id, name, slug, runtime)
  VALUES (@function_id::uuid, @deployment_id::uuid, @asset_id::uuid, 'Playground MCP App', @function_slug::text, @runtime::text)
  ON CONFLICT (deployment_id, slug) DO UPDATE
    SET asset_id = EXCLUDED.asset_id, runtime = EXCLUDED.runtime
  RETURNING id
),
tool AS (
  INSERT INTO function_tool_definitions
    (tool_urn, project_id, deployment_id, function_id, runtime, name, description, input_schema, meta)
  SELECT @tool_urn::text, @project_id::uuid, @deployment_id::uuid, fn.id, @runtime::text, @tool_name::text, @tool_description::text, @input_schema::jsonb,
         jsonb_build_object('ui/resourceUri', @resource_uri::text)
  FROM fn
  ON CONFLICT (deployment_id, tool_urn) WHERE deleted IS FALSE
  DO UPDATE SET
    function_id = EXCLUDED.function_id, input_schema = EXCLUDED.input_schema,
    description = EXCLUDED.description, meta = EXCLUDED.meta,
    deleted_at = NULL, updated_at = clock_timestamp()
  RETURNING id
)
INSERT INTO function_resource_definitions
  (resource_urn, project_id, deployment_id, function_id, runtime, name, description, uri, title, mime_type, meta)
SELECT @resource_urn::text, @project_id::uuid, @deployment_id::uuid, fn.id, @runtime::text, 'playground_dashboard',
       'A tiny interactive HTML dashboard for validating MCP Apps in the playground',
       @resource_uri::text, 'Playground MCP App', 'text/html;profile=mcp-app',
       jsonb_build_object('ui', jsonb_build_object('prefersBorder', true))
FROM fn
ON CONFLICT (deployment_id, resource_urn) WHERE deleted IS FALSE
DO UPDATE SET
  function_id = EXCLUDED.function_id, uri = EXCLUDED.uri, meta = EXCLUDED.meta,
  deleted_at = NULL, updated_at = clock_timestamp();

-- name: LocalAttachMCPAppToToolset :exec
-- The tool is only reachable from the Playground once a toolset lists it. The
-- seed stamps a fresh toolset_versions row per run (the version is epoch
-- seconds, to bust the Redis content cache), so append to the newest version
-- of the primary seeded toolset — named, not "whichever sorts last", so the
-- Playground always finds the app in the same place.
UPDATE toolset_versions
SET tool_urns = array_append(tool_urns, @tool_urn::text),
    resource_urns = array_append(resource_urns, @resource_urn::text)
WHERE id = (
  SELECT tv.id FROM toolset_versions tv
  JOIN toolsets t ON t.id = tv.toolset_id
  WHERE t.organization_id = @organization_id::text AND t.slug = @toolset_slug::text AND t.deleted IS FALSE
  ORDER BY tv.version DESC, tv.id DESC
  LIMIT 1
)
AND NOT (@tool_urn::text = ANY(tool_urns));

-- Local MCP fixture (local_mcp_fixture.go). These point a few seeded servers
-- at the throwaway fixture server (mcp-fixture/) so they work right after
-- seeding. Local org only: the public demo org cannot reach localhost.

-- name: LocalFixtureUpsertEnvironment :exec
INSERT INTO environments (id, organization_id, project_id, name, slug, description)
VALUES (@id::uuid, @organization_id::text, @project_id::uuid, 'MCP fixture', 'mcp-fixture',
        'Points the demo API servers at the local MCP fixture''s API stub.')
ON CONFLICT (project_id, slug) WHERE deleted IS FALSE
DO UPDATE SET name = EXCLUDED.name, description = EXCLUDED.description, updated_at = clock_timestamp();

-- name: LocalFixtureUpsertEnvironmentEntry :execrows
INSERT INTO environment_entries (environment_id, name, value, is_secret)
SELECT e.id, @name::text, @value::text, FALSE
FROM environments e
WHERE e.id = @environment_id::uuid AND e.project_id = @project_id::uuid AND e.deleted IS FALSE
ON CONFLICT (environment_id, name)
DO UPDATE SET value = EXCLUDED.value, is_secret = FALSE, updated_at = clock_timestamp();

-- A toolset's environment is system-provided: it wins over any caller's own.
-- name: LocalFixtureLinkToolsetEnvironments :execrows
INSERT INTO toolset_environments (toolset_id, project_id, environment_id)
SELECT t.id, t.project_id, @environment_id::uuid
FROM toolsets t
WHERE t.project_id = @project_id::uuid AND t.deleted IS FALSE
  AND t.id = ANY(@toolset_ids::uuid[])
ON CONFLICT (toolset_id)
DO UPDATE SET environment_id = EXCLUDED.environment_id, updated_at = clock_timestamp();

-- name: LocalFixturePointRemoteServer :execrows
UPDATE remote_mcp_servers SET url = @url::text, updated_at = clock_timestamp()
WHERE id = @id::uuid AND project_id = @project_id::uuid AND deleted IS FALSE;

-- name: LocalFixtureSetRemoteHeaderValue :execrows
UPDATE remote_mcp_server_headers h SET value = @value::text, is_secret = FALSE, updated_at = clock_timestamp()
FROM remote_mcp_servers r
WHERE h.id = @id::uuid AND h.remote_mcp_server_id = r.id
  AND r.project_id = @project_id::uuid AND h.deleted IS FALSE AND h.value IS NOT NULL;

-- Upserted by a fixed id, so a reseed rewrites it in place.
-- name: LocalFixtureUpsertForwardedHeader :execrows
INSERT INTO remote_mcp_server_headers (id, remote_mcp_server_id, name, description, is_required, is_secret, value_from_request_header)
SELECT @id::uuid, r.id, @name::text, 'Your own upstream token, forwarded from this request header', TRUE, TRUE, @from_request_header::text
FROM remote_mcp_servers r
WHERE r.id = @remote_mcp_server_id::uuid AND r.project_id = @project_id::uuid AND r.deleted IS FALSE
ON CONFLICT (id) DO UPDATE
SET name = EXCLUDED.name, value_from_request_header = EXCLUDED.value_from_request_header,
    value = NULL, deleted_at = NULL, updated_at = clock_timestamp();

-- name: LocalFixturePointIssuer :execrows
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

-- name: LocalFixturePointProtectedResource :execrows
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
-- name: LocalFixturePointMetadataDocumentClient :execrows
UPDATE remote_session_clients
SET client_id = @client_id::text, client_id_metadata_uri = @client_id::text,
    callback_base_url = @callback_base_url::text, updated_at = clock_timestamp()
WHERE id = @id::uuid AND project_id = @project_id::uuid AND deleted IS FALSE;

-- name: LocalFixtureSetHeaderValue :execrows
UPDATE mcp_auth_client_headers SET value = @value::text, is_secret = FALSE, updated_at = clock_timestamp()
WHERE id = @id::uuid AND organization_id = @organization_id::text;

-- The live seeded sign-ins on the given clients, with the email each signed in as.
-- name: LocalFixtureListLiveSessions :many
SELECT rs.id, COALESCE(u.email, rs.upstream_email, '')::text AS email
FROM remote_sessions rs
JOIN remote_session_clients c
  ON c.id = rs.remote_session_client_id AND c.project_id = @project_id::uuid
LEFT JOIN users u ON rs.subject_urn = 'user:' || u.id
WHERE rs.remote_session_client_id = ANY(@client_ids::uuid[])
  AND rs.deleted IS FALSE
  AND rs.access_expires_at > clock_timestamp()
ORDER BY rs.id;

-- name: LocalFixtureSetSessionTokens :execrows
UPDATE remote_sessions
SET access_token_encrypted = @access_token_encrypted::text,
    access_expires_at = @access_expires_at::timestamptz,
    refresh_token_encrypted = @refresh_token_encrypted::text,
    refresh_expires_at = @refresh_expires_at::timestamptz,
    scopes = @scopes::text[],
    auto_refresh = TRUE,
    upstream_email = @email::text,
    resource = @resource::text,
    updated_at = clock_timestamp()
WHERE id = @id::uuid AND remote_session_client_id = ANY(@client_ids::uuid[]) AND deleted IS FALSE
  AND EXISTS (
    SELECT 1 FROM remote_session_clients c
    WHERE c.id = remote_sessions.remote_session_client_id AND c.project_id = @project_id::uuid
  );

-- The External OAuth Server's authorization server: the running fixture's.
-- name: LocalFixturePointExternalOAuth :execrows
UPDATE external_oauth_server_metadata SET metadata = @metadata::jsonb, authorization_server_issuer = NULL, updated_at = clock_timestamp()
WHERE id = @id::uuid AND project_id = @project_id::uuid AND deleted IS FALSE;

-- The Tunnelled Private Server's tunnel takes the fixture daemon's checked-in
-- agent key, so the tunnel is live locally. The shared seed keeps it inert.
-- name: LocalFixtureSetTunnelKey :execrows
UPDATE tunneled_mcp_servers SET key_hash = @key_hash::text, key_prefix = @key_prefix::text, status = 'active', updated_at = clock_timestamp()
WHERE id = @id::uuid AND project_id = @project_id::uuid AND deleted IS FALSE;
