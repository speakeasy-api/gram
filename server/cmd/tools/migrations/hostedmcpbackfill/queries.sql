-- name: ListCandidateToolsets :many
SELECT id, project_id
FROM toolsets
WHERE mcp_slug IS NOT NULL
  AND deleted IS FALSE
  AND (sqlc.narg(project_id)::uuid IS NULL OR project_id = sqlc.narg(project_id)::uuid)
  AND id > @after_id::uuid
ORDER BY id
LIMIT @page_size::int;

-- name: LockToolsetBackfill :exec
SELECT pg_advisory_xact_lock(hashtextextended('hosted-mcp-backfill:' || @toolset_id::text, 0));

-- name: LockToolsetRow :one
SELECT id, project_id, organization_id, name, mcp_slug, mcp_is_public, mcp_enabled,
       custom_domain_id, user_session_issuer_id, tool_variations_group_id, deleted
FROM toolsets
WHERE id = @id AND project_id = @project_id
FOR UPDATE;

-- name: GetCustomDomainForBackfill :one
-- FOR SHARE serializes against DeleteDomain's FOR UPDATE on the same row.
SELECT id, deleted, deleted_at
FROM custom_domains
WHERE id = @id AND organization_id = @organization_id
FOR SHARE;

-- name: GetWrapperByID :one
SELECT id, toolset_id, deleted
FROM mcp_servers
WHERE id = @id AND project_id = @project_id;

-- name: PlatformSlugOwnedByOtherToolset :one
-- Addresses are a global namespace, so this probe is deliberately unscoped.
SELECT EXISTS (
  SELECT 1 FROM toolsets
  WHERE mcp_slug = @slug AND custom_domain_id IS NULL AND deleted IS FALSE AND id <> @toolset_id
);

-- name: WrapperSlugTaken :one
SELECT EXISTS (
  SELECT 1 FROM mcp_servers
  WHERE project_id = @project_id AND slug = @slug AND deleted IS FALSE AND id <> @id
);

-- name: InsertWrapper :exec
INSERT INTO mcp_servers (
  id, project_id, name, slug, user_session_issuer_id, toolset_id, tool_variations_group_id, visibility
) VALUES (
  @id, @project_id, @name, @slug, @user_session_issuer_id, @toolset_id, @tool_variations_group_id, @visibility
);

-- name: ReconcileWrapper :exec
UPDATE mcp_servers
SET user_session_issuer_id = @user_session_issuer_id,
    tool_variations_group_id = @tool_variations_group_id,
    visibility = @visibility,
    updated_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE;

-- name: ClearRootEndpoints :many
UPDATE mcp_endpoints
SET is_domain_root = NULL, updated_at = clock_timestamp()
WHERE mcp_server_id = @mcp_server_id AND project_id = @project_id
  AND is_domain_root IS TRUE AND deleted IS FALSE
RETURNING custom_domain_id;

-- name: ListWrapperEndpoints :many
SELECT id, custom_domain_id, slug, deleted
FROM mcp_endpoints
WHERE mcp_server_id = @mcp_server_id AND project_id = @project_id;

-- name: GetEndpointByID :one
SELECT id, mcp_server_id, custom_domain_id, slug, deleted
FROM mcp_endpoints
WHERE id = @id AND project_id = @project_id;

-- name: EndpointAddressTaken :one
-- Addresses are a global namespace, so this probe is deliberately unscoped.
SELECT EXISTS (
  SELECT 1 FROM mcp_endpoints
  WHERE slug = @slug
    AND custom_domain_id IS NOT DISTINCT FROM sqlc.narg(custom_domain_id)::uuid
    AND deleted IS FALSE
    AND mcp_server_id IS DISTINCT FROM @mcp_server_id::uuid
);

-- name: InsertEndpoint :exec
INSERT INTO mcp_endpoints (id, project_id, custom_domain_id, mcp_server_id, slug, deleted_at)
VALUES (@id, @project_id, @custom_domain_id, @mcp_server_id, @slug, @deleted_at);

-- name: MoveEndpointAddress :exec
-- The root marker only survives a move that keeps the domain and stays live.
UPDATE mcp_endpoints
SET custom_domain_id = @custom_domain_id,
    slug = @slug,
    is_domain_root = CASE
      WHEN custom_domain_id IS NOT DISTINCT FROM @custom_domain_id AND sqlc.narg(deleted_at)::timestamptz IS NULL THEN is_domain_root
      ELSE NULL
    END,
    deleted_at = sqlc.narg(deleted_at)::timestamptz,
    updated_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id;

-- name: CopyMCPGrantsToWrapper :execrows
INSERT INTO principal_grants (organization_id, principal_urn, scope, effect, selectors)
SELECT g.organization_id, g.principal_urn, g.scope, g.effect,
       jsonb_set(g.selectors, '{resource_id}', to_jsonb(@wrapper_id::text))
FROM principal_grants AS g
WHERE g.organization_id = @organization_id
  AND g.selectors->>'resource_kind' = 'mcp'
  AND g.selectors->>'resource_id' = @toolset_id::text
ON CONFLICT DO NOTHING;

-- name: RetireToolsetMCPGrants :execrows
DELETE FROM principal_grants AS g
WHERE g.organization_id = @organization_id
  AND g.selectors->>'resource_kind' = 'mcp'
  AND g.selectors->>'resource_id' = @toolset_id::text
  AND EXISTS (
    SELECT 1 FROM principal_grants AS w
    WHERE w.organization_id = g.organization_id
      AND w.principal_urn = g.principal_urn
      AND w.scope = g.scope
      AND w.effect IS NOT DISTINCT FROM g.effect
      AND w.selectors = jsonb_set(g.selectors, '{resource_id}', to_jsonb(@wrapper_id::text))
  );

-- name: CountOauthProxyToolsets :one
SELECT count(*) FILTER (WHERE deleted IS FALSE)::bigint AS live,
       count(*) FILTER (WHERE deleted IS FALSE AND mcp_enabled)::bigint AS enabled,
       count(*) FILTER (WHERE deleted IS FALSE AND mcp_enabled AND mcp_is_public)::bigint AS enabled_public
FROM toolsets
WHERE oauth_proxy_server_id IS NOT NULL;

-- name: MoveMcpMetadata :execrows
UPDATE mcp_metadata AS m
SET mcp_server_id = @mcp_server_id, toolset_id = NULL
WHERE m.toolset_id = @toolset_id AND m.project_id = @project_id
  AND NOT EXISTS (SELECT 1 FROM mcp_metadata AS d WHERE d.mcp_server_id = @mcp_server_id);

-- name: CountSkippedMcpMetadata :one
SELECT count(*) FROM mcp_metadata WHERE toolset_id = @toolset_id AND project_id = @project_id;

-- name: MoveCollectionAttachments :execrows
UPDATE organization_mcp_collection_server_attachments AS a
SET mcp_server_id = @mcp_server_id, toolset_id = NULL
FROM organization_mcp_collections AS c
WHERE a.collection_id = c.id
  AND c.organization_id = @organization_id
  AND a.toolset_id = @toolset_id
  AND (a.deleted OR NOT EXISTS (
    SELECT 1 FROM organization_mcp_collection_server_attachments AS d
    WHERE d.collection_id = a.collection_id AND d.mcp_server_id = @mcp_server_id AND d.deleted IS FALSE
  ));

-- name: CountSkippedCollectionAttachments :one
SELECT count(*)
FROM organization_mcp_collection_server_attachments AS a
JOIN organization_mcp_collections AS c ON a.collection_id = c.id
WHERE c.organization_id = @organization_id AND a.toolset_id = @toolset_id;

-- name: MovePluginServers :execrows
UPDATE plugin_servers AS ps
SET mcp_server_id = @mcp_server_id, toolset_id = NULL
FROM plugins AS p
WHERE ps.plugin_id = p.id
  AND p.project_id = @project_id
  AND ps.toolset_id = @toolset_id
  AND (ps.deleted OR NOT EXISTS (
    SELECT 1 FROM plugin_servers AS d
    WHERE d.plugin_id = ps.plugin_id AND d.mcp_server_id = @mcp_server_id AND d.deleted IS FALSE
  ));

-- name: CountSkippedPluginServers :one
SELECT count(*)
FROM plugin_servers AS ps
JOIN plugins AS p ON ps.plugin_id = p.id
WHERE p.project_id = @project_id AND ps.toolset_id = @toolset_id;

-- name: MoveAssistantToolsets :many
INSERT INTO assistant_mcp_servers (id, assistant_id, mcp_server_id, environment_id, project_id, created_at, updated_at)
SELECT at.id, at.assistant_id, @mcp_server_id, at.environment_id, at.project_id, at.created_at, at.updated_at
FROM assistant_toolsets AS at
WHERE at.toolset_id = @toolset_id AND at.project_id = @project_id
ON CONFLICT (assistant_id, mcp_server_id) DO NOTHING
RETURNING id;

-- name: DeleteAssistantToolsetsByIDs :execrows
DELETE FROM assistant_toolsets
WHERE id = ANY(@ids::uuid[]) AND toolset_id = @toolset_id AND project_id = @project_id;

-- name: CountSkippedAssistantToolsets :one
SELECT count(*) FROM assistant_toolsets WHERE toolset_id = @toolset_id AND project_id = @project_id;

-- TEST FIXTURE ONLY: every query below is for package tests and may create impossible states or take exclusive locks.

-- name: SeedOrganizationFixture :exec
INSERT INTO organization_metadata (id, name, slug)
VALUES (@id, @name, @slug);

-- name: SeedProjectFixture :exec
INSERT INTO projects (id, name, slug, organization_id)
VALUES (@id, @name, @slug, @organization_id);

-- name: SeedCustomDomainFixture :exec
INSERT INTO custom_domains (id, organization_id, domain, verified, activated, deleted_at)
VALUES (@id, @organization_id, @domain, TRUE, TRUE, @deleted_at);

-- name: SeedUserSessionIssuerFixture :exec
INSERT INTO user_session_issuers (id, project_id, organization_id, slug, authn_challenge_mode, session_duration)
VALUES (@id, @project_id, @organization_id, @slug, 'interactive', INTERVAL '1 hour');

-- name: SeedToolsetFixture :exec
INSERT INTO toolsets (
  id, organization_id, project_id, name, slug, mcp_slug, mcp_is_public, mcp_enabled,
  custom_domain_id, user_session_issuer_id, tool_variations_group_id
) VALUES (
  @id, @organization_id, @project_id, @name, @slug, @mcp_slug, @mcp_is_public, @mcp_enabled,
  @custom_domain_id, @user_session_issuer_id, @tool_variations_group_id
);

-- name: SeedWrapperFixture :exec
INSERT INTO mcp_servers (id, project_id, name, slug, user_session_issuer_id, toolset_id, visibility)
VALUES (@id, @project_id, @name, @slug, @user_session_issuer_id, @toolset_id, @visibility);

-- name: SeedEndpointFixture :exec
INSERT INTO mcp_endpoints (id, project_id, custom_domain_id, mcp_server_id, slug, is_domain_root)
VALUES (@id, @project_id, @custom_domain_id, @mcp_server_id, @slug, @is_domain_root);

-- name: SeedGrantFixture :exec
INSERT INTO principal_grants (organization_id, principal_urn, scope, effect, selectors)
VALUES (@organization_id, @principal_urn, @scope, @effect, @selectors);

-- name: SeedMcpMetadataFixture :exec
INSERT INTO mcp_metadata (id, toolset_id, project_id, instructions)
VALUES (@id, @toolset_id, @project_id, @instructions);

-- name: SeedCollectionFixture :exec
INSERT INTO organization_mcp_collections (id, organization_id, name, slug, visibility)
VALUES (@id, @organization_id, @name, @slug, 'private');

-- name: SeedCollectionAttachmentFixture :exec
INSERT INTO organization_mcp_collection_server_attachments (id, collection_id, toolset_id, deleted_at)
VALUES (@id, @collection_id, @toolset_id, @deleted_at);

-- name: SeedPluginFixture :exec
INSERT INTO plugins (id, organization_id, project_id, name, slug)
VALUES (@id, @organization_id, @project_id, @name, @slug);

-- name: SeedPluginServerFixture :exec
INSERT INTO plugin_servers (id, plugin_id, toolset_id, display_name)
VALUES (@id, @plugin_id, @toolset_id, @display_name);

-- name: SeedAssistantFixture :exec
INSERT INTO assistants (id, project_id, organization_id, name, model, instructions)
VALUES (@id, @project_id, @organization_id, @name, 'test-model', '');

-- name: SeedAssistantToolsetFixture :exec
INSERT INTO assistant_toolsets (id, assistant_id, toolset_id, project_id)
VALUES (@id, @assistant_id, @toolset_id, @project_id);

-- name: SeedAssistantMcpServerFixture :exec
INSERT INTO assistant_mcp_servers (id, assistant_id, mcp_server_id, project_id)
VALUES (@id, @assistant_id, @mcp_server_id, @project_id);

-- name: UpdateToolsetSlugFixture :exec
UPDATE toolsets SET mcp_slug = @mcp_slug WHERE id = @id;

-- name: UpdateToolsetDomainFixture :exec
UPDATE toolsets SET custom_domain_id = @custom_domain_id WHERE id = @id;

-- name: UpdateWrapperSlugFixture :exec
UPDATE mcp_servers SET slug = @slug WHERE id = @id;

-- name: SetEndpointRootFixture :exec
UPDATE mcp_endpoints SET is_domain_root = TRUE WHERE id = @id;

-- name: SoftDeleteWrapperFixture :exec
UPDATE mcp_servers SET deleted_at = clock_timestamp() WHERE id = @id;

-- name: GetWrapperFixture :one
SELECT id, name, slug, visibility, user_session_issuer_id, tool_variations_group_id, remote_session_issuer_id
FROM mcp_servers
WHERE toolset_id = @toolset_id AND deleted IS FALSE;

-- name: SeedRemoteSessionIssuerFixture :exec
INSERT INTO remote_session_issuers (id, project_id, organization_id, slug, issuer)
VALUES (@id, @project_id, @organization_id, @slug, @issuer);

-- name: SeedRemoteSessionClientFixture :exec
INSERT INTO remote_session_clients (id, project_id, organization_id, remote_session_issuer_id, client_id)
VALUES (@id, @project_id, @organization_id, @remote_session_issuer_id, @client_id);

-- name: SeedRemoteSessionClientIssuerLinkFixture :exec
INSERT INTO remote_session_client_user_session_issuers (remote_session_client_id, user_session_issuer_id)
VALUES (@remote_session_client_id, @user_session_issuer_id);

-- name: CountWrappersFixture :one
SELECT count(*) FROM mcp_servers WHERE toolset_id = @toolset_id;

-- name: ListEndpointsFixture :many
SELECT id, custom_domain_id, slug, deleted, deleted_at, is_domain_root
FROM mcp_endpoints
WHERE mcp_server_id = @mcp_server_id
ORDER BY slug, custom_domain_id;

-- name: ListGrantsFixture :many
SELECT scope, effect, (selectors->>'resource_id')::text AS resource_id
FROM principal_grants
WHERE organization_id = @organization_id
ORDER BY scope, effect, resource_id;

-- name: GetMcpMetadataFixture :one
SELECT id, toolset_id, mcp_server_id
FROM mcp_metadata
WHERE id = @id;

-- name: GetCollectionAttachmentFixture :one
SELECT id, toolset_id, mcp_server_id, deleted, created_at
FROM organization_mcp_collection_server_attachments
WHERE id = @id;

-- name: GetPluginServerFixture :one
SELECT id, toolset_id, mcp_server_id
FROM plugin_servers
WHERE id = @id;

-- name: CountAssistantToolsetsFixture :one
SELECT count(*) FROM assistant_toolsets WHERE toolset_id = @toolset_id;

-- name: GetAssistantMcpServerFixture :one
SELECT id, created_at
FROM assistant_mcp_servers
WHERE assistant_id = @assistant_id AND mcp_server_id = @mcp_server_id;
