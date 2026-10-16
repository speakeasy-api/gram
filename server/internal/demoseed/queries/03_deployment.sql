-- name: DeployInsertAsset :exec
INSERT INTO assets (id, project_id, organization_id, name, url, kind, content_type, content_length, sha256)
VALUES (@id, @project_id::uuid, @organization_id::text, 'Acme Internal API', 'file://demo/acme-openapi.yaml',
        'openapiv3', 'application/x-yaml', 24576, @sha256);

-- name: DeployInsertDeployment :exec
INSERT INTO deployments (id, user_id, project_id, organization_id, idempotency_key, created_at)
VALUES (@id, @user_id, @project_id::uuid, @organization_id::text, @idempotency_key,
        now() - interval '11 days');

-- name: DeployInsertDeploymentStatus :exec
INSERT INTO deployment_statuses (deployment_id, status)
VALUES (@deployment_id, 'completed');

-- name: DeployInsertDeploymentLogs :exec
INSERT INTO deployment_logs (event, message, deployment_id, project_id) VALUES
  ('deployment:started', 'Processing 1 OpenAPI document', @deployment_id, @project_id::uuid),
  ('deployment:tools', 'Extracted 8 tools from Acme Internal API', @deployment_id, @project_id::uuid),
  ('deployment:completed', 'Deployment completed successfully', @deployment_id, @project_id::uuid);

-- name: DeployInsertOpenAPIAsset :exec
INSERT INTO deployments_openapiv3_assets (id, deployment_id, asset_id, name, slug)
VALUES (@id, @deployment_id, @asset_id, 'Acme Internal API', 'acme');

-- name: DeployInsertHTTPTool :exec
INSERT INTO http_tool_definitions
  (tool_urn, project_id, deployment_id, openapiv3_document_id, name, summary,
   description, server_env_var, http_method, path, schema_version, schema,
   read_only_hint, destructive_hint)
VALUES (@tool_urn::text, @project_id::uuid, @deployment_id, @openapiv3_document_id::uuid, @name,
        @summary, @description::text, 'ACME_SERVER_URL', @http_method, @path,
        '1.0.0', '{"type":"object","properties":{}}'::jsonb,
        @read_only_hint::boolean, @destructive_hint::boolean);

-- name: DeployInsertExternalOAuthMetadata :exec
INSERT INTO external_oauth_server_metadata
  (id, project_id, slug, metadata, authorization_server_issuer)
VALUES
  (@id, @project_id::uuid, 'acme-oauth-discovery',
   '{"issuer":"https://auth.example.com","authorization_endpoint":"https://auth.example.com/oauth/authorize","token_endpoint":"https://auth.example.com/oauth/token","registration_endpoint":"https://auth.example.com/oauth/register"}'::jsonb,
   NULL);

-- name: DeployInsertToolsets :exec
INSERT INTO toolsets (id, organization_id, project_id, name, slug, description,
                      mcp_slug, mcp_enabled, mcp_is_public, external_oauth_server_id)
VALUES
  (@toolset_1, @organization_id::text, @project_id::uuid, 'Acme Support Tools', 'acme-support-tools',
   'Support workflows: logs, refunds, customer lookups.',
   @support_mcp_slug::text, TRUE, FALSE, NULL),
  (@toolset_2, @organization_id::text, @project_id::uuid, 'Acme Ops', 'acme-ops',
   'Operational checks and deploy tooling.', NULL, TRUE, FALSE, NULL),
  -- The OAuth-protected server, kept separate from the two above: attaching
  -- a session issuer changes what a server's authentication tab and install
  -- instructions say, and the support server is the one a visitor meets
  -- first.
  (@toolset_3, @organization_id::text, @project_id::uuid, 'Acme Partner Gateway', 'acme-partner-gateway',
   'Tools Acme exposes to partner agents, behind OAuth.',
   @partner_mcp_slug::text, TRUE, FALSE, NULL),
  (@toolset_4, @organization_id::text, @project_id::uuid, 'Acme OAuth Discovery', 'acme-oauth-discovery',
   'Demonstrates external OAuth metadata source choices.',
   @discovery_mcp_slug::text, TRUE, TRUE,
   @external_oauth_server_id::uuid);

-- Version = epoch seconds: the server caches toolset contents in Redis
-- keyed by (deployment, toolset, version), and a reseed reusing version 1
-- would keep serving the stale cached payload. A fresh version per run
-- changes the cache key, so every reseed is immediately visible.
-- name: DeployInsertToolsetVersions :exec
INSERT INTO toolset_versions (toolset_id, version, tool_urns, resource_urns) VALUES
  (@toolset_1, extract(epoch FROM now())::bigint, @toolset_1_urns::text[], '{}'),
  (@toolset_2, extract(epoch FROM now())::bigint, @toolset_2_urns::text[], '{}'),
  (@toolset_3, extract(epoch FROM now())::bigint, @toolset_3_urns::text[], '{}'),
  (@toolset_4, extract(epoch FROM now())::bigint, @toolset_4_urns::text[], '{}');

-- name: DeployInsertUserSessionIssuer :exec
INSERT INTO user_session_issuers (id, project_id, organization_id, slug, authn_challenge_mode, session_duration)
VALUES (@id, @project_id::uuid, @organization_id::text, 'acme-partner-gateway', 'interactive', interval '30 days');

-- name: DeployAttachUserSessionIssuer :exec
UPDATE toolsets SET user_session_issuer_id = @user_session_issuer_id::uuid WHERE id = @toolset_id;

-- name: DeployInsertChainingIssuer :exec
INSERT INTO remote_session_issuers
  (id, project_id, organization_id, slug, issuer, token_endpoint, name,
   grant_types_supported, authorization_grant_profiles_supported,
   token_endpoint_auth_methods_supported, metadata_fetched_at)
VALUES
  (@id, @project_id::uuid, @organization_id::text, 'identity-chaining-example',
   'https://authorization.example.com', 'https://authorization.example.com/token',
   'Identity chaining example',
   ARRAY['urn:ietf:params:oauth:grant-type:jwt-bearer'],
   ARRAY['urn:ietf:params:oauth:grant-profile:id-jag'], ARRAY['none'], NULL);

-- name: DeployInsertChainingClient :exec
INSERT INTO remote_session_clients
  (id, project_id, organization_id, remote_session_issuer_id, client_id,
   token_endpoint_auth_method, grant_types, scope, resource_identifier)
VALUES
  (@id, @project_id::uuid, @organization_id::text, @remote_session_issuer_id,
   'demo-resource-client', 'none',
   ARRAY['urn:ietf:params:oauth:grant-type:jwt-bearer'], ARRAY['documents:read'],
   'https://resource.example.com/mcp');

-- name: DeployInsertJamfIssuer :exec
INSERT INTO remote_session_issuers
  (id, project_id, organization_id, slug, issuer, token_endpoint, name,
   grant_types_supported, token_endpoint_auth_methods_supported,
   client_setup_documentation_url, metadata_fetched_at)
VALUES
  (@id, @project_id::uuid, @organization_id::text,
   'jamf-pro', @issuer, @token_endpoint::text, 'Jamf Pro',
   ARRAY['client_credentials'], ARRAY['client_secret_post'],
   'https://learn.jamf.com/r/en-US/jamf-pro-documentation-current/API_Roles_and_Clients',
   NULL);

-- name: DeployInsertJamfClient :exec
INSERT INTO remote_session_clients
  (id, project_id, organization_id, remote_session_issuer_id, client_id,
   client_secret_encrypted, token_endpoint_auth_method, grant_types, scope,
   credential_owner)
VALUES
  (@id, @project_id::uuid, @organization_id::text, @remote_session_issuer_id,
   'demo-jamf-pro-api-client', 'DEMO-NOT-VALID-CIPHERTEXT', 'client_secret_post',
   ARRAY['client_credentials'], NULL, 'self');

-- name: DeployInsertCIMDClient :exec
INSERT INTO user_session_clients
  (id, project_id, organization_id, user_session_issuer_id, client_id, client_name,
   redirect_uris, client_id_issued_at, client_id_metadata_uri,
   client_id_metadata_fetched_at, client_id_metadata_cache_expires_at,
   client_id_metadata_etag, token_endpoint_auth_method, client_jwks_uri)
VALUES
  (@id, @project_id::uuid, @organization_id::text, @user_session_issuer_id::uuid, @cimd_url, 'Partner Reconciliation Agent',
   ARRAY['https://agents.example.com/callback'],
   now() - interval '9 days', @cimd_url, now() - interval '2 hours',
   now() + interval '22 hours', '"demo-etag-v3"',
   'private_key_jwt', 'https://agents.example.com/.well-known/jwks.json');

-- name: DeployInsertUserSessionClient :exec
INSERT INTO user_session_clients
  (id, project_id, organization_id, user_session_issuer_id, client_id,
   client_secret_hash, client_name, redirect_uris, client_id_issued_at,
   token_endpoint_auth_method)
VALUES
  (@id, @project_id::uuid, @organization_id::text, @user_session_issuer_id::uuid, @client_id,
   sqlc.narg(client_secret_hash)::text, @client_name, ARRAY[@redirect_uri::text],
   now() - (@issued_ago::text)::interval, sqlc.narg(token_endpoint_auth_method)::text);

-- name: DeployInsertUserSession :exec
INSERT INTO user_sessions
  (id, project_id, organization_id, user_session_issuer_id, user_session_client_id,
   subject_urn, jti, refresh_token_hash, refresh_expires_at, expires_at,
   last_used_at, created_at)
VALUES
  (@id, @project_id::uuid, @organization_id::text, @user_session_issuer_id::uuid, @user_session_client_id::uuid,
   @subject_urn::text, @jti, @refresh_token_hash::text,
   now() + (@refresh_expires_in::text)::interval, now() + (@expires_in::text)::interval,
   now() - (@last_used_ago::text)::interval, now() - (@created_ago::text)::interval);

-- A display-only managed-agent credential: no signing token, an invalid
-- refresh hash, and an empty delegation prevent usable programmatic access.
-- The issuer/project cascade cleans this row on every reseed.
-- name: DeployInsertManagedAgentSession :exec
INSERT INTO user_sessions
  (id, project_id, organization_id, user_session_issuer_id,
   user_session_client_id, subject_urn, authorizer_user_id, delegated_grants,
   delegated_grants_version, jti, refresh_token_hash, refresh_expires_at,
   expires_at, last_used_at, created_at)
VALUES
  (@id, @project_id::uuid, @organization_id::text,
   @user_session_issuer_id::uuid, @user_session_client_id::uuid,
   @subject_urn::text, @authorizer_user_id::text, '[]'::jsonb, 1,
   @jti, @refresh_token_hash::text,
   now() + interval '7 days', now() - interval '10 minutes',
   now() - interval '20 minutes', now() - interval '3 days');

-- name: DeployInsertOrgWorkloadIssuer :exec
INSERT INTO workload_issuers
  (id, organization_id, project_id, name, description, tags, issuer, jwks_uri,
   allow_wildcard_admission)
VALUES
  (@id, @organization_id::text, NULL, @name, @description::text, @tags::text[], @issuer, @jwks_uri,
   @allow_wildcard_admission);

-- name: DeployInsertWorkloadAdmission :exec
INSERT INTO workload_identity_admissions
  (id, organization_id, project_id, workload_issuer_id, subject, match_kind,
   name, tags)
VALUES
  (@id, @organization_id::text, sqlc.narg(project_id)::uuid, @workload_issuer_id,
   @subject, @match_kind, @name::text, @tags::text[]);

-- name: DeployInsertWorkloadAssignment :exec
INSERT INTO workload_agent_assignments
  (id, organization_id, workload_issuer_id, subject, match_kind, agent_id)
VALUES
  (@id, @organization_id::text, @workload_issuer_id, @subject, @match_kind, @agent_id);

-- name: DeployInsertWorkloadSession :exec
INSERT INTO user_sessions
  (id, project_id, organization_id, user_session_issuer_id,
   user_session_client_id, subject_urn, authorizer_user_id, delegated_grants,
   delegated_grants_version, jti, refresh_token_hash, refresh_expires_at,
   expires_at, last_used_at, created_at)
VALUES
  (@id, @project_id::uuid, @organization_id::text,
   @user_session_issuer_id::uuid, NULL,
   @subject_urn::text, NULL, '[]'::jsonb, 1,
   @jti, @refresh_token_hash::text,
   now() + (@refresh_expires_in::text)::interval, now() - (@expired_ago::text)::interval,
   now() - (@last_used_ago::text)::interval, now() - (@created_ago::text)::interval);

-- name: DeployInsertAssistant :exec
INSERT INTO assistants
  (id, organization_id, project_id, created_by_user_id, name, model, instructions)
VALUES
  (@id, @organization_id::text, @project_id::uuid, @created_by_user_id::text,
   @name, 'anthropic/claude-sonnet-4.6', 'Help summarize project activity.');

-- name: DeployInsertAgent :exec
INSERT INTO agents (id, organization_id, project_id, owner_user_id, name)
VALUES (@id, @organization_id::text, @project_id::uuid, @owner_user_id, 'Identity-bound assistant agent');

-- name: DeployInsertPrincipalGrant :exec
INSERT INTO principal_grants
  (id, organization_id, principal_urn, scope, selectors)
VALUES
  (@id, @organization_id::text, @principal_urn::text, @scope, @selectors::jsonb);

-- name: DeployInsertDashboardTrigger :exec
INSERT INTO trigger_instances
  (id, organization_id, project_id, definition_slug, name, target_kind, target_ref, target_display)
VALUES
  (@id, @organization_id::text, @project_id::uuid, 'dashboard', @name::text || ' dashboard', 'assistant',
   @target_ref, @name::text);

-- Inert display fixture: runtime provisioning uses GRAM_AUTHZ_ISSUER_URL and
-- the existing Speakeasy JWKS endpoint; this example cannot sign or run work.
-- name: DeployInsertAssistantWorkloadIssuer :exec
INSERT INTO workload_issuers
  (id, organization_id, project_id, name, issuer, jwks_uri, allow_wildcard_admission)
VALUES (@id, @organization_id::text, @project_id::uuid,
        @name, 'https://platform.example.invalid',
        'https://platform.example.invalid/.well-known/jwks.json', FALSE);

-- name: DeployInsertAssistantAgentBinding :exec
INSERT INTO assistant_agent_bindings
  (id, organization_id, project_id, project_ref_organization_id, project_ref_id,
   original_assistant_id, assistant_ref_organization_id, assistant_ref_project_id, assistant_id,
   original_agent_id, agent_ref_organization_id, agent_ref_project_id, agent_id, generation)
VALUES (@id, @organization_id::text, @project_id::uuid, @organization_id::text, @project_id::uuid,
        @assistant_id::uuid, @organization_id::text, @project_id::uuid, @assistant_id::uuid,
        @agent_id::uuid, @organization_id::text, @project_id::uuid, @agent_id::uuid, 1);

-- Bind the dashboard root trigger, never a continuation wake.
-- name: DeployInsertTriggerWorkloadBinding :exec
INSERT INTO trigger_workload_bindings
  (id, organization_id, project_id, project_ref_organization_id, project_ref_id,
   original_trigger_id, trigger_ref_organization_id, trigger_ref_project_id, trigger_id,
   original_assistant_binding_id, assistant_binding_ref_organization_id,
   assistant_binding_ref_project_id, assistant_binding_id, assistant_binding_generation,
   original_workload_issuer_id, workload_issuer_ref_organization_id, workload_issuer_ref_project_id,
   workload_issuer_id, subject, generation)
SELECT @id::uuid, @organization_id::text, @project_id::uuid, @organization_id::text, @project_id::uuid,
       ti.id, @organization_id::text, @project_id::uuid, ti.id,
       @assistant_binding_id::uuid, @organization_id::text, @project_id::uuid,
       @assistant_binding_id::uuid, 1,
       @workload_issuer_id::uuid, @organization_id::text, @project_id::uuid,
       @workload_issuer_id::uuid, 'assistant-trigger:' || ti.id::text, 1
FROM trigger_instances ti
WHERE ti.organization_id = @organization_id::text AND ti.project_id = @project_id::uuid
  AND ti.definition_slug = 'dashboard' AND ti.target_kind = 'assistant'
  AND ti.target_ref = @assistant_id::text
  AND ti.status = 'active' AND ti.deleted IS FALSE;

-- name: DeployInsertAssistantAdmission :exec
INSERT INTO workload_identity_admissions
  (id, organization_id, project_id, workload_issuer_id, subject, match_kind, name)
SELECT @id::uuid, @organization_id::text, @project_id::uuid,
       twb.workload_issuer_id, twb.subject, 'exact', 'Identity-bound assistant dashboard'
FROM trigger_workload_bindings twb
WHERE twb.organization_id = @organization_id::text AND twb.project_id = @project_id::uuid;

-- name: DeployInsertAssistantAssignment :exec
INSERT INTO workload_agent_assignments
  (id, organization_id, workload_issuer_id, subject, match_kind, agent_id)
SELECT @id::uuid, @organization_id::text,
       twb.workload_issuer_id, twb.subject, 'exact', @agent_id::uuid
FROM trigger_workload_bindings twb
WHERE twb.organization_id = @organization_id::text AND twb.project_id = @project_id::uuid;
