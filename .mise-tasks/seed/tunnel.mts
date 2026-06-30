#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Seed the local Postgres MCP tunnel fixture"
//MISE dir="{{ config_root }}"

import { pathToFileURL } from "node:url";
import { $ } from "zx";

const SEEDED_PROJECT_SLUG = "ecommerce-api";
const SEEDED_TUNNEL_SOURCE_NAME = "Seeded Local Postgres MCP";
const SEEDED_TUNNEL_KEY =
  "gram_tunnel_localpostgresmcpseedkey000000000000000000000000000000";
const SEEDED_TUNNEL_KEY_HASH =
  "da47d4681359b43e76c22b686ddeb16320916c7510550c03550b5114634d63d5";
const SEEDED_TUNNEL_KEY_PREFIX = "gram_tunnel_local";

type TunnelFixture = {
  sourceId: string;
  endpointSlug: string;
  mcpServerId: string;
};

export async function seedTunnel() {
  const dbUser = process.env.DB_USER || "gram";
  const dbName = process.env.DB_NAME || "gram";
  const sql = `
WITH project AS (
  SELECT id, slug
  FROM projects
  WHERE slug = :'project_slug'
    AND deleted IS FALSE
  ORDER BY created_at DESC
  LIMIT 1
),
source AS (
  INSERT INTO tunnelled_mcp_servers (project_id, name, key_hash, key_prefix, status)
  SELECT id, :'source_name', :'key_hash', :'key_prefix', 'created'
  FROM project
  ON CONFLICT (project_id, name) WHERE deleted IS FALSE
  DO UPDATE SET
    key_hash = EXCLUDED.key_hash,
    key_prefix = EXCLUDED.key_prefix,
    status = 'created',
    agent_version = NULL,
    last_seen_at = NULL,
    updated_at = clock_timestamp()
  RETURNING id, project_id
),
issuer AS (
  INSERT INTO user_session_issuers (project_id, slug, authn_challenge_mode, session_duration)
  SELECT id, slug || '-gram-postgres-mcp-issuer', 'interactive', '14 days'::interval
  FROM project
  ON CONFLICT (project_id, slug) WHERE deleted IS FALSE
  DO UPDATE SET
    authn_challenge_mode = EXCLUDED.authn_challenge_mode,
    session_duration = EXCLUDED.session_duration,
    updated_at = clock_timestamp()
  RETURNING id, project_id
),
mcp_server AS (
  INSERT INTO mcp_servers (
    project_id,
    name,
    slug,
    user_session_issuer_id,
    tunnelled_mcp_server_id,
    visibility
  )
  SELECT source.project_id, :'source_name', 'seeded-local-postgres-mcp', issuer.id, source.id, 'private'
  FROM source
  JOIN issuer ON issuer.project_id = source.project_id
  ON CONFLICT (project_id, slug) WHERE deleted IS FALSE
  DO UPDATE SET
    name = EXCLUDED.name,
    user_session_issuer_id = EXCLUDED.user_session_issuer_id,
    remote_mcp_server_id = NULL,
    tunnelled_mcp_server_id = EXCLUDED.tunnelled_mcp_server_id,
    toolset_id = NULL,
    visibility = EXCLUDED.visibility,
    updated_at = clock_timestamp()
  RETURNING id, project_id
),
endpoint AS (
  INSERT INTO mcp_endpoints (project_id, mcp_server_id, slug)
  SELECT mcp_server.project_id, mcp_server.id, project.slug || '-gram-postgres-mcp'
  FROM mcp_server
  JOIN project ON project.id = mcp_server.project_id
  ON CONFLICT (slug) WHERE custom_domain_id IS NULL AND deleted IS FALSE
  DO UPDATE SET
    project_id = EXCLUDED.project_id,
    mcp_server_id = EXCLUDED.mcp_server_id,
    updated_at = clock_timestamp()
  RETURNING slug
)
SELECT json_build_object(
  'sourceId', source.id::text,
  'endpointSlug', endpoint.slug,
  'mcpServerId', mcp_server.id::text
)::text
FROM source, endpoint, mcp_server;
`;

  const result = await $({
    input: sql,
  })`docker compose exec -T gram-db psql -U ${dbUser} -d ${dbName} -v ON_ERROR_STOP=1 -v project_slug=${SEEDED_PROJECT_SLUG} -v source_name=${SEEDED_TUNNEL_SOURCE_NAME} -v key_hash=${SEEDED_TUNNEL_KEY_HASH} -v key_prefix=${SEEDED_TUNNEL_KEY_PREFIX} -tA -f -`.quiet();

  const output = result.stdout.trim();
  if (!output) {
    throw new Error(
      `Project '${SEEDED_PROJECT_SLUG}' was not found. Run 'mise run seed' first.`,
    );
  }

  const fixture = JSON.parse(output) as TunnelFixture;
  await $`mise set --file mise.local.toml TUNNEL_LOCAL_ID=${fixture.sourceId} TUNNEL_LOCAL_KEY=${SEEDED_TUNNEL_KEY} TUNNEL_LOCAL_MCP_ENDPOINT_SLUG=${fixture.endpointSlug} TUNNEL_LOCAL_MCP_SERVER_ID=${fixture.mcpServerId}`;
  console.log(
    `Seeded local Postgres MCP tunnel at /mcp/${fixture.endpointSlug}`,
  );
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  await seedTunnel();
}
