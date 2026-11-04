-- name: ListCandidates :many
-- Toolsets whose latest version references a proxy tool, or the per-tool
-- (direct) tools, of an external MCP attachment on any deployment, across
-- every organization, with what staff need to decide whether each one switches
-- in place or is deleted. A toolset is live when it is MCP-enabled or fronted
-- by a live mcp_servers row; on_active_deployment is set when its project's
-- active deployment still serves the attachment. Pass toolset_ids to narrow
-- the scan.
WITH active_deployments AS (
  SELECT DISTINCT ON (d.project_id) d.project_id, d.id
  FROM deployments AS d
  JOIN deployment_statuses AS ds ON ds.deployment_id = d.id
  WHERE ds.status = 'completed'
    AND (
      sqlc.narg('toolset_ids')::uuid[] IS NULL
      OR d.project_id IN (SELECT t.project_id FROM toolsets AS t WHERE t.id = ANY(sqlc.narg('toolset_ids')::uuid[]))
    )
  ORDER BY d.project_id, d.seq DESC
),
-- Every toolset, live or not, so the ones left with nothing to migrate are
-- listed for deletion.
scoped_toolsets AS (
  SELECT
    t.id,
    t.organization_id,
    t.project_id,
    t.name,
    t.slug,
    -- The canonical wrapper owns hosting state; a toolset without one is not hosted.
    (cw.id IS NOT NULL AND cw.visibility <> 'disabled')::boolean AS mcp_enabled,
    -- The issuer of the server fronting the toolset: its canonical wrapper, else the server that holds it.
    (
      SELECT ms.user_session_issuer_id
      FROM mcp_servers AS ms
      WHERE ms.toolset_id = t.id AND ms.project_id = t.project_id AND ms.deleted IS FALSE
      ORDER BY (ms.id = t.id) DESC, ms.id
      LIMIT 1
    ) AS user_session_issuer_id,
    t.external_oauth_server_id,
    t.tool_variations_group_id,
    (
      (cw.id IS NOT NULL AND cw.visibility <> 'disabled')
      OR EXISTS (
        SELECT 1
        FROM mcp_servers AS ms
        WHERE ms.toolset_id = t.id AND ms.project_id = t.project_id AND ms.deleted IS FALSE
      )
    ) AS live
  FROM toolsets AS t
  JOIN projects AS p ON p.id = t.project_id AND p.deleted IS FALSE
  LEFT JOIN mcp_servers AS cw ON cw.id = t.id AND cw.toolset_id = t.id AND cw.project_id = t.project_id AND cw.deleted IS FALSE
  WHERE t.deleted IS FALSE
    AND (sqlc.narg('toolset_ids')::uuid[] IS NULL OR t.id = ANY(sqlc.narg('toolset_ids')::uuid[]))
),
latest_versions AS (
  SELECT DISTINCT ON (tv.toolset_id) tv.toolset_id, tv.tool_urns, tv.resource_urns
  FROM toolset_versions AS tv
  JOIN scoped_toolsets AS lt ON lt.id = tv.toolset_id
  WHERE tv.deleted IS FALSE
  ORDER BY tv.toolset_id, tv.version DESC
),
-- Definitions on any deployment; on_active marks the ones the project's active
-- deployment serves, which are all the migration looks at.
proxy_definitions AS (
  SELECT
    t.id AS toolset_id,
    (ea.deployment_id = ad.id) IS TRUE AS on_active,
    ea.id AS attachment_id,
    etd.tool_urn,
    etd.remote_url,
    etd.transport_type,
    etd.requires_oauth,
    CASE
      WHEN jsonb_typeof(etd.header_definitions) = 'array' THEN jsonb_array_length(etd.header_definitions) > 0
      ELSE FALSE
    END AS declares_headers
  FROM scoped_toolsets AS t
  JOIN latest_versions AS lv ON lv.toolset_id = t.id
  LEFT JOIN active_deployments AS ad ON ad.project_id = t.project_id
  CROSS JOIN LATERAL unnest(lv.tool_urns) AS tool_urn(value)
  JOIN external_mcp_tool_definitions AS etd ON etd.tool_urn = tool_urn.value
  JOIN external_mcp_attachments AS ea ON ea.id = etd.external_mcp_attachment_id
  WHERE etd.type IN ('proxy', 'direct')
    AND etd.deleted IS FALSE
    AND ea.deleted IS FALSE
),
grouped AS (
  SELECT
    pd.toolset_id,
    bool_or(pd.on_active)::boolean AS on_active_deployment,
    count(DISTINCT pd.attachment_id) FILTER (WHERE pd.on_active)::bigint AS attachment_count,
    COALESCE(array_agg(DISTINCT pd.remote_url ORDER BY pd.remote_url) FILTER (WHERE pd.on_active), ARRAY[]::text[])::text[] AS remote_urls,
    COALESCE(array_agg(DISTINCT pd.transport_type ORDER BY pd.transport_type) FILTER (WHERE pd.on_active), ARRAY[]::text[])::text[] AS transport_types,
    COALESCE(bool_or(pd.declares_headers) FILTER (WHERE pd.on_active), FALSE)::boolean AS declares_headers,
    COALESCE(bool_or(pd.requires_oauth) FILTER (WHERE pd.on_active), FALSE)::boolean AS requires_oauth
  FROM proxy_definitions AS pd
  GROUP BY pd.toolset_id
)
SELECT
  t.id AS toolset_id,
  t.organization_id,
  COALESCE(o.name, '')::text AS organization_name,
  COALESCE(o.slug, '')::text AS organization_slug,
  (o.id IS NOT NULL)::boolean AS has_organization_metadata,
  t.project_id,
  p.name AS project_name,
  p.slug AS project_slug,
  t.name AS toolset_name,
  t.slug AS toolset_slug,
  t.user_session_issuer_id,
  t.external_oauth_server_id,
  (t.tool_variations_group_id IS NOT NULL)::boolean AS toolset_has_variations,
  usi.slug AS user_session_issuer_slug,
  COALESCE(t.live, FALSE)::boolean AS live,
  -- The server that kept the toolset's id now proxies a remote MCP server:
  -- the toolset was migrated in place and only waits to be deleted.
  EXISTS (
    SELECT 1
    FROM mcp_servers AS ms
    WHERE ms.id = t.id AND ms.project_id = t.project_id AND ms.deleted IS FALSE
      AND ms.toolset_id IS NULL AND ms.remote_mcp_server_id IS NOT NULL
  )::boolean AS migrated_in_place,
  g.on_active_deployment,
  g.attachment_count,
  g.remote_urls,
  g.transport_types,
  g.declares_headers,
  g.requires_oauth,
  -- The switched server only proxies the upstream, so any other tool or
  -- resource on the toolset would disappear.
  (
    COALESCE(cardinality(lv.resource_urns), 0) > 0
    OR EXISTS (
      SELECT 1
      FROM unnest(lv.tool_urns) AS tool_urn(value)
      WHERE tool_urn.value NOT IN (SELECT pd.tool_urn FROM proxy_definitions AS pd WHERE pd.toolset_id = t.id AND pd.on_active)
    )
  )::boolean AS serves_other_tools,
  (
    SELECT count(*)
    FROM mcp_servers AS ms
    WHERE ms.toolset_id = t.id AND ms.project_id = t.project_id AND ms.deleted IS FALSE
  )::bigint AS server_count,
  wrapper.user_session_issuer_id AS server_user_session_issuer_id,
  (wrapper.tool_variations_group_id IS NOT NULL)::boolean AS server_has_variations,
  (
    SELECT count(*)
    FROM mcp_endpoints AS me
    JOIN mcp_servers AS ms ON ms.id = me.mcp_server_id AND ms.project_id = me.project_id
    WHERE ms.toolset_id = t.id AND ms.project_id = t.project_id AND ms.deleted IS FALSE AND me.deleted IS FALSE
  )::bigint AS endpoint_count,
  -- The caller checks the legacy alias (legacyalias) is one of this server's endpoints.
  COALESCE(wrapper.id, '00000000-0000-0000-0000-000000000000'::uuid)::uuid AS wrapper_id,
  EXISTS (SELECT 1 FROM toolset_prompts AS tp WHERE tp.toolset_id = t.id)::boolean AS has_prompts,
  EXISTS (SELECT 1 FROM slack_app_toolsets AS sat WHERE sat.toolset_id = t.id)::boolean AS has_slack_apps,
  EXISTS (SELECT 1 FROM user_oauth_tokens AS uot WHERE uot.toolset_id = t.id AND uot.deleted IS FALSE)::boolean AS has_user_oauth_tokens,
  EXISTS (
    SELECT 1
    FROM meta_mcp_server_members AS m
    WHERE m.deleted IS FALSE AND m.mcp_server_id = wrapper.id
  )::boolean AS in_gateway,
  COALESCE((
    SELECT array_agg(DISTINCT rsi.id)
    FROM remote_session_client_user_session_issuers AS link
    JOIN remote_session_clients AS rsc
      ON rsc.id = link.remote_session_client_id
     AND rsc.deleted IS FALSE
     -- The serve-time credential resolver's tenancy: the project's own
     -- clients and its organization's organization-level clients.
     AND (rsc.project_id = t.project_id OR (rsc.project_id IS NULL AND rsc.organization_id = t.organization_id))
    JOIN remote_session_issuers AS rsi ON rsi.id = rsc.remote_session_issuer_id AND rsi.deleted IS FALSE
     AND (rsi.project_id = t.project_id OR (rsi.project_id IS NULL AND rsi.organization_id = t.organization_id))
    WHERE link.user_session_issuer_id = t.user_session_issuer_id
  ), ARRAY[]::uuid[])::uuid[] AS remote_session_issuer_ids,
  COALESCE((
    SELECT array_agg(DISTINCT COALESCE(rsi.name, rsi.issuer) ORDER BY COALESCE(rsi.name, rsi.issuer))
    FROM remote_session_client_user_session_issuers AS link
    JOIN remote_session_clients AS rsc
      ON rsc.id = link.remote_session_client_id
     AND rsc.deleted IS FALSE
     -- The serve-time credential resolver's tenancy: the project's own
     -- clients and its organization's organization-level clients.
     AND (rsc.project_id = t.project_id OR (rsc.project_id IS NULL AND rsc.organization_id = t.organization_id))
    JOIN remote_session_issuers AS rsi ON rsi.id = rsc.remote_session_issuer_id AND rsi.deleted IS FALSE
     AND (rsi.project_id = t.project_id OR (rsi.project_id IS NULL AND rsi.organization_id = t.organization_id))
    WHERE link.user_session_issuer_id = t.user_session_issuer_id
  ), ARRAY[]::text[])::text[] AS remote_issuer_names,
  EXISTS (
    SELECT 1
    FROM remote_session_client_user_session_issuers AS link
    JOIN remote_session_clients AS rsc
      ON rsc.id = link.remote_session_client_id
     AND rsc.deleted IS FALSE
     -- The serve-time credential resolver's tenancy: the project's own
     -- clients and its organization's organization-level clients.
     AND (rsc.project_id = t.project_id OR (rsc.project_id IS NULL AND rsc.organization_id = t.organization_id))
    WHERE link.user_session_issuer_id = t.user_session_issuer_id AND rsc.credential_owner = 'self'
  )::boolean AS has_self_owned_clients,
  EXISTS (
    SELECT 1 FROM remote_session_ema_bindings AS b WHERE b.user_session_issuer_id = t.user_session_issuer_id
  )::boolean AS has_ema_bindings,
  -- Another enabled toolset on the issuer still reads its sessions, so
  -- stamping a resource on them could cut it off.
  (
    SELECT count(*)
    FROM toolsets AS other
    JOIN projects AS op ON op.id = other.project_id AND op.deleted IS FALSE
    JOIN mcp_servers AS ocw ON ocw.id = other.id AND ocw.toolset_id = other.id AND ocw.project_id = other.project_id AND ocw.deleted IS FALSE
    WHERE ocw.user_session_issuer_id = t.user_session_issuer_id
      AND other.id <> t.id
      AND other.deleted IS FALSE
      AND ocw.visibility <> 'disabled'
  )::bigint AS other_toolsets_on_issuer,
  COALESCE((
    SELECT array_agg(DISTINCT rtrim(rs.resource, '/') ORDER BY rtrim(rs.resource, '/'))
    FROM remote_sessions AS rs
    WHERE rs.user_session_issuer_id = t.user_session_issuer_id
      AND rs.deleted IS FALSE
      AND rs.resource IS NOT NULL
      AND rs.resource <> ''
  ), ARRAY[]::text[])::text[] AS session_resources,
  -- Other live servers signing in through the issuer. Each must already be a
  -- remote MCP server for the same upstream, or the stamped resource would
  -- send its users' credentials to the wrong one.
  (
    SELECT count(*)
    FROM mcp_servers AS other
    JOIN projects AS op ON op.id = other.project_id AND op.deleted IS FALSE
    WHERE other.user_session_issuer_id = t.user_session_issuer_id
      AND other.deleted IS FALSE
      AND other.id IS DISTINCT FROM wrapper.id
      AND other.remote_mcp_server_id IS NULL
  )::bigint AS other_non_remote_servers_on_issuer,
  COALESCE((
    SELECT array_agg(DISTINCT rtrim(rms.url, '/') ORDER BY rtrim(rms.url, '/'))
    FROM mcp_servers AS other
    JOIN projects AS op ON op.id = other.project_id AND op.deleted IS FALSE
    JOIN remote_mcp_servers AS rms ON rms.id = other.remote_mcp_server_id AND rms.deleted IS FALSE
    WHERE other.user_session_issuer_id = t.user_session_issuer_id
      AND other.deleted IS FALSE
      AND other.id IS DISTINCT FROM wrapper.id
  ), ARRAY[]::text[])::text[] AS other_remote_urls_on_issuer
FROM grouped AS g
JOIN scoped_toolsets AS t ON t.id = g.toolset_id
JOIN latest_versions AS lv ON lv.toolset_id = t.id
JOIN projects AS p ON p.id = t.project_id
LEFT JOIN organization_metadata AS o ON o.id = t.organization_id
LEFT JOIN user_session_issuers AS usi ON usi.id = t.user_session_issuer_id AND usi.deleted IS FALSE
LEFT JOIN LATERAL (
  SELECT ms.id, ms.user_session_issuer_id, ms.tool_variations_group_id
  FROM mcp_servers AS ms
  WHERE ms.toolset_id = t.id AND ms.project_id = t.project_id AND ms.deleted IS FALSE
  ORDER BY (ms.id = t.id) DESC, ms.id
  LIMIT 1
) AS wrapper ON TRUE
ORDER BY organization_name, p.name, t.name, t.id;

-- name: ListHeaderDefinitions :many
-- The proxy and per-tool definitions behind a toolset on its project's active
-- deployment, with their attachment's slug, for copying headers.
WITH active_deployment AS (
  SELECT d.id
  FROM deployments AS d
  JOIN deployment_statuses AS ds ON ds.deployment_id = d.id
  WHERE ds.status = 'completed' AND d.project_id = @project_id
  ORDER BY d.seq DESC
  LIMIT 1
),
latest_version AS (
  SELECT tv.tool_urns
  FROM toolset_versions AS tv
  WHERE tv.toolset_id = @toolset_id AND tv.deleted IS FALSE
  ORDER BY tv.version DESC
  LIMIT 1
)
SELECT ea.slug AS attachment_slug, etd.requires_oauth, etd.header_definitions
FROM latest_version AS lv
CROSS JOIN LATERAL unnest(lv.tool_urns) AS tool_urn(value)
JOIN external_mcp_tool_definitions AS etd ON etd.tool_urn = tool_urn.value
JOIN external_mcp_attachments AS ea ON ea.id = etd.external_mcp_attachment_id
JOIN active_deployment AS ad ON ad.id = ea.deployment_id
WHERE etd.type IN ('proxy', 'direct') AND etd.deleted IS FALSE AND ea.deleted IS FALSE
ORDER BY ea.slug, etd.tool_urn;

-- name: LockToolset :one
SELECT id
FROM toolsets
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
FOR UPDATE;

-- name: GetToolsetServerID :one
-- The one live server fronting the toolset, the row switched in place. It is
-- locked by the switch itself, after its endpoints, as hostedmcp orders locks.
SELECT id
FROM mcp_servers
WHERE toolset_id = @toolset_id AND project_id = @project_id AND deleted IS FALSE;

-- name: SwitchServerToRemote :execrows
-- Swaps the backend in one statement, as mcp_servers_backend_exclusivity_check
-- requires, keeping the row's id, slug, visibility and network access. The
-- upstream issuer is then derived by the shared resync, as for any server.
UPDATE mcp_servers
SET toolset_id = NULL,
  remote_mcp_server_id = @remote_mcp_server_id,
  user_session_issuer_id = COALESCE(user_session_issuer_id, @user_session_issuer_id),
  updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND toolset_id = @toolset_id
  AND deleted IS FALSE;

-- name: CountServerPluginEntries :one
SELECT count(*)::bigint AS entries
FROM plugin_servers AS ps
JOIN plugins AS pl ON pl.id = ps.plugin_id
WHERE pl.project_id = @project_id AND ps.mcp_server_id = @mcp_server_id AND ps.deleted IS FALSE;

-- name: FillRemoteSessionResources :execrows
-- Stamps the upstream URL on the issuer's sessions that recorded no resource.
-- A remote MCP backend only forwards a credential whose resource matches its
-- URL, so without this every user would have to sign in again.
UPDATE remote_sessions AS rs
SET resource = @resource
WHERE rs.user_session_issuer_id = @user_session_issuer_id
  AND rs.deleted IS FALSE
  AND (rs.resource IS NULL OR rs.resource = '')
  AND rs.user_session_issuer_id IN (
    SELECT usi.id
    FROM user_session_issuers AS usi
    WHERE usi.id = @user_session_issuer_id
      AND (usi.organization_id = @organization_id::text OR usi.project_id IN (SELECT p.id FROM projects AS p WHERE p.organization_id = @organization_id::text))
  )
  AND rs.remote_session_client_id IN (
    SELECT link.remote_session_client_id
    FROM remote_session_client_user_session_issuers AS link
    JOIN remote_session_clients AS rsc ON rsc.id = link.remote_session_client_id
    WHERE link.user_session_issuer_id = @user_session_issuer_id
      AND (rsc.project_id = @project_id::uuid OR (rsc.project_id IS NULL AND rsc.organization_id = @organization_id::text))
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
INSERT INTO external_mcp_tool_definitions (external_mcp_attachment_id, tool_urn, remote_url, transport_type, type, requires_oauth, header_definitions)
SELECT id, @tool_urn, @remote_url, 'streamable-http', @type, @requires_oauth, sqlc.narg('header_definitions')::jsonb
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

-- name: MakeClientSelfOwnedFixture :exec
UPDATE remote_session_clients
SET credential_owner = 'self', token_endpoint_auth_method = 'client_secret_basic'
WHERE id = @id;

-- name: SeedEmaBindingFixture :exec
INSERT INTO remote_session_ema_bindings (project_id, organization_id, user_session_issuer_id, remote_session_issuer_id, resource)
SELECT rsc.project_id, rsc.organization_id, @user_session_issuer_id, rsc.remote_session_issuer_id, 'https://mcp.example.com/mcp'
FROM remote_session_clients AS rsc
WHERE rsc.id = @remote_session_client_id;

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
INSERT INTO plugin_servers (plugin_id, project_id, mcp_server_id, display_name)
SELECT id, @project_id, @mcp_server_id, @name FROM plugin
RETURNING id;

-- name: GetPluginServerFixture :one
SELECT mcp_server_id, deleted
FROM plugin_servers
WHERE id = @id;

-- name: SeedToolsetEnvironmentFixture :exec
-- A toolset environment holding one plaintext entry.
WITH environment AS (
  INSERT INTO environments (organization_id, project_id, name, slug)
  VALUES (@organization_id, @project_id, @slug, @slug)
  RETURNING id
),
entry AS (
  INSERT INTO environment_entries (name, value, is_secret, environment_id)
  SELECT @entry_name, @entry_value, FALSE, id FROM environment
)
INSERT INTO toolset_environments (toolset_id, project_id, environment_id)
SELECT @toolset_id, @project_id, id FROM environment;

-- name: ListRemoteHeadersFixture :many
SELECT name, value, value_from_request_header, is_secret
FROM remote_mcp_server_headers
WHERE remote_mcp_server_id = @remote_mcp_server_id AND deleted IS FALSE
ORDER BY name;

-- name: GetToolsetStateFixture :one
SELECT deleted
FROM toolsets
WHERE id = @id;

-- name: CountIssuersAndClientsFixture :one
SELECT
  (SELECT count(*) FROM user_session_issuers AS usi WHERE usi.organization_id = @organization_id::text OR usi.project_id IN (SELECT p.id FROM projects AS p WHERE p.organization_id = @organization_id::text))::bigint AS user_session_issuers,
  (SELECT count(*) FROM remote_session_clients AS rsc WHERE rsc.organization_id = @organization_id::text OR rsc.project_id IN (SELECT p.id FROM projects AS p WHERE p.organization_id = @organization_id::text))::bigint AS remote_session_clients,
  (SELECT count(*) FROM remote_session_client_user_session_issuers)::bigint AS client_links;
