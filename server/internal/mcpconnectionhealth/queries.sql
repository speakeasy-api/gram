-- name: GetProviderIssuer :one
-- A project's issuer, or an organization issuer of the project's organization.
SELECT issuer.id, issuer.slug, issuer.session_duration
FROM user_session_issuers AS issuer
WHERE issuer.id = @issuer_id
  AND issuer.deleted IS FALSE
  AND (
    issuer.project_id = @project_id
    OR (issuer.project_id IS NULL AND issuer.organization_id = @organization_id::text)
  );

-- name: ListIssuerServerIDs :many
-- Servers and gateways in the project attached to the issuer, deleted ones
-- included so their earlier attempts stay attributed.
SELECT server.id
FROM mcp_servers AS server
WHERE server.project_id = @project_id
  AND server.user_session_issuer_id = @issuer_id
UNION ALL
SELECT gateway.id
FROM meta_mcp_servers AS gateway
WHERE gateway.project_id = @project_id
  AND gateway.organization_id = @organization_id::text
  AND gateway.user_session_issuer_id = @issuer_id;

-- name: ListServerNames :many
SELECT server.id, COALESCE(server.name, server.slug, '')::text AS name, 'mcp'::text AS kind
FROM mcp_servers AS server
WHERE server.project_id = @project_id
  AND server.deleted IS FALSE
  AND server.id = ANY(@ids::uuid[])
UNION ALL
SELECT gateway.id, gateway.name, 'gateway'::text AS kind
FROM meta_mcp_servers AS gateway
WHERE gateway.project_id = @project_id
  AND gateway.organization_id = @organization_id::text
  AND gateway.deleted IS FALSE
  AND gateway.id = ANY(@ids::uuid[])
UNION ALL
SELECT toolset.id, toolset.name, 'toolset'::text AS kind
FROM toolsets AS toolset
WHERE toolset.project_id = @project_id
  AND toolset.organization_id = @organization_id::text
  AND toolset.deleted IS FALSE
  AND toolset.id = ANY(@ids::uuid[])
UNION ALL
SELECT issuer.id, issuer.slug, 'authorization_server'::text AS kind
FROM user_session_issuers AS issuer
WHERE (issuer.project_id = @project_id OR (issuer.project_id IS NULL AND issuer.organization_id = @organization_id::text))
  AND issuer.deleted IS FALSE
  AND issuer.id = ANY(@ids::uuid[]);

-- name: ListMemberNames :many
-- Members of the organization by user id. Users who left keep no name.
SELECT member.id, member.display_name, member.email
FROM users AS member
JOIN organization_user_relationships AS relationship
  ON relationship.user_id = member.id
WHERE relationship.organization_id = @organization_id::text
  AND relationship.deleted IS FALSE
  AND member.id = ANY(@ids::text[]);

-- name: GetSessionActivity :many
-- A credential keeps its id across refresh rotation, so it is one session. A
-- row lives from creation until deleted or its deadline; a dashboard mint has
-- no refresh, so its access token's. An organization session counts unless its
-- resource or client ties it to another project only. Credentials with a row
-- ending after live_since are read, all of their rows included.
WITH endpoints AS (
  SELECT endpoint.project_id, endpoint.slug, domain.domain AS host, COALESCE(endpoint.is_domain_root, FALSE) AS is_root,
         endpoint.created_at AS live_from,
         LEAST(COALESCE(endpoint.deleted_at, 'infinity'::timestamptz), COALESCE(domain.deleted_at, 'infinity'::timestamptz)) AS gone_at
  FROM mcp_endpoints AS endpoint
  JOIN projects AS project ON project.id = endpoint.project_id
  LEFT JOIN custom_domains AS domain ON domain.id = endpoint.custom_domain_id AND domain.organization_id = @organization_id::text
  WHERE project.organization_id = @organization_id::text
),
candidates AS (
  SELECT session.*,
         substring(session.resource from '^https?://([^/?#]+)') AS resource_host,
         regexp_replace(regexp_replace(session.resource, '^https?://[^/?#]+', ''), '[?#].*$', '') AS resource_path
  FROM user_sessions AS session
  WHERE session.user_session_issuer_id = @issuer_id
    AND (session.project_id = @project_id OR (session.project_id IS NULL AND session.organization_id = @organization_id::text))
    AND COALESCE(session.credential_id, session.id) IN (
      SELECT COALESCE(recent.credential_id, recent.id)
      FROM user_sessions AS recent
      WHERE recent.user_session_issuer_id = @issuer_id
        AND LEAST(COALESCE(recent.deleted_at, 'infinity'::timestamptz), CASE WHEN recent.refresh_token_hash LIKE @dashboard_mint_pattern::text THEN recent.expires_at ELSE recent.refresh_expires_at END) >= @live_since::timestamptz
    )
),
addressed AS (
  SELECT session.*,
         EXISTS (SELECT 1 FROM custom_domains AS domain WHERE domain.organization_id = @organization_id::text AND domain.domain = session.resource_host) AS on_custom_domain,
         substring(session.resource_path from '/mcp/([^/]+)/?$') AS endpoint_slug,
         substring(session.resource_path from '/mcp/gateway/[^/]+/projects/([^/]+)/?$') AS gateway_project_slug,
         substring(session.resource from ':([0-9a-f-]{36})$') AS resource_project_id
  FROM candidates AS session
),
scoped AS (
  SELECT COALESCE(session.credential_id, session.id) AS credential,
         session.created_at AS started_at,
         LEAST(COALESCE(session.deleted_at, 'infinity'::timestamptz), CASE WHEN session.refresh_token_hash LIKE @dashboard_mint_pattern::text THEN session.expires_at ELSE session.refresh_expires_at END) AS ended_at,
         session.deleted
  FROM addressed AS session
  WHERE session.project_id = @project_id
     OR NOT COALESCE(
          EXISTS (SELECT 1 FROM projects AS project WHERE project.organization_id = @organization_id::text
                    AND project.id::text = session.resource_project_id AND project.id <> @project_id)
          OR EXISTS (SELECT 1 FROM user_session_clients AS client
                     WHERE client.id = session.user_session_client_id AND client.organization_id = @organization_id::text
                       AND client.project_id <> @project_id)
          OR session.gateway_project_slug <> (SELECT project.slug FROM projects AS project WHERE project.id = @project_id AND project.organization_id = @organization_id::text)
          OR (EXISTS (SELECT 1 FROM endpoints AS endpoint WHERE endpoint.project_id <> @project_id AND endpoint.live_from <= session.created_at AND endpoint.gone_at > session.created_at
                        AND (endpoint.host = session.resource_host OR (endpoint.host IS NULL AND NOT session.on_custom_domain))
                        AND ((endpoint.is_root AND session.resource_path IN ('', '/')) OR endpoint.slug = session.endpoint_slug))
              AND NOT EXISTS (SELECT 1 FROM endpoints AS endpoint WHERE endpoint.project_id = @project_id AND endpoint.live_from <= session.created_at AND endpoint.gone_at > session.created_at
                        AND (endpoint.host = session.resource_host OR (endpoint.host IS NULL AND NOT session.on_custom_domain))
                        AND ((endpoint.is_root AND session.resource_path IN ('', '/')) OR endpoint.slug = session.endpoint_slug))),
          FALSE)
),
lives AS (
  SELECT scoped.credential, MIN(scoped.started_at) AS started_at, MAX(scoped.ended_at) AS ended_at
  FROM scoped
  GROUP BY scoped.credential
),
days AS (
  SELECT day_start::timestamptz AS day_start
  FROM generate_series(@from_day::timestamptz, @last_day::timestamptz, '24 hours'::interval) AS day_start
)
SELECT days.day_start,
       COUNT(DISTINCT scoped.credential)::bigint AS active_sessions,
       (SELECT COUNT(DISTINCT live.credential) FROM scoped AS live
        WHERE live.deleted IS FALSE AND live.ended_at > clock_timestamp())::bigint AS active_now,
       (SELECT COALESCE(AVG(EXTRACT(EPOCH FROM ended.ended_at - ended.started_at)), -1) FROM lives AS ended
        WHERE ended.ended_at >= @average_from::timestamptz
          AND ended.ended_at < @average_to::timestamptz
          AND ended.ended_at <= clock_timestamp())::float8 AS average_seconds
FROM days
LEFT JOIN scoped
  ON scoped.started_at < days.day_start + '24 hours'::interval
  AND scoped.ended_at > days.day_start
GROUP BY days.day_start
ORDER BY days.day_start;
