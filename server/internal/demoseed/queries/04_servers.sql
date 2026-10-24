-- MCP servers, gateway endpoints, plugins, meta MCP servers, remote sessions,
-- and killswitches (seed_04_servers.go). Each query is one statement of the
-- old demo.ensure_demo_org(); tenant identifiers arrive as parameters.

-- MCP servers and the Gateway Endpoint fronting them (AGE-3299).
-- Two backends so the gateway's member table shows both classes: the
-- toolset-backed pair executes in-process, the third-party remotes are
-- proxied. URLs are the vendors' public MCP endpoints: no credentials,
-- and nothing here connects on its own.
-- name: ServersInsertRemoteMCPServers :exec
INSERT INTO remote_mcp_servers (id, project_id, name, slug, transport_type, url) VALUES
  (@linear_id::uuid, @project_id::uuid, 'Linear', 'linear',
   'streamable-http', 'https://mcp.linear.app/mcp'),
  (@slack_id::uuid, @project_id::uuid, 'Slack', 'slack',
   'streamable-http', 'https://mcp.slack.com/mcp'),
  (@github_id::uuid, @project_id::uuid, 'GitHub', 'github',
   'streamable-http', 'https://api.githubcopilot.com/mcp/');

-- Remote-backed servers must carry a Speakeasy-as-AS issuer for their lifetime
-- (mcp_servers_issuer_required_check); the gateway gets its own so clients
-- authenticate to it rather than to a member.
-- session_duration must be a Microseconds-only interval: the user-session
-- mint rejects Months/Days components (see usersessions/minthandler.go).
-- The workforce issuer serves one shared authorization server for its MCP
-- servers at /oauth/usi/{id}. It is left unpinned: this seed runs against
-- every deployment, and an unpinned issuer URL follows the deployment's own
-- server URL.
-- name: ServersInsertUserSessionIssuers :exec
INSERT INTO user_session_issuers (id, project_id, organization_id, slug,
                                  authn_challenge_mode, session_duration,
                                  authorization_server_mode) VALUES
  (@workforce_id::uuid, NULL, @org_id::text, 'acme-workforce',
   'interactive', make_interval(secs => 14 * 24 * 60 * 60), 'shared'),
  (@linear_id::uuid, @project_id::uuid, @org_id::text, 'linear',
   'interactive', make_interval(secs => 14 * 24 * 60 * 60), 'endpoint'),
  (@slack_id::uuid, @project_id::uuid, @org_id::text, 'slack',
   'interactive', make_interval(secs => 14 * 24 * 60 * 60), 'endpoint'),
  (@gateway_id::uuid, @project_id::uuid, @org_id::text, 'acme-agent-gateway',
   'interactive', make_interval(secs => 14 * 24 * 60 * 60), 'endpoint');

-- name: ServersInsertMCPServers :exec
INSERT INTO mcp_servers (id, project_id, name, slug, toolset_id,
                         remote_mcp_server_id, user_session_issuer_id,
                         visibility) VALUES
  (@support_id::uuid, @project_id::uuid, 'Acme Support Tools',
   'acme-support-tools', @toolset_1::uuid, NULL, NULL, 'private'),
  (@ops_id::uuid, @project_id::uuid, 'Acme Ops', 'acme-ops',
   @toolset_2::uuid, NULL, NULL, 'private'),
  (@linear_id::uuid, @project_id::uuid, 'Linear', 'linear',
   NULL, @remote_linear_id::uuid,
   @issuer_linear_id::uuid, 'private'),
  (@slack_id::uuid, @project_id::uuid, 'Slack', 'slack',
   NULL, @remote_slack_id::uuid,
   @issuer_slack_id::uuid, 'private'),
  (@github_id::uuid, @project_id::uuid, 'GitHub', 'github',
   NULL, @remote_github_id::uuid,
   @issuer_workforce_id::uuid, 'private');

-- Every toolset with an MCP slug owns a canonical hosted wrapper (id = the
-- toolset id) and one endpoint at its address, mirroring its hosting
-- columns. The fresh-id servers above stay separate servers over the same
-- toolsets.
-- name: ServersInsertHostedToolsetMCPServers :exec
INSERT INTO mcp_servers (id, project_id, name, slug, toolset_id,
                         user_session_issuer_id, tool_variations_group_id,
                         visibility)
SELECT t.id, t.project_id, t.name, t.mcp_slug, t.id, t.user_session_issuer_id,
       t.tool_variations_group_id,
       CASE WHEN NOT t.mcp_enabled THEN 'disabled'
            WHEN t.mcp_is_public THEN 'public'
            ELSE 'private' END
FROM toolsets t
WHERE t.project_id = @project_id::uuid AND t.deleted IS FALSE AND t.mcp_slug IS NOT NULL;

-- The endpoint id is det_uuid(<name seed> || 'endpoint-hosted-' || slug),
-- computed in SQL with the same md5 formula as detUUID in names.go.
-- name: ServersInsertHostedToolsetEndpoints :exec
INSERT INTO mcp_endpoints (id, project_id, mcp_server_id, custom_domain_id, slug)
SELECT overlay(overlay(md5(@endpoint_name_prefix::text || t.slug) placing '5' from 13) placing '8' from 17)::uuid,
       t.project_id, t.id, t.custom_domain_id, t.mcp_slug
FROM toolsets t
WHERE t.project_id = @project_id::uuid AND t.deleted IS FALSE AND t.mcp_slug IS NOT NULL;

-- Check Access scenarios on GitHub. Contractors blocks connecting, which
-- wins over every other role's grant: Mateo (a contractor through the
-- directory) is blocked even though Engineer reaches every server, and his
-- own grant covering every server cannot outrank the block. Priya holds the
-- role too, but a grant made to her by name for GitHub outranks it. The
-- user grants are cleared by id at the top of the seed.
-- name: ServersInsertGitHubContractorsBlock :exec
INSERT INTO principal_grants (id, organization_id, principal_urn, scope, selectors)
SELECT @id::uuid, @org_id::text,
       'role:organization:' || r.id, 'mcp:blocked_connect',
       jsonb_build_object('resource_kind', 'mcp',
         'resource_id', (@github_server_id::uuid)::text)
FROM organization_roles r
WHERE r.organization_id = @org_id::text AND r.workos_slug = 'contractors';

-- Support Desk names its servers one by one, the MCP access tab's main
-- shape: Acme Support Tools with every tool (toolset-backed, so the grant
-- names the toolset), Slack only for tools annotated read-only.
-- name: ServersInsertSupportDeskGrants :exec
INSERT INTO principal_grants (organization_id, principal_urn, scope, selectors)
SELECT @org_id::text, 'role:organization:' || r.id, 'mcp:connect', sel
FROM organization_roles r
CROSS JOIN (VALUES
  (jsonb_build_object('resource_kind', 'mcp',
     'resource_id', (@toolset_1::uuid)::text)),
  (jsonb_build_object('resource_kind', 'mcp',
     'resource_id', (@slack_server_id::uuid)::text,
     'disposition', 'read_only'))
) AS v(sel)
WHERE r.organization_id = @org_id::text AND r.workos_slug = 'support-desk';

-- name: ServersInsertGitHubUserGrants :exec
INSERT INTO principal_grants (id, organization_id, principal_urn, scope, selectors)
VALUES
  (@direct_id::uuid, @org_id::text,
   'user:' || @direct_user_id::text, 'mcp:connect',
   jsonb_build_object('resource_kind', 'mcp',
     'resource_id', (@github_server_id::uuid)::text)),
  (@wildcard_id::uuid, @org_id::text,
   'user:' || @wildcard_user_id::text, 'mcp:connect',
   jsonb_build_object('resource_kind', 'mcp', 'resource_id', '*'));

-- Distribute these servers to the seeded organization roles holding
-- mcp:connect, including disposition-limited connect. mcp:read and
-- mcp:write still allow connecting at the endpoint but do not deliver
-- servers. Store server identities, not legacy toolset memberships. Content
-- has no manual/automatic distinction and never widens a plugin's audience.
-- name: ServersInsertRoleServerMemberships :execrows
INSERT INTO plugin_servers (plugin_id, mcp_server_id, display_name)
SELECT p.id, s.id, s.name
FROM plugins p
JOIN plugin_assignments a ON a.plugin_id = p.id AND a.organization_id = @org_id::text
JOIN organization_roles r ON a.principal_urn = 'role:organization:' || r.id
  AND r.organization_id = @org_id::text
CROSS JOIN mcp_servers s
WHERE p.organization_id = @org_id::text AND p.project_id = @project_id::uuid
  AND s.project_id = @project_id::uuid
  -- Canonical hosted wrappers share their toolset's name with the
  -- fresh-id servers above; distribute those servers only.
  AND s.id IS DISTINCT FROM s.toolset_id
  AND EXISTS (
    SELECT 1 FROM principal_grants g
    WHERE g.organization_id = @org_id::text AND g.principal_urn = a.principal_urn
      AND g.scope = 'mcp:connect'
      AND g.selectors->>'resource_kind' = 'mcp'
      AND g.selectors->>'resource_id' = '*');

-- A hand-curated supplement gives Team Access a multiple-plugin example:
-- Engineer reaches GitHub through both its role plugin and this one. Other
-- servers/roles keep their single-match and no-match examples.
-- name: ServersInsertEngineeringEssentialsPlugin :one
INSERT INTO plugins (organization_id, project_id, name, slug)
VALUES (@org_id::text, @project_id::uuid, 'Engineering Essentials', 'engineering-essentials')
RETURNING id;

-- name: ServersInsertEngineeringEssentialsAssignment :exec
INSERT INTO plugin_assignments (plugin_id, organization_id, principal_urn)
SELECT @plugin_id::uuid, @org_id::text, 'role:organization:' || id
FROM organization_roles
WHERE organization_id = @org_id::text AND workos_slug = 'engineer';

-- name: ServersInsertEngineeringEssentialsServers :execrows
INSERT INTO plugin_servers (plugin_id, mcp_server_id, display_name)
SELECT @plugin_id::uuid, id, name FROM mcp_servers
WHERE project_id = @project_id::uuid AND slug = 'github';

-- Leave instructions NULL so Settings starts with the editable built-in
-- instructions, matching the gateway's initialize and server/discover text.
-- Remote MCP identity modes: Linear uses per-user OAuth through a
-- project-scoped CIMD client, Slack carries one inert shared Service Account
-- credential, and GitHub intentionally has no upstream identity. The demo
-- values are display fixtures only and cannot authenticate to any service.
-- name: ServersInsertLinearRemoteSessionIssuer :exec
INSERT INTO remote_session_issuers
  (id, project_id, organization_id, slug, issuer, authorization_endpoint,
   token_endpoint, jwks_uri, scopes_supported, grant_types_supported,
   response_types_supported, token_endpoint_auth_methods_supported,
   code_challenge_methods_supported, client_id_metadata_document_supported,
   name, scope_override)
VALUES
  (@id::uuid, @project_id::uuid, @org_id::text,
   'example-workspace-identity', 'https://identity.example.com',
   'https://identity.example.com/oauth/authorize',
   'https://identity.example.com/oauth/token',
   'https://identity.example.com/.well-known/jwks.json',
   ARRAY['read', 'write'], ARRAY['authorization_code', 'refresh_token'],
   ARRAY['code'], ARRAY['none'], ARRAY['S256'], TRUE,
   -- An issuer-wide scope override, shown on the provider's page.
   'Example Workspace Identity', ARRAY['read']);

-- name: ServersInsertLinearRemoteSessionClient :exec
INSERT INTO remote_session_clients
  (id, project_id, organization_id, remote_session_issuer_id, client_id,
   client_id_metadata_uri, client_id_issued_at, token_endpoint_auth_method,
   scope)
VALUES
  (@id::uuid, @project_id::uuid, @org_id::text,
   @remote_session_issuer_id::uuid,
   @client_id::text,
   @client_id::text, clock_timestamp(), 'none',
   -- No scope of its own: the resource pin decides once discovery is on.
   NULL);

-- name: ServersInsertRemoteSessionClientIssuerBinding :exec
INSERT INTO remote_session_client_user_session_issuers
  (remote_session_client_id, user_session_issuer_id)
VALUES
  (@remote_session_client_id::uuid,
   @user_session_issuer_id::uuid);

-- mcp_servers.remote_session_issuer_id is denormalized from the live client
-- bindings, and the app recomputes it after the commit that writes them
-- (ResyncMCPServerRemoteSessionIssuers). Seeding the binding in SQL never
-- goes through that path, so stamp what the resync would derive: exactly one
-- remote issuer is bound to this server's user session issuer (the
-- attachment fixture's client below sits under the same one). Left NULL,
-- upstream token routing fails closed and Linear reads as having no identity.
-- name: ServersStampLinearRemoteSessionIssuer :exec
UPDATE mcp_servers
SET remote_session_issuer_id = @remote_session_issuer_id::uuid
WHERE id = @mcp_server_id::uuid
  AND project_id = @project_id::uuid;

-- One server's upstream OAuth client association. The app records it when
-- User Identity is configured on the server, or when the server is the only
-- consumer of its user session issuer; the seed writes it directly.
-- name: ServersInsertServerClientAssociation :exec
INSERT INTO mcp_server_remote_session_clients
  (id, project_id, organization_id, mcp_server_id, remote_session_client_id,
   client_attachment_scope)
SELECT @id::uuid, @project_id::uuid, @org_id::text, @mcp_server_id::uuid,
       c.id, c.attachment_scope
FROM remote_session_clients AS c
WHERE c.id = @remote_session_client_id::uuid;

-- One seeded credential's choice for one server, as issuance records it: the
-- client the credential uses there, or no-auth when the client is omitted.
-- no_auth follows the parameter, so a client id that resolves to no client
-- fails the no-auth check instead of being recorded as no-auth. The projects
-- delete cascades these rows.
-- name: ServersInsertUpstreamClientChoice :exec
INSERT INTO upstream_client_selections
  (id, project_id, organization_id, credential_kind, credential_id,
   mcp_server_id, remote_session_client_id, client_attachment_scope, no_auth,
   source)
SELECT @id::uuid, @project_id::uuid, @org_id::text, @credential_kind::text,
       @credential_id::uuid, @mcp_server_id::uuid, c.id, c.attachment_scope,
       sqlc.narg(remote_session_client_id)::uuid IS NULL, @source::text
FROM (SELECT 1) AS one
LEFT JOIN remote_session_clients AS c
  ON c.id = sqlc.narg(remote_session_client_id)::uuid;

-- Linear's protected resource, read recently, with a scope pin for the server's scope panel.
-- name: ServersUpsertLinearProtectedResource :exec
INSERT INTO remote_protected_resources
  (id, project_id, organization_id, resource_identifier, metadata_url,
   authorization_servers, scopes_supported, scope_override, metadata_fetched_at)
VALUES
  (@id::uuid, @project_id::uuid, @org_id::text,
   'https://mcp.linear.app/mcp',
   'https://mcp.linear.app/.well-known/oauth-protected-resource/mcp',
   ARRAY['https://identity.example.com'], ARRAY['read', 'write'],
   ARRAY['read'], now() - interval '2 hours')
ON CONFLICT (project_id, resource_identifier) WHERE deleted IS FALSE DO UPDATE
SET metadata_url = EXCLUDED.metadata_url,
    authorization_servers = EXCLUDED.authorization_servers,
    scopes_supported = EXCLUDED.scopes_supported,
    scope_override = EXCLUDED.scope_override,
    metadata_fetched_at = EXCLUDED.metadata_fetched_at,
    updated_at = clock_timestamp();

-- name: ServersInsertSlackIdentityHeader :exec
INSERT INTO remote_mcp_server_headers
  (id, remote_mcp_server_id, name, description, is_required, is_secret, value)
VALUES
  (@id::uuid,
   @remote_mcp_server_id::uuid, 'Authorization',
   'Inert demo Service Account credential', TRUE, FALSE,
   'Bearer DEMO-NONFUNCTIONAL-TOKEN');

-- name: ServersInsertMetaMCPServer :exec
INSERT INTO meta_mcp_servers (id, organization_id, project_id, name,
                              user_session_issuer_id) VALUES
  (@id::uuid, @org_id::text, @project_id::uuid, 'Acme Agent Gateway',
   @user_session_issuer_id::uuid);

-- sort_order is the order agents see members in list_servers.
-- name: ServersInsertMetaMCPServerMembers :exec
INSERT INTO meta_mcp_server_members (id, project_id, meta_mcp_server_id,
                                     mcp_server_id, sort_order) VALUES
  (@support_member_id::uuid, @project_id::uuid,
   @meta_id::uuid, @support_server_id::uuid, 0),
  (@ops_member_id::uuid, @project_id::uuid,
   @meta_id::uuid, @ops_server_id::uuid, 1),
  (@linear_member_id::uuid, @project_id::uuid,
   @meta_id::uuid, @linear_server_id::uuid, 2),
  (@slack_member_id::uuid, @project_id::uuid,
   @meta_id::uuid, @slack_server_id::uuid, 3);

-- GitHub was a member until three days ago. Its dispatches are still in
-- ClickHouse (gwgone rows), so the Activity section can show that a removed
-- member drops out of Calls by member while the gateway totals keep them.
-- name: ServersInsertRemovedGitHubMember :exec
INSERT INTO meta_mcp_server_members (id, project_id, meta_mcp_server_id,
                                     mcp_server_id, sort_order, deleted_at) VALUES
  (@id::uuid, @project_id::uuid,
   @meta_id::uuid, @github_server_id::uuid, 4,
   now() - interval '3 days');

-- Endpoint slugs on the platform domain are globally unique and org-slug
-- prefixed, so they are built from the tenant's OrgSlug.
-- name: ServersInsertGatewayEndpoints :exec
INSERT INTO mcp_endpoints (id, project_id, meta_mcp_server_id, mcp_server_id, slug) VALUES
  (@gateway_endpoint_id::uuid, @project_id::uuid,
   @meta_id::uuid, NULL, @gateway_slug::text),
  (@linear_endpoint_id::uuid, @project_id::uuid, NULL,
   @linear_server_id::uuid, @linear_slug::text),
  (@slack_endpoint_id::uuid, @project_id::uuid, NULL,
   @slack_server_id::uuid, @slack_slug::text);

-- Tunnel example, deliberately disconnected: its activity history must not
-- fabricate a live customer agent in the shared demo environment. The key
-- hash comes from the tenant's name seed, so it stays globally unique and
-- matches no issued key. ClickHouse metric rows reference these ids
-- (ch_05_tunnel_metrics.go).
-- name: ServersInsertPrivateInventoryTunnel :exec
INSERT INTO tunneled_mcp_servers (id, project_id, name, key_hash, key_prefix,
                                  status, agent_version, last_seen_at) VALUES
  (@id::uuid, @project_id::uuid, 'Private inventory tunnel',
   md5(@key_seed::text), 'demo_inert', 'active', '0.2.0', NULL);

-- name: ServersInsertPrivateInventoryMCPServer :exec
INSERT INTO mcp_servers (id, project_id, name, slug, tunneled_mcp_server_id,
                         visibility) VALUES
  (@id::uuid, @project_id::uuid, 'Private inventory', @slug::text,
   @tunnel_id::uuid, 'private');

-- name: ServersInsertPrivateInventoryEndpoint :exec
INSERT INTO mcp_endpoints (id, project_id, mcp_server_id, slug) VALUES
  (@id::uuid, @project_id::uuid, @mcp_server_id::uuid, @slug::text);

-- Enterprise Managed Authorization (EMA). Acme Identity is a fictional
-- identity provider for two fictional services behind workforce servers;
-- the workforce sign-in deliberately does not trust it, so demo sign-in is
-- unaffected. Neither service has accepted a provider yet: a seeded
-- acceptance needs a trusted issuer session, which the hourly delegation
-- cleanup erases unless an organization sign-in trusts the issuer. Acme
-- Wiki carries a saved provider order. Speakeasy's own issuer is not seeded:
-- it needs a KMS signing key. Reserved example hosts and invalid ciphertext
-- keep every row inert. These servers are added after role distribution, so
-- plugins skip them.
-- name: ServersInsertEMAIdentityProvider :exec
INSERT INTO remote_session_issuers
  (id, project_id, organization_id, slug, issuer, authorization_endpoint,
   token_endpoint, jwks_uri, scopes_supported, grant_types_supported,
   response_types_supported, token_endpoint_auth_methods_supported,
   code_challenge_methods_supported, oidc, name)
VALUES
  (@id::uuid, NULL, @org_id::text, 'acme-identity',
   'https://login.acme-identity.example',
   'https://login.acme-identity.example/oauth2/v1/authorize',
   'https://login.acme-identity.example/oauth2/v1/token',
   'https://login.acme-identity.example/oauth2/v1/keys',
   ARRAY['openid', 'profile', 'email'],
   ARRAY['authorization_code', 'refresh_token'],
   ARRAY['code'], ARRAY['client_secret_basic'], ARRAY['S256'], TRUE,
   'Acme Identity');

-- The workforce sign-in is left as it is: pointing it at this fictional
-- provider would send interactive sign-in for every server on it to a
-- provider that doesn't exist. The Services pages still show these vendors.
-- name: ServersInsertEMAIdentityProviderClient :exec
INSERT INTO remote_session_clients
  (id, project_id, organization_id, remote_session_issuer_id, client_id,
   client_secret_encrypted, token_endpoint_auth_method, grant_types, scope)
VALUES
  (@id::uuid, NULL, @org_id::text, @remote_session_issuer_id::uuid,
   'demo-acme-identity-sign-in', 'DEMO-NOT-VALID-CIPHERTEXT',
   'client_secret_basic', ARRAY['authorization_code', 'refresh_token'],
   ARRAY['openid', 'profile', 'email']);

-- name: ServersInsertEMARemoteMCPServers :exec
INSERT INTO remote_mcp_servers (id, project_id, name, slug, transport_type, url) VALUES
  (@tracker_id::uuid, @project_id::uuid, 'Acme Tracker', 'acme-tracker',
   'streamable-http', 'https://mcp.acme-tracker.example/mcp'),
  (@wiki_id::uuid, @project_id::uuid, 'Acme Wiki', 'acme-wiki',
   'streamable-http', 'https://mcp.acme-wiki.example/mcp');

-- name: ServersInsertEMAMCPServers :exec
INSERT INTO mcp_servers (id, project_id, name, slug, toolset_id,
                         remote_mcp_server_id, user_session_issuer_id,
                         visibility) VALUES
  (@tracker_id::uuid, @project_id::uuid, 'Acme Tracker', 'acme-tracker',
   NULL, @remote_tracker_id::uuid, @issuer_workforce_id::uuid, 'disabled'),
  (@wiki_id::uuid, @project_id::uuid, 'Acme Wiki', 'acme-wiki',
   NULL, @remote_wiki_id::uuid, @issuer_workforce_id::uuid, 'disabled');

-- Each service's authorization server accepts ID-JAGs from a confidential client.
-- name: ServersInsertEMAVendorIssuers :exec
INSERT INTO remote_session_issuers
  (id, project_id, organization_id, slug, issuer, token_endpoint, name,
   grant_types_supported, authorization_grant_profiles_supported,
   token_endpoint_auth_methods_supported)
VALUES
  (@tracker_id::uuid, @project_id::uuid, @org_id::text,
   'acme-tracker-auth', 'https://auth.acme-tracker.example',
   'https://auth.acme-tracker.example/oauth/token', 'Acme Tracker',
   ARRAY['authorization_code', 'urn:ietf:params:oauth:grant-type:jwt-bearer'],
   ARRAY['urn:ietf:params:oauth:grant-profile:id-jag'],
   ARRAY['client_secret_basic']),
  (@wiki_id::uuid, @project_id::uuid, @org_id::text,
   'acme-wiki-auth', 'https://auth.acme-wiki.example',
   'https://auth.acme-wiki.example/oauth/token', 'Acme Wiki',
   ARRAY['authorization_code', 'urn:ietf:params:oauth:grant-type:jwt-bearer'],
   ARRAY['urn:ietf:params:oauth:grant-profile:id-jag'],
   ARRAY['client_secret_basic']);

-- name: ServersInsertEMAVendorClients :exec
INSERT INTO remote_session_clients
  (id, project_id, organization_id, remote_session_issuer_id, client_id,
   client_secret_encrypted, token_endpoint_auth_method, grant_types, scope)
VALUES
  (@tracker_id::uuid, @project_id::uuid, @org_id::text,
   @tracker_issuer_id::uuid, 'demo-acme-tracker-client',
   'DEMO-NOT-VALID-CIPHERTEXT', 'client_secret_basic',
   ARRAY['authorization_code', 'urn:ietf:params:oauth:grant-type:jwt-bearer'],
   ARRAY['issues:read']),
  (@wiki_id::uuid, @project_id::uuid, @org_id::text,
   @wiki_issuer_id::uuid, 'demo-acme-wiki-client',
   'DEMO-NOT-VALID-CIPHERTEXT', 'client_secret_basic',
   ARRAY['authorization_code', 'urn:ietf:params:oauth:grant-type:jwt-bearer'],
   ARRAY['pages:read']);

-- name: ServersInsertEMAProtectedResources :exec
INSERT INTO remote_protected_resources
  (id, project_id, organization_id, resource_identifier, metadata_url,
   authorization_servers, scopes_supported, metadata_fetched_at)
VALUES
  (@tracker_id::uuid, @project_id::uuid, @org_id::text,
   'https://mcp.acme-tracker.example/mcp',
   'https://mcp.acme-tracker.example/.well-known/oauth-protected-resource/mcp',
   ARRAY['https://auth.acme-tracker.example'], ARRAY['issues:read'],
   now() - interval '2 hours'),
  (@wiki_id::uuid, @project_id::uuid, @org_id::text,
   'https://mcp.acme-wiki.example/mcp',
   'https://mcp.acme-wiki.example/.well-known/oauth-protected-resource/mcp',
   ARRAY['https://auth.acme-wiki.example'], ARRAY['pages:read'],
   now() - interval '2 hours');

-- name: ServersInsertEMABindings :exec
INSERT INTO remote_session_ema_bindings
  (id, project_id, organization_id, user_session_issuer_id,
   remote_session_issuer_id, resource, remote_session_client_id, generation,
   state, grant_source, created_at, updated_at)
VALUES
  (@tracker_id::uuid, @project_id::uuid, @org_id::text,
   @issuer_workforce_id::uuid, @tracker_issuer_id::uuid,
   'https://mcp.acme-tracker.example/mcp', @tracker_client_id::uuid, 1,
   'ready', 'administrator_declared',
   now() - interval '6 days', now() - interval '6 days'),
  (@wiki_id::uuid, @project_id::uuid, @org_id::text,
   @issuer_workforce_id::uuid, @wiki_issuer_id::uuid,
   'https://mcp.acme-wiki.example/mcp', @wiki_client_id::uuid, 1,
   'ready', 'administrator_declared',
   now() - interval '1 day', now() - interval '1 day');

-- A saved order for Acme Wiki, so its service page shows "Use default".
-- name: ServersInsertEMAProviderOrder :exec
INSERT INTO remote_session_ema_providers
  (id, organization_id, vendor_issuer, remote_session_issuer_id, position)
VALUES
  (@id::uuid, @org_id::text, 'https://auth.acme-wiki.example',
   @remote_session_issuer_id::uuid, 0);

-- Live connections spread across the MCP servers, not pooled on one issuer.
-- The identity page's connections tab groups by MCP server, and every
-- session hanging off the partner gateway collapsed that view to a single
-- row: the one shape that makes a grouping control look broken. Each
-- person also gets at least one, so no one's tab reads "no connections"
-- while their usage panels show a week of traffic.
-- name: ServersInsertUserSessionClients :exec
INSERT INTO user_session_clients
  (id, project_id, organization_id, user_session_issuer_id, client_id,
   client_secret_hash, client_name, redirect_uris, client_id_issued_at,
   token_endpoint_auth_method)
VALUES
  (@linear_client_id::uuid, @project_id::uuid, @org_id::text,
   @linear_issuer_id::uuid, 'gram_demo_client_linear', NULL,
   'Claude Code', ARRAY['http://127.0.0.1:41293/callback'],
   now() - interval '10 days', 'none'),
  (@slack_client_id::uuid, @project_id::uuid, @org_id::text,
   @slack_issuer_id::uuid, 'gram_demo_client_slack', NULL,
   'Cursor', ARRAY['http://127.0.0.1:41294/callback'],
   now() - interval '8 days', 'none'),
  (@gateway_client_id::uuid, @project_id::uuid, @org_id::text,
   @gateway_issuer_id::uuid, 'gram_demo_client_gateway', NULL,
   'Claude Desktop', ARRAY['http://127.0.0.1:41295/callback'],
   now() - interval '13 days', 'none');

-- One live user session. Offsets are interval strings ('11 days',
-- '-40 minutes') applied to now(), as the old VALUES list did inline.
-- name: ServersInsertUserSession :exec
INSERT INTO user_sessions
  (id, project_id, organization_id, user_session_issuer_id, user_session_client_id,
   subject_urn, jti, refresh_token_hash, refresh_expires_at, expires_at,
   last_used_at, created_at)
VALUES
  (@id::uuid, @project_id::uuid, @org_id::text,
   @user_session_issuer_id::uuid, @user_session_client_id::uuid,
   'user:' || @user_id::text, @jti::text,
   @refresh_token_hash::text,
   now() + (@refresh_expires_in::text)::interval, now() + (@expires_in::text)::interval,
   now() - (@last_used_ago::text)::interval, now() - (@created_ago::text)::interval);

-- Inert upstream account, owned by the human on Linear session 6. Both
-- active release agents share that human owner. The explicit client link
-- makes this exact session reachable from the requesting session's issuer.
-- The client sits under Linear's own remote identity provider: every client
-- bound to one user session issuer must share a remote issuer, or the
-- mcp_servers.remote_session_issuer_id derivation above would come out NULL.
-- Reserved example endpoints, an invalid ciphertext and no refresh token
-- prevent this display fixture from becoming a usable upstream credential.
-- name: ServersInsertAttachmentClient :exec
INSERT INTO remote_session_clients
  (id, project_id, organization_id, remote_session_issuer_id, client_id,
   token_endpoint_auth_method, scope, legacy_callback_url)
VALUES (@id::uuid, @project_id::uuid, @org_id::text,
        @remote_session_issuer_id::uuid,
        (@id::uuid)::text, 'none',
        -- No scope of its own: the resource pin decides once discovery is on.
        NULL, TRUE);

-- name: ServersInsertAttachmentSession :exec
INSERT INTO remote_sessions
  (id, subject_urn, user_session_issuer_id, remote_session_client_id,
   access_token_encrypted, access_expires_at, auto_refresh, scopes,
   upstream_display_name, created_at, last_used_at)
VALUES (@id::uuid,
        'user:' || @user_id::text, @user_session_issuer_id::uuid,
        @remote_session_client_id::uuid,
        'DEMO-NOT-VALID-CIPHERTEXT', now() + interval '7 days', false,
        ARRAY['releases:read'], 'Fictional release account',
        now() - interval '3 days', now() - interval '40 minutes');

-- Upstream sessions on Linear's CIMD client, one per last-validation outcome
-- plus one never validated, so the admin server health page shows every
-- validation state. Inert like the session above: an invalid ciphertext and
-- no refresh token. grant_generation above 1 records re-authorizations.
-- An empty status is the never-validated session (NULL status and
-- last_validated_at).
-- name: ServersInsertValidatedSession :exec
INSERT INTO remote_sessions
  (id, subject_urn, user_session_issuer_id, remote_session_client_id,
   access_token_encrypted, access_expires_at, auto_refresh, scopes,
   grant_generation, last_validated_at, validation_status, created_at, last_used_at)
VALUES (@id::uuid,
        'user:' || @user_id::text, @user_session_issuer_id::uuid,
        @remote_session_client_id::uuid,
        'DEMO-NOT-VALID-CIPHERTEXT', now() + interval '7 days', false,
        ARRAY['read'], @grant_generation::bigint,
        CASE WHEN @status::text <> '' THEN now() - make_interval(hours => @hours_ago::int) END,
        NULLIF(@status::text, ''), now() - make_interval(days => @days_ago::int),
        now() - make_interval(hours => @hours_ago::int));

-- Many principals can reference one exact human-owned session. Reconnecting
-- must never silently replace these references with a different session.
-- name: ServersInsertAttachmentBinding :exec
INSERT INTO principal_remote_session_bindings
  (id, project_id, organization_id, principal_id, user_session_issuer_id,
   remote_session_client_id, remote_session_id, grant_generation, attached_by_subject_id)
SELECT @id::uuid, @project_id::uuid, @org_id::text,
       @principal_id::uuid,
       @user_session_issuer_id::uuid,
       @remote_session_client_id::uuid,
       rs.id, rs.grant_generation, 'user:' || @attached_by_user_id::text
FROM remote_sessions rs
WHERE rs.id = @remote_session_id::uuid;

-- Display-only policy fixtures for the credential wizard. Exact project and
-- remote MCP resource IDs avoid wildcard delegation; owner/caller access is
-- still intersected at runtime. No API key or usable upstream secret is seeded.
-- name: ServersInsertAgentConnectGrant :exec
INSERT INTO principal_grants
  (id, organization_id, principal_urn, scope, selectors)
VALUES (@id::uuid, @org_id::text,
        'agent:' || (@agent_id::uuid)::text,
        'mcp:connect', jsonb_build_object(
          'resource_kind', 'mcp',
          'resource_id', (@mcp_server_id::uuid)::text,
          'project_id', (@project_id::uuid)::text));

-- Killswitches. Six stable aggregates exercise every customer status and
-- the principal-first overlap/history stories. Direct SQL mirrors lifecycle
-- transactions: immutable complete snapshots, superseded predecessors,
-- completed replay receipts, and current_version pointing at the newest
-- version. All resource keys are fronting server IDs.
-- name: ServersInsertKillswitchPrescriptions :exec
INSERT INTO killswitch_prescriptions
  (id, organization_id, definition_key, principal_kind, principal_key,
   resource_kind, current_version, created_at, updated_at)
VALUES
  (@active_selected_id::uuid, @org_id::text,
   'mcp_tool_execution', 'user', @user_1::text, 'mcp_server', 1,
   now() - interval '8 days', now() - interval '8 days'),
  (@active_all_id::uuid, @org_id::text,
   'mcp_tool_execution', 'user', @user_1::text, 'mcp_server', 1,
   now() - interval '7 days', now() - interval '7 days'),
  (@scheduled_all_id::uuid, @org_id::text,
   'mcp_tool_execution', 'user', @user_3::text, 'mcp_server', 1,
   now() - interval '1 hour', now() - interval '1 hour'),
  (@changed_id::uuid, @org_id::text,
   'mcp_tool_execution', 'user', @user_2::text, 'mcp_server', 2,
   now() - interval '6 days', now() - interval '2 days'),
  (@lifted_id::uuid, @org_id::text,
   'mcp_tool_execution', 'user', @user_4::text, 'mcp_server', 2,
   now() - interval '5 days', now() - interval '12 hours'),
  (@expired_id::uuid, @org_id::text,
   'mcp_tool_execution', 'user', @user_5::text, 'mcp_server', 1,
   now() - interval '4 days', now() - interval '4 days');

-- name: ServersInsertKillswitchPrescriptionVersions :exec
INSERT INTO killswitch_prescription_versions
  (organization_id, prescription_id, version, state, resource_scope, starts_at,
   expires_at, activated_at, superseded_at, internal_note, external_note,
   created_at)
VALUES
  (@org_id::text, @active_selected_id::uuid, 1,
   'active', 'selected', NULL, NULL, now() - interval '8 days', NULL,
   E'Incident containment for the fictional support workflow.\nReview after the demo investigation.',
   E'MCP tool calls are paused for this member.\n<script>alert("demo")</script>\n**This is plain text, not Markdown.**',
   now() - interval '8 days'),
  (@org_id::text, @active_all_id::uuid, 1,
   'active', 'all', NULL, NULL, now() - interval '7 days', NULL,
   'Overlapping all-server containment for the fictional support incident.',
   'MCP tool calls are paused across all current and future servers.',
   now() - interval '7 days'),
  (@org_id::text, @scheduled_all_id::uuid, 1,
   'active', 'all', now() + interval '2 days', now() + interval '6 days',
   now() - interval '1 hour', NULL,
   'Scheduled maintenance window for the fictional platform team.',
   'MCP tool calls will be paused during scheduled maintenance.',
   now() - interval '1 hour'),
  (@org_id::text, @changed_id::uuid, 1,
   'active', 'selected', NULL, NULL, now() - interval '6 days',
   now() - interval '2 days',
   'Initial three-server scope for the fictional reconciliation review.',
   'MCP tool calls are paused while access is reviewed.',
   now() - interval '6 days'),
  (@org_id::text, @changed_id::uuid, 2,
   'active', 'selected', NULL, NULL, now() - interval '6 days', NULL,
   'Narrowed after review; only the support server remains in scope.',
   'MCP tool calls remain paused for the support server.',
   now() - interval '2 days'),
  (@org_id::text, @lifted_id::uuid, 1,
   'active', 'selected', NULL, NULL, now() - interval '5 days',
   now() - interval '12 hours',
   'Temporary pause for a fictional credential review.',
   'MCP tool calls are paused during the credential review.',
   now() - interval '5 days'),
  (@org_id::text, @lifted_id::uuid, 2,
   'inactive', 'selected', NULL, NULL, now() - interval '5 days', NULL,
   'Temporary pause for a fictional credential review.',
   'MCP tool calls are paused during the credential review.',
   now() - interval '12 hours'),
  (@org_id::text, @expired_id::uuid, 1,
   'active', 'selected', NULL, now() - interval '1 day',
   now() - interval '4 days', NULL,
   'Bounded pause for a completed fictional operations exercise.',
   'MCP tool calls were paused for the operations exercise.',
   now() - interval '4 days');

-- name: ServersInsertKillswitchVersionResources :exec
INSERT INTO killswitch_prescription_version_resources
  (organization_id, prescription_id, version, resource_key)
VALUES
  (@org_id::text, @active_selected_id::uuid, 1, (@support_server_id::uuid)::text),
  (@org_id::text, @changed_id::uuid, 1, (@support_server_id::uuid)::text),
  (@org_id::text, @changed_id::uuid, 1, (@ops_server_id::uuid)::text),
  (@org_id::text, @changed_id::uuid, 1, (@linear_server_id::uuid)::text),
  (@org_id::text, @changed_id::uuid, 2, (@support_server_id::uuid)::text),
  (@org_id::text, @lifted_id::uuid, 1, (@slack_server_id::uuid)::text),
  (@org_id::text, @lifted_id::uuid, 2, (@slack_server_id::uuid)::text),
  (@org_id::text, @expired_id::uuid, 1, (@ops_server_id::uuid)::text);

-- name: ServersInsertKillswitchExpiryEvent :exec
INSERT INTO killswitch_expiry_events
  (organization_id, prescription_id, version, recorded_at)
VALUES (@org_id::text, @prescription_id::uuid, 1,
        now() - interval '23 hours');

-- One completed killswitch operation receipt. expires_at and created_at are
-- interval strings applied to now(); updated_at equals created_at.
-- name: ServersInsertKillswitchOperation :exec
INSERT INTO killswitch_operations
  (organization_id, operation_id, actor_user_id, operation, request_hash,
   status, response, expires_at, created_at, updated_at)
VALUES
  (@org_id::text, @operation_id::uuid,
   @actor_user_id::text, @operation::text, 'sha256:' || repeat(@hash_digit::text, 64), 'completed',
   jsonb_build_object('response_version', 'killswitch-operation-response-v1',
     'prescription_id', (@prescription_id::uuid)::text,
     'prescription_version', @prescription_version::int, 'state', @state::text),
   now() + (@expires_in::text)::interval, now() - (@created_ago::text)::interval,
   now() - (@created_ago::text)::interval);

-- Typed auth clients, as the upstream OAuth migration records them: Linear's
-- personal client over its OAuth registration, and one static credential
-- shared by GitHub and Linear, so the server settings show a shared client
-- and a server offering two. Slack is left undecided about upstream
-- authentication.
-- The projects delete cascades the clients, their headers and associations.
-- name: ServersInsertAuthClient :exec
INSERT INTO mcp_auth_clients
  (id, organization_id, project_id, client_type, source, name,
   remote_session_client_id, client_attachment_scope)
SELECT @id::uuid, @org_id::text, @project_id::uuid, @client_type::text,
       @source::text, @name::text, c.id, c.attachment_scope
FROM (SELECT 1) AS one
LEFT JOIN remote_session_clients AS c
  ON c.id = sqlc.narg(remote_session_client_id)::uuid;

-- The value is a placeholder; stored values are never returned.
-- name: ServersInsertAuthClientHeader :exec
INSERT INTO mcp_auth_client_headers
  (id, auth_client_id, organization_id, name, is_secret, value)
VALUES
  (@id::uuid, @auth_client_id::uuid, @org_id::text, @name::text, TRUE,
   @value::text);

-- name: ServersInsertAuthClientForwardedHeader :exec
INSERT INTO mcp_auth_client_headers
  (id, auth_client_id, organization_id, name, is_required, value_from_request_header)
VALUES
  (@id::uuid, @auth_client_id::uuid, @org_id::text, @name::text, TRUE,
   @value_from_request_header::text);

-- name: ServersInsertServerAuthClient :exec
INSERT INTO mcp_server_auth_clients
  (id, project_id, organization_id, mcp_server_id, auth_client_id,
   client_attachment_scope, source)
SELECT @id::uuid, @project_id::uuid, @org_id::text, @mcp_server_id::uuid,
       ac.id, ac.attachment_scope, ac.source
FROM mcp_auth_clients AS ac
WHERE ac.id = @auth_client_id::uuid;

-- name: ServersMarkRequiresUpstreamAuth :exec
UPDATE mcp_servers
SET requires_upstream_auth = TRUE
WHERE project_id = @project_id::uuid
  AND id = ANY(@ids::uuid[]);

-- Engineers may use the shared GitHub token; Contractors are blocked from
-- it on every server that offers it. Cleared by id at the top of the seed.
-- name: ServersInsertAuthClientRoleGrants :exec
INSERT INTO principal_grants (id, organization_id, principal_urn, scope, selectors)
SELECT v.id, @org_id::text, 'role:organization:' || r.id, v.scope,
       jsonb_build_object('resource_kind', 'mcp_auth_client',
         'resource_id', (@auth_client_id::uuid)::text)
FROM (VALUES
  (@use_id::uuid, 'engineer', 'mcp:auth'),
  (@block_id::uuid, 'contractors', 'mcp:blocked_auth')
) AS v(id, slug, scope)
JOIN organization_roles r
  ON r.organization_id = @org_id::text AND r.workos_slug = v.slug;

-- An app that signs in to Linear without a browser, so it cannot pick a
-- credential on the consent screen.
-- name: ServersInsertHeadlessClient :exec
INSERT INTO user_session_clients
  (id, project_id, organization_id, user_session_issuer_id, client_id,
   client_secret_hash, client_name, redirect_uris, client_id_issued_at,
   token_endpoint_auth_method)
VALUES
  (@id::uuid, @project_id::uuid, @org_id::text, @issuer_id::uuid,
   'gram_demo_client_nightly_report', NULL, 'Nightly report job',
   ARRAY[]::text[], now() - interval '6 days', 'none');

-- The credential the headless app's sessions use on one server.
-- name: ServersInsertIssuanceProfile :exec
INSERT INTO issuance_selection_profiles
  (id, organization_id, project_id, user_session_issuer_id,
   user_session_client_id, name)
VALUES
  (@id::uuid, @org_id::text, @project_id::uuid, @issuer_id::uuid,
   @client_id::uuid, @name::text);

-- name: ServersInsertIssuanceProfileChoice :exec
INSERT INTO upstream_client_selections
  (id, project_id, organization_id, credential_kind, credential_id,
   mcp_server_id, remote_session_client_id, client_attachment_scope, no_auth,
   auth_client_id, auth_client_attachment_scope, source)
SELECT @id::uuid, @project_id::uuid, @org_id::text, 'issuance_profile',
       @profile_id::uuid, @mcp_server_id::uuid, ac.remote_session_client_id,
       NULL, FALSE, ac.id, ac.attachment_scope, 'issuance'
FROM mcp_auth_clients AS ac
WHERE ac.id = @auth_client_id::uuid AND ac.organization_id = @org_id::text;

-- name: ServersAttachAssistantServer :exec
INSERT INTO assistant_mcp_servers (id, assistant_id, mcp_server_id, project_id)
VALUES (@id::uuid, @assistant_id::uuid, @mcp_server_id::uuid, @project_id::uuid);

-- The upstream credential an administrator chose for an assistant's turns
-- under one root trigger and execution context, as version 1 of its profile.
-- name: ServersInsertExecutionProfile :exec
INSERT INTO execution_selection_profiles
  (id, organization_id, project_id, assistant_id, trigger_instance_id, execution_context)
VALUES
  (@id::uuid, @org_id::text, @project_id::uuid, @assistant_id::uuid,
   @trigger_instance_id::uuid, @execution_context::text);

-- name: ServersInsertExecutionProfileVersion :exec
INSERT INTO execution_selection_profile_versions
  (id, profile_id, organization_id, project_id, version, source)
VALUES (@id::uuid, @profile_id::uuid, @org_id::text, @project_id::uuid, 1, 'admin');

-- name: ServersInsertExecutionProfileChoice :exec
INSERT INTO upstream_client_selections
  (id, project_id, organization_id, credential_kind, credential_id,
   mcp_server_id, remote_session_client_id, client_attachment_scope, no_auth,
   auth_client_id, auth_client_attachment_scope, source)
SELECT @id::uuid, @project_id::uuid, @org_id::text, 'execution_profile_version',
       @version_id::uuid, @mcp_server_id::uuid, ac.remote_session_client_id,
       NULL, FALSE, ac.id, ac.attachment_scope, 'admin'
FROM mcp_auth_clients AS ac
WHERE ac.id = @auth_client_id::uuid AND ac.organization_id = @org_id::text;
