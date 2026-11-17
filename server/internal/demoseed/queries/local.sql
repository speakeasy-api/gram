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
INSERT INTO principal_grants (organization_id, principal_urn, scope, selectors)
VALUES (@organization_id::text, 'user:' || @user_id::text, 'log:read',
        jsonb_build_object('resource_kind', 'project', 'resource_id', '*'))
ON CONFLICT (organization_id, principal_urn, scope, selectors) DO NOTHING;

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

-- The Enterprise Sign-In (ID-JAG) Server (local_ema.go). Locally the company
-- identity provider is the dev-idp's OAuth 2.1 server.
-- name: LocalEMAPointIdentityProvider :execrows
UPDATE remote_session_issuers
SET issuer = @issuer::text,
    authorization_endpoint = @issuer::text || '/authorize',
    token_endpoint = @issuer::text || '/token',
    userinfo_endpoint = @issuer::text || '/userinfo',
    revocation_endpoint = @issuer::text || '/revoke',
    registration_endpoint = NULL,
    jwks_uri = @issuer::text || '/.well-known/jwks.json',
    jwks = NULL, jwks_fetched_at = NULL, jwks_cache_expires_at = NULL, jwks_etag = NULL,
    scopes_supported = ARRAY['openid', 'email', 'profile'],
    grant_types_supported = ARRAY['authorization_code', 'refresh_token', 'urn:ietf:params:oauth:grant-type:token-exchange'],
    response_types_supported = ARRAY['code'],
    token_endpoint_auth_methods_supported = ARRAY['client_secret_post'],
    code_challenge_methods_supported = ARRAY['S256'],
    id_token_signing_alg_values_supported = ARRAY['RS256'],
    authorization_response_iss_parameter_supported = TRUE,
    oidc = TRUE,
    name = @name::text,
    updated_at = clock_timestamp()
WHERE id = @id::uuid AND organization_id = @organization_id::text AND project_id IS NULL AND deleted IS FALSE;

-- Speakeasy's registration at the dev-idp: its static login client, with the
-- checked-in local secret the dev-idp EMA app also holds.
-- name: LocalEMAPointIdentityProviderClient :execrows
UPDATE remote_session_clients
SET client_id = @client_id::text,
    client_secret_encrypted = @client_secret_encrypted::text,
    -- A replaced secret is a new revision, and the organization-key copy
    -- would hold the previous secret.
    client_secret_org_encrypted = NULL,
    client_secret_generation = client_secret_generation + 1,
    token_endpoint_auth_method = 'client_secret_post',
    grant_types = ARRAY['authorization_code', 'refresh_token', 'urn:ietf:params:oauth:grant-type:token-exchange'],
    scope = ARRAY['openid', 'email', 'profile'],
    callback_base_url = @callback_base_url::text,
    updated_at = clock_timestamp()
WHERE id = @id::uuid AND organization_id = @organization_id::text AND project_id IS NULL AND deleted IS FALSE;

-- The Enterprise Sign-In (ID-JAG) Server exists only in the local org: it
-- needs the dev-idp and the MCP fixture, which the demo org does not have.
-- Its rows sit in the seeded project (the project delete cascades them away)
-- except its organization sign-in, which the org-scoped user session issuer
-- delete removes on every reseed. Its organization sign-in federates through
-- the company provider: its trusted client is the one the caller signs in
-- with, and the ID token it returns is what Speakeasy exchanges for an ID-JAG.
-- name: LocalEMAInsertUserSessionIssuer :exec
INSERT INTO user_session_issuers (id, project_id, organization_id, slug,
                                  authn_challenge_mode, session_duration,
                                  authorization_server_mode,
                                  trusted_remote_session_issuer_id,
                                  trusted_remote_session_client_id) VALUES
  (@id::uuid, NULL, @org_id::text, 'enterprise-sign-in',
   'interactive', make_interval(secs => 14 * 24 * 60 * 60), 'endpoint',
   @trusted_issuer_id::uuid, @trusted_client_id::uuid);

-- name: LocalEMAInsertRemoteMCPServer :exec
INSERT INTO remote_mcp_servers (id, project_id, name, slug, transport_type, url) VALUES
  (@id::uuid, @project_id::uuid, 'Enterprise Sign-In (ID-JAG) Server', 'enterprise-sign-in',
   'streamable-http', @url::text);

-- name: LocalEMAInsertMCPServer :exec
INSERT INTO mcp_servers (id, project_id, name, slug, toolset_id,
                         remote_mcp_server_id, user_session_issuer_id,
                         visibility) VALUES
  (@id::uuid, @project_id::uuid, 'Enterprise Sign-In (ID-JAG) Server', 'enterprise-sign-in',
   NULL, @remote_id::uuid, @issuer_id::uuid, 'private');

-- name: LocalEMAInsertEndpoint :exec
INSERT INTO mcp_endpoints (id, project_id, meta_mcp_server_id, mcp_server_id, slug) VALUES
  (@id::uuid, @project_id::uuid, NULL, @mcp_server_id::uuid, @slug::text);

-- The server's authorization server is the dev-idp resource authorization
-- server for the fixture's /mcp/ema endpoint, which accepts ID-JAGs from a
-- confidential client. Identity chaining trusts its JWT-bearer and ID-JAG
-- support only from a discovered document, so the row records when that
-- document was read; without it the binding reads as incomplete metadata and
-- the consent page offers an interactive sign-in the server does not have.
-- name: LocalEMAInsertVendorIssuer :exec
INSERT INTO remote_session_issuers
  (id, project_id, organization_id, slug, issuer, token_endpoint,
   introspection_endpoint, jwks_uri, name, scopes_supported,
   grant_types_supported, authorization_grant_profiles_supported,
   token_endpoint_auth_methods_supported, metadata_fetched_at)
VALUES
  (@id::uuid, @project_id::uuid, @org_id::text,
   'enterprise-sign-in-auth', @issuer::text, @issuer::text || '/token',
   @issuer::text || '/introspect', @issuer::text || '/.well-known/jwks.json',
   'Enterprise Sign-In Authorization Server', @scopes::text[],
   ARRAY['urn:ietf:params:oauth:grant-type:jwt-bearer'],
   ARRAY['urn:ietf:params:oauth:grant-profile:id-jag'],
   ARRAY['client_secret_post'], clock_timestamp());

-- name: LocalEMAInsertVendorClient :exec
INSERT INTO remote_session_clients
  (id, project_id, organization_id, remote_session_issuer_id, client_id,
   client_secret_encrypted, token_endpoint_auth_method, grant_types, scope)
VALUES
  (@id::uuid, @project_id::uuid, @org_id::text,
   @issuer_id::uuid, @client_id::text,
   @client_secret_encrypted::text, 'client_secret_post',
   ARRAY['urn:ietf:params:oauth:grant-type:jwt-bearer'],
   @scopes::text[]);

-- name: LocalEMAInsertProtectedResource :exec
INSERT INTO remote_protected_resources
  (id, project_id, organization_id, resource_identifier, metadata_url,
   authorization_servers, scopes_supported, metadata_fetched_at)
VALUES
  (@id::uuid, @project_id::uuid, @org_id::text, @resource::text,
   @metadata_url::text, ARRAY[@issuer::text], @scopes::text[],
   clock_timestamp());

-- name: LocalEMAInsertBinding :exec
INSERT INTO remote_session_ema_bindings
  (id, project_id, organization_id, user_session_issuer_id,
   remote_session_issuer_id, resource, remote_session_client_id, generation,
   state, grant_source, created_at, updated_at)
VALUES
  (@id::uuid, @project_id::uuid, @org_id::text,
   @user_session_issuer_id::uuid, @issuer_id::uuid,
   @resource::text, @client_id::uuid, 1,
   'ready', 'administrator_declared',
   now() - interval '6 days', now() - interval '6 days');

-- Locally a vendor (Linear, Notion) cannot fetch a client ID metadata
-- document from localhost, so the shared seed's metadata document client
-- becomes the public client the local seed registered with the vendor by
-- dynamic registration.
-- name: LocalVendorAppPointRegisteredClient :execrows
UPDATE remote_session_clients
SET client_id = @client_id::text, client_id_metadata_uri = NULL,
    callback_base_url = @callback_base_url::text, updated_at = clock_timestamp()
WHERE id = @id::uuid AND project_id = @project_id::uuid AND deleted IS FALSE;

-- Provisions the developer in the local org's directory, linked to their
-- user, the way directory sync would. Federated sign-in (the Enterprise
-- Sign-In server) admits only directory-provisioned people.
-- name: LocalProvisionDeveloperDirectoryUser :exec
INSERT INTO directory_users
  (organization_id, user_id, workos_directory_user_id, email, first_name,
   last_name, workos_created_at, workos_updated_at)
VALUES (@organization_id::text, @user_id::text, 'devidp_dir_' || @user_id::text,
        @email::text, @first_name::text, NULLIF(@last_name::text, ''),
        clock_timestamp(), clock_timestamp())
ON CONFLICT (workos_directory_user_id) DO UPDATE
SET organization_id = EXCLUDED.organization_id, user_id = EXCLUDED.user_id,
    email = EXCLUDED.email, first_name = EXCLUDED.first_name,
    last_name = EXCLUDED.last_name,
    deleted_at = NULL, workos_deleted_at = NULL,
    workos_updated_at = clock_timestamp(), updated_at = clock_timestamp();

-- name: LocalUpsertInspectRemoteServer :exec
-- Remote MCP servers for the Inspect tab that exist only locally: the mock
-- task tracker (server/cmd/mock-mcp), an unreachable upstream and a disabled
-- copy of the mock. They live in the seeded project, so the next seed run's
-- projects cascade removes them before the fixtures add them back.
INSERT INTO remote_mcp_servers (id, project_id, name, slug, transport_type, url)
VALUES (@id::uuid, @project_id::uuid, @name::text, @slug::text, 'streamable-http', @url::text)
ON CONFLICT (id) DO UPDATE
  SET name = EXCLUDED.name, slug = EXCLUDED.slug, url = EXCLUDED.url,
      deleted_at = NULL, updated_at = clock_timestamp();

-- name: LocalUpsertInspectMCPServer :exec
-- Like DeepWiki, each takes no upstream credential and sits under the shared
-- workforce issuer, so the local developer connects without a sign-in.
INSERT INTO mcp_servers (id, project_id, name, slug, remote_mcp_server_id,
                         user_session_issuer_id, visibility)
VALUES (@id::uuid, @project_id::uuid, @name::text, @slug::text, @remote_mcp_server_id::uuid,
        @user_session_issuer_id::uuid, @visibility::text)
ON CONFLICT (id) DO UPDATE
  SET name = EXCLUDED.name, slug = EXCLUDED.slug, visibility = EXCLUDED.visibility,
      deleted_at = NULL, updated_at = clock_timestamp();

-- name: LocalUpsertInspectEndpoint :exec
-- Restores the fixture endpoint if it was soft-deleted, but only within the
-- local fixture's project.
INSERT INTO mcp_endpoints (id, project_id, mcp_server_id, slug)
VALUES (@id::uuid, @project_id::uuid, @mcp_server_id::uuid, @slug::text)
ON CONFLICT (id) DO UPDATE
  SET mcp_server_id = EXCLUDED.mcp_server_id, meta_mcp_server_id = NULL, slug = EXCLUDED.slug,
      deleted_at = NULL, updated_at = clock_timestamp()
  WHERE mcp_endpoints.project_id = EXCLUDED.project_id;

-- name: LocalGrantRoleReadOnlyConnect :execrows
-- A role limited to the read-only tools of one server, the read_only reason
-- on the Inspect tab for its members. Custom role grants are cleared with
-- the roles on every seed run.
INSERT INTO principal_grants (organization_id, principal_urn, scope, selectors)
SELECT @organization_id::text, 'role:organization:' || r.id, 'mcp:connect',
       jsonb_build_object('resource_kind', 'mcp',
         'resource_id', (@mcp_server_id::uuid)::text, 'disposition', 'read_only')
FROM organization_roles r
WHERE r.organization_id = @organization_id::text AND r.workos_slug = @role_slug::text
ON CONFLICT DO NOTHING;
