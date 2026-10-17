-- name: ListCandidates :many
-- Live servers whose latest toolset version references a proxy tool from an
-- external MCP attachment on the project's active deployment, across every
-- organization. A server is live when its toolset is MCP-enabled or fronted
-- by a live mcp_servers row. Pass toolset_ids to narrow the scan.
WITH active_deployments AS (
  SELECT DISTINCT ON (d.project_id) d.project_id, d.id
  FROM deployments AS d
  JOIN deployment_statuses AS ds ON ds.deployment_id = d.id
  WHERE ds.status = 'completed'
  ORDER BY d.project_id, d.seq DESC
),
live_toolsets AS (
  SELECT
    t.id,
    t.organization_id,
    t.project_id,
    t.name,
    t.slug,
    t.mcp_slug,
    t.mcp_enabled,
    t.mcp_is_public,
    t.custom_domain_id,
    t.user_session_issuer_id,
    t.external_oauth_server_id,
    t.oauth_proxy_server_id,
    t.default_environment_slug
  FROM toolsets AS t
  JOIN projects AS p ON p.id = t.project_id AND p.deleted IS FALSE
  WHERE t.deleted IS FALSE
    AND (sqlc.narg('toolset_ids')::uuid[] IS NULL OR t.id = ANY(sqlc.narg('toolset_ids')::uuid[]))
    AND (
      t.mcp_enabled
      OR EXISTS (
        SELECT 1
        FROM mcp_servers AS ms
        WHERE ms.toolset_id = t.id AND ms.project_id = t.project_id AND ms.deleted IS FALSE
      )
    )
),
latest_versions AS (
  SELECT DISTINCT ON (tv.toolset_id) tv.toolset_id, tv.tool_urns
  FROM toolset_versions AS tv
  JOIN live_toolsets AS lt ON lt.id = tv.toolset_id
  WHERE tv.deleted IS FALSE
  ORDER BY tv.toolset_id, tv.version DESC
),
proxy_definitions AS (
  SELECT
    t.id AS toolset_id,
    etd.remote_url,
    etd.transport_type,
    CASE
      WHEN jsonb_typeof(etd.header_definitions) = 'array' THEN jsonb_array_length(etd.header_definitions) > 0
      ELSE FALSE
    END AS declares_headers
  FROM live_toolsets AS t
  JOIN latest_versions AS lv ON lv.toolset_id = t.id
  JOIN active_deployments AS ad ON ad.project_id = t.project_id
  CROSS JOIN LATERAL unnest(lv.tool_urns) AS tool_urn(value)
  JOIN external_mcp_tool_definitions AS etd ON etd.tool_urn = tool_urn.value
  JOIN external_mcp_attachments AS ea ON ea.id = etd.external_mcp_attachment_id
  WHERE ea.deployment_id = ad.id
    AND etd.type = 'proxy'
    AND etd.deleted IS FALSE
    AND ea.deleted IS FALSE
),
grouped AS (
  SELECT
    pd.toolset_id,
    array_agg(DISTINCT pd.remote_url ORDER BY pd.remote_url)::text[] AS remote_urls,
    array_agg(DISTINCT pd.transport_type ORDER BY pd.transport_type)::text[] AS transport_types,
    bool_or(pd.declares_headers) AS declares_headers
  FROM proxy_definitions AS pd
  GROUP BY pd.toolset_id
)
SELECT
  t.id AS toolset_id,
  t.organization_id,
  o.name AS organization_name,
  o.slug AS organization_slug,
  t.project_id,
  p.name AS project_name,
  p.slug AS project_slug,
  t.name AS toolset_name,
  t.slug AS toolset_slug,
  t.mcp_slug,
  t.mcp_enabled,
  t.mcp_is_public,
  t.custom_domain_id,
  t.user_session_issuer_id,
  t.external_oauth_server_id,
  t.oauth_proxy_server_id,
  t.default_environment_slug,
  usi.slug AS user_session_issuer_slug,
  COALESCE((
    SELECT array_agg(DISTINCT COALESCE(rsi.name, rsi.issuer) ORDER BY COALESCE(rsi.name, rsi.issuer))
    FROM remote_session_client_user_session_issuers AS link
    JOIN remote_session_clients AS rsc ON rsc.id = link.remote_session_client_id AND rsc.deleted IS FALSE
    JOIN remote_session_issuers AS rsi ON rsi.id = rsc.remote_session_issuer_id AND rsi.deleted IS FALSE
    WHERE link.user_session_issuer_id = t.user_session_issuer_id
  ), ARRAY[]::text[])::text[] AS remote_issuer_names,
  g.remote_urls,
  g.transport_types,
  g.declares_headers,
  wrapper.network_access_mode AS wrapper_network_access_mode
FROM grouped AS g
JOIN live_toolsets AS t ON t.id = g.toolset_id
JOIN projects AS p ON p.id = t.project_id
JOIN organization_metadata AS o ON o.id = t.organization_id
LEFT JOIN user_session_issuers AS usi ON usi.id = t.user_session_issuer_id AND usi.deleted IS FALSE
LEFT JOIN mcp_servers AS wrapper
  ON wrapper.id = t.id AND wrapper.toolset_id = t.id AND wrapper.project_id = t.project_id AND wrapper.deleted IS FALSE
ORDER BY o.name, p.name, t.name, t.id;

-- name: ListRemoteServersOnIssuers :many
-- Live remote-backed MCP servers that share one of the given user session
-- issuers, the servers a migration may merge into instead of creating one.
SELECT
  ms.id,
  ms.project_id,
  ms.name,
  ms.slug,
  ms.user_session_issuer_id,
  ms.network_access_mode,
  rms.url
FROM mcp_servers AS ms
JOIN remote_mcp_servers AS rms ON rms.id = ms.remote_mcp_server_id AND rms.project_id = ms.project_id AND rms.deleted IS FALSE
WHERE ms.deleted IS FALSE
  AND ms.user_session_issuer_id = ANY(@user_session_issuer_ids::uuid[])
ORDER BY ms.project_id, ms.name, ms.id;

-- name: LockToolset :one
SELECT id
FROM toolsets
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
FOR UPDATE;

-- name: LockTargetServer :one
-- The existing remote-backed server a migration merges into.
SELECT ms.id, ms.name, ms.slug, ms.user_session_issuer_id, ms.remote_mcp_server_id, ms.network_access_mode, rms.url AS remote_url
FROM mcp_servers AS ms
JOIN remote_mcp_servers AS rms ON rms.id = ms.remote_mcp_server_id AND rms.project_id = ms.project_id AND rms.deleted IS FALSE
WHERE ms.id = @id AND ms.project_id = @project_id AND ms.deleted IS FALSE
FOR UPDATE OF ms;

-- name: GetCanonicalWrapper :one
SELECT *
FROM mcp_servers
WHERE id = @toolset_id AND project_id = @project_id AND toolset_id = @toolset_id AND deleted IS FALSE
FOR UPDATE;

-- name: ListRemoteURLsOnIssuer :many
-- Upstream URLs of the live remote servers signed in through an issuer,
-- other than the one being migrated onto, across the issuer's organization.
SELECT DISTINCT rms.url
FROM mcp_servers AS ms
JOIN remote_mcp_servers AS rms ON rms.id = ms.remote_mcp_server_id AND rms.project_id = ms.project_id AND rms.deleted IS FALSE
JOIN projects AS p ON p.id = ms.project_id AND p.organization_id = @organization_id
WHERE ms.deleted IS FALSE
  AND ms.user_session_issuer_id = @user_session_issuer_id
  AND ms.id <> @excluded_mcp_server_id;

-- name: ListPluginServersForBackend :many
-- Live plugin entries pointing at the old server, keyed either by its toolset
-- or by its canonical wrapper, locked for the move.
SELECT
  ps.id,
  ps.plugin_id,
  ps.toolset_id,
  ps.mcp_server_id,
  ps.display_name,
  ps.policy,
  ps.sort_order,
  pl.name AS plugin_name,
  pl.slug AS plugin_slug,
  EXISTS (
    SELECT 1
    FROM platform_mcp_distributions AS d
    WHERE d.project_id = pl.project_id AND d.plugin_server_id = ps.id
  ) AS distributed
FROM plugin_servers AS ps
JOIN plugins AS pl ON pl.id = ps.plugin_id
WHERE pl.project_id = @project_id
  AND ps.deleted IS FALSE
  AND (ps.toolset_id = @backend_id OR ps.mcp_server_id = @backend_id)
ORDER BY ps.plugin_id, ps.id
FOR UPDATE OF ps;

-- name: GetPluginServerForServer :one
-- The plugin's live entry for a server, if it already carries one.
SELECT ps.id
FROM plugin_servers AS ps
JOIN plugins AS pl ON pl.id = ps.plugin_id
WHERE ps.plugin_id = @plugin_id
  AND pl.project_id = @project_id
  AND ps.mcp_server_id = @mcp_server_id
  AND ps.deleted IS FALSE
ORDER BY ps.id
LIMIT 1;

-- name: RepointDistributionsToPluginServer :execrows
-- Moves Platform MCP distributions off a plugin entry being removed onto the
-- entry the plugin keeps, which sits in the same plugin.
UPDATE platform_mcp_distributions
SET plugin_server_id = @retained_plugin_server_id, version = version + 1, updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND plugin_server_id = @removed_plugin_server_id;

-- name: RekeyPluginServer :execrows
UPDATE plugin_servers AS ps
SET toolset_id = NULL, mcp_server_id = @mcp_server_id, updated_at = clock_timestamp()
FROM plugins AS pl
WHERE ps.id = @id
  AND pl.id = ps.plugin_id
  AND pl.project_id = @project_id
  AND ps.deleted IS FALSE;

-- name: SoftDeletePluginServer :execrows
UPDATE plugin_servers AS ps
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
FROM plugins AS pl
WHERE ps.id = @id
  AND pl.id = ps.plugin_id
  AND pl.project_id = @project_id
  AND ps.deleted IS FALSE;

-- name: ListAssistantAttachmentsForBackend :many
-- Assistant attachments of the old server, from both the toolset-keyed and
-- the server-keyed tables, with their assistants locked first.
WITH attachments AS (
  SELECT 'toolset'::text AS kind, at.id, at.assistant_id, at.environment_id
  FROM assistant_toolsets AS at
  WHERE at.project_id = @project_id AND at.toolset_id = @backend_id
  UNION ALL
  SELECT 'mcp_server'::text AS kind, ams.id, ams.assistant_id, ams.environment_id
  FROM assistant_mcp_servers AS ams
  WHERE ams.project_id = @project_id AND ams.mcp_server_id = @backend_id
),
locked AS (
  SELECT a.id
  FROM assistants AS a
  WHERE a.project_id = @project_id
    AND a.id IN (SELECT assistant_id FROM attachments)
  ORDER BY a.id
  FOR NO KEY UPDATE
)
SELECT at.kind, at.id, at.assistant_id, at.environment_id
FROM attachments AS at
JOIN locked AS l ON l.id = at.assistant_id
ORDER BY at.assistant_id, at.kind DESC, at.id;

-- name: AssistantHasServer :one
SELECT EXISTS (
  SELECT 1
  FROM assistant_mcp_servers
  WHERE assistant_id = @assistant_id AND project_id = @project_id AND mcp_server_id = @mcp_server_id
) AS present;

-- name: MoveAssistantToolsetToServer :execrows
-- Same id, environment and created_at, so the attachment is the same row in its new table.
WITH moved AS (
  DELETE FROM assistant_toolsets AS at
  WHERE at.id = @id AND at.project_id = @project_id
  RETURNING at.id, at.assistant_id, at.environment_id, at.project_id, at.created_at
)
INSERT INTO assistant_mcp_servers (id, assistant_id, mcp_server_id, environment_id, project_id, created_at)
SELECT moved.id, moved.assistant_id, @mcp_server_id::uuid, moved.environment_id, moved.project_id, moved.created_at
FROM moved;

-- name: RepointAssistantMcpServer :execrows
UPDATE assistant_mcp_servers
SET mcp_server_id = @mcp_server_id, updated_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id;

-- name: DeleteAssistantToolset :execrows
DELETE FROM assistant_toolsets
WHERE id = @id AND project_id = @project_id;

-- name: DeleteAssistantMcpServer :execrows
DELETE FROM assistant_mcp_servers
WHERE id = @id AND project_id = @project_id;

-- name: ListMetadataForBackend :many
-- The old server's metadata rows, toolset-keyed first, locked for the move.
SELECT id, toolset_id, mcp_server_id
FROM mcp_metadata
WHERE project_id = @project_id
  AND (toolset_id = @backend_id OR mcp_server_id = @backend_id)
ORDER BY (toolset_id IS NULL), id
FOR UPDATE;

-- name: ServerHasMetadata :one
SELECT EXISTS (
  SELECT 1
  FROM mcp_metadata
  WHERE project_id = @project_id AND mcp_server_id = @mcp_server_id
) AS present;

-- name: RekeyMetadata :execrows
-- Same row id, so its mcp_environment_configs stay attached.
UPDATE mcp_metadata
SET toolset_id = NULL, mcp_server_id = @mcp_server_id, updated_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id;

-- name: DeleteMetadata :execrows
-- Cascades to the row's mcp_environment_configs.
DELETE FROM mcp_metadata
WHERE id = @id AND project_id = @project_id;

-- name: RepointMetaMcpMembers :many
-- Moves gateway memberships to the new server unless it is already a member
-- of that gateway; the memberships left behind are removed with the old server.
UPDATE meta_mcp_server_members AS m
SET mcp_server_id = @mcp_server_id, updated_at = clock_timestamp()
FROM meta_mcp_servers AS meta
WHERE m.project_id = @project_id
  AND m.mcp_server_id = @backend_id
  AND m.deleted IS FALSE
  AND meta.id = m.meta_mcp_server_id
  AND meta.project_id = m.project_id
  AND NOT EXISTS (
    SELECT 1
    FROM meta_mcp_server_members AS existing
    WHERE existing.meta_mcp_server_id = m.meta_mcp_server_id
      AND existing.mcp_server_id = @mcp_server_id
      AND existing.deleted IS FALSE
  )
RETURNING m.id, m.meta_mcp_server_id, meta.name AS meta_mcp_server_name, m.sort_order;

-- name: RemoveDuplicateCollectionAttachments :execrows
-- Removes the old server's collection attachments that cannot move: those in
-- a collection that already carries the new server, and all but one of a
-- collection's twins, keeping the wrapper-keyed one, then the oldest.
UPDATE organization_mcp_collection_server_attachments AS a
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
FROM organization_mcp_collections AS c
WHERE c.id = a.collection_id
  AND c.organization_id = @organization_id
  AND a.deleted IS FALSE
  AND (a.toolset_id = @backend_id OR a.mcp_server_id = @backend_id)
  AND (
    EXISTS (
      SELECT 1
      FROM organization_mcp_collection_server_attachments AS existing
      WHERE existing.collection_id = a.collection_id
        AND existing.mcp_server_id = @mcp_server_id
        AND existing.deleted IS FALSE
    )
    OR EXISTS (
      SELECT 1
      FROM organization_mcp_collection_server_attachments AS twin
      WHERE twin.collection_id = a.collection_id
        AND twin.id <> a.id
        AND twin.deleted IS FALSE
        AND (twin.toolset_id = @backend_id OR twin.mcp_server_id = @backend_id)
        AND (
          (twin.mcp_server_id IS NOT NULL AND a.mcp_server_id IS NULL)
          OR ((twin.mcp_server_id IS NULL) = (a.mcp_server_id IS NULL) AND twin.id < a.id)
        )
    )
  );

-- name: RekeyCollectionAttachments :execrows
-- Moves the old server's remaining collection attachments to the new server,
-- after RemoveDuplicateCollectionAttachments left at most one per collection.
UPDATE organization_mcp_collection_server_attachments AS a
SET toolset_id = NULL, mcp_server_id = @mcp_server_id, updated_at = clock_timestamp()
FROM organization_mcp_collections AS c
WHERE c.id = a.collection_id
  AND c.organization_id = @organization_id
  AND a.deleted IS FALSE
  AND (a.toolset_id = @backend_id OR a.mcp_server_id = @backend_id)
  AND NOT EXISTS (
    SELECT 1
    FROM organization_mcp_collection_server_attachments AS existing
    WHERE existing.collection_id = a.collection_id
      AND existing.mcp_server_id = @mcp_server_id
      AND existing.deleted IS FALSE
  );

-- name: CopyMCPGrantsToServer :execrows
-- Copies every MCP grant naming the old server onto the new one, keeping the
-- principal, effect and every other selector key.
INSERT INTO principal_grants (organization_id, principal_urn, scope, effect, drop_resource, selectors)
SELECT
  g.organization_id,
  g.principal_urn,
  g.scope,
  g.effect,
  g.drop_resource,
  jsonb_set(g.selectors, '{resource_id}', to_jsonb(@mcp_server_id::text))
FROM principal_grants AS g
WHERE g.organization_id = @organization_id
  AND g.scope LIKE 'mcp:%'
  AND g.selectors->>'resource_id' = @backend_id::text
ON CONFLICT DO NOTHING;

-- name: RewriteRemoteSessionResources :execrows
-- Stamps the new upstream URL on the issuer's sessions that recorded no
-- resource or the old one, the same stamp a refresh would apply.
UPDATE remote_sessions AS rs
SET resource = @resource
WHERE rs.user_session_issuer_id = @user_session_issuer_id
  AND rs.deleted IS FALSE
  AND (rs.resource IS NULL OR rs.resource = '' OR rtrim(rs.resource, '/') = rtrim(@previous_resource::text, '/'))
  AND rs.user_session_issuer_id IN (
    SELECT usi.id
    FROM user_session_issuers AS usi
    WHERE usi.id = @user_session_issuer_id
      AND (usi.organization_id = @organization_id::text OR usi.project_id IN (SELECT p.id FROM projects AS p WHERE p.organization_id = @organization_id::text))
  )
  AND rs.remote_session_client_id IN (
    SELECT link.remote_session_client_id
    FROM remote_session_client_user_session_issuers AS link
    WHERE link.user_session_issuer_id = @user_session_issuer_id
  );

-- TEST FIXTURE ONLY: every query below is for package tests and may create impossible states.

-- name: SeedCompletedDeploymentFixture :one
WITH deployment AS (
  INSERT INTO deployments (project_id, organization_id, user_id, idempotency_key)
  VALUES (@project_id, @organization_id, 'fixture-user', @idempotency_key)
  RETURNING id
),
status AS (
  INSERT INTO deployment_statuses (deployment_id, status)
  SELECT id, 'completed' FROM deployment
)
SELECT id FROM deployment;

-- name: SeedExternalMCPProxyFixture :exec
WITH attachment AS (
  INSERT INTO external_mcp_attachments (deployment_id, name, slug, registry_server_specifier)
  VALUES (@deployment_id, @slug, @slug, @slug)
  RETURNING id
)
INSERT INTO external_mcp_tool_definitions (external_mcp_attachment_id, tool_urn, remote_url, transport_type, type, header_definitions)
SELECT id, @tool_urn, @remote_url, 'streamable-http', 'proxy', sqlc.narg('header_definitions')::jsonb
FROM attachment;

-- name: SeedRemoteSessionClientFixture :one
-- A remote issuer and one client linked to the user session issuer.
WITH issuer AS (
  INSERT INTO remote_session_issuers (project_id, organization_id, slug, issuer)
  VALUES (@project_id, @organization_id, @slug, @issuer_url)
  RETURNING id
),
client AS (
  INSERT INTO remote_session_clients (project_id, organization_id, remote_session_issuer_id, client_id)
  SELECT @project_id, @organization_id, issuer.id, 'hand-registered-client'
  FROM issuer
  RETURNING id
),
link AS (
  INSERT INTO remote_session_client_user_session_issuers (remote_session_client_id, user_session_issuer_id)
  SELECT client.id, @user_session_issuer_id FROM client
)
SELECT id FROM client;

-- name: SeedRemoteSessionFixture :one
INSERT INTO remote_sessions (subject_urn, user_session_issuer_id, remote_session_client_id, access_token_encrypted, resource)
VALUES (@subject_urn, @user_session_issuer_id, @remote_session_client_id, 'encrypted', sqlc.narg('resource'))
RETURNING id;

-- name: GetRemoteSessionResourceFixture :one
SELECT resource
FROM remote_sessions
WHERE id = @id;

-- name: SeedPluginServerFixture :one
WITH plugin AS (
  INSERT INTO plugins (organization_id, project_id, name, slug)
  VALUES (@organization_id, @project_id, @name, @name)
  RETURNING id
)
INSERT INTO plugin_servers (plugin_id, project_id, toolset_id, display_name)
SELECT id, @project_id, @toolset_id, @name FROM plugin
RETURNING id;

-- name: GetPluginServerFixture :one
SELECT toolset_id, mcp_server_id, deleted
FROM plugin_servers
WHERE id = @id;

-- name: CountIssuersAndClientsFixture :one
SELECT
  (SELECT count(*) FROM user_session_issuers AS usi WHERE usi.organization_id = @organization_id::text OR usi.project_id IN (SELECT p.id FROM projects AS p WHERE p.organization_id = @organization_id::text))::bigint AS user_session_issuers,
  (SELECT count(*) FROM remote_session_clients AS rsc WHERE rsc.organization_id = @organization_id::text OR rsc.project_id IN (SELECT p.id FROM projects AS p WHERE p.organization_id = @organization_id::text))::bigint AS remote_session_clients,
  (SELECT count(*) FROM remote_session_client_user_session_issuers)::bigint AS client_links;

-- name: ListGrantResourceIDsFixture :many
SELECT (selectors->>'resource_id')::text AS resource_id, scope
FROM principal_grants
WHERE organization_id = @organization_id AND scope LIKE 'mcp:%'
ORDER BY 1, 2;

-- name: SeedPluginServerForServerFixture :one
INSERT INTO plugin_servers (plugin_id, project_id, mcp_server_id, display_name)
VALUES (@plugin_id, @project_id, @mcp_server_id, @display_name)
RETURNING id;

-- name: SeedDistributionFixture :one
WITH registration AS (
  INSERT INTO platform_mcp_catalog_registrations (organization_id, project_id, source_kind, catalog_provider, catalog_reference)
  VALUES (@organization_id, @project_id, 'remote', 'fixture', @catalog_reference)
  RETURNING id
)
INSERT INTO platform_mcp_distributions (
  organization_id, project_id, registration_id, default_plugin_id, plugin_id, plugin_server_id,
  state, version, attachment_was_created, user_id, acting_surface
)
SELECT @organization_id, @project_id, registration.id, @plugin_id, @plugin_id, @plugin_server_id,
  'active', 1, FALSE, 'fixture-user', 'dashboard'
FROM registration
RETURNING id;

-- name: GetDistributionPluginServerFixture :one
SELECT plugin_server_id
FROM platform_mcp_distributions
WHERE id = @id;

-- name: SeedCollectionFixture :one
INSERT INTO organization_mcp_collections (organization_id, name, slug, visibility)
VALUES (@organization_id, @slug, @slug, 'private')
RETURNING id;

-- name: SeedCollectionAttachmentFixture :one
INSERT INTO organization_mcp_collection_server_attachments (collection_id, toolset_id, mcp_server_id)
VALUES (@collection_id, sqlc.narg('toolset_id'), sqlc.narg('mcp_server_id'))
RETURNING id;

-- name: ListCollectionAttachmentsFixture :many
SELECT id, toolset_id, mcp_server_id, deleted
FROM organization_mcp_collection_server_attachments
WHERE collection_id = @collection_id
ORDER BY id;

-- name: SetNetworkAccessModeFixture :exec
UPDATE mcp_servers
SET network_access_mode = @network_access_mode
WHERE id = @id;

-- name: GetPluginIDFixture :one
SELECT plugin_id
FROM plugin_servers
WHERE id = @id;
