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

-- name: CountActiveSessionsByDay :many
-- A credential keeps its id across refresh rotation, so it is one session. A
-- row lives from creation until deleted or its deadline; a dashboard mint has
-- no refresh, so its access token's. An organization session counts unless its
-- resource or client ties it to another project only.
WITH days AS (
  SELECT day_start::timestamptz AS day_start
  FROM generate_series(@from_day::timestamptz, @last_day::timestamptz, '1 day'::interval) AS day_start
),
endpoints AS (
  SELECT endpoint.project_id, endpoint.slug, domain.domain AS host, COALESCE(endpoint.is_domain_root, FALSE) AS is_root,
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
    AND LEAST(COALESCE(session.deleted_at, 'infinity'::timestamptz), CASE WHEN session.refresh_token_hash LIKE @dashboard_mint_pattern::text THEN session.expires_at ELSE session.refresh_expires_at END) >= @live_since::timestamptz
),
scoped AS (
  SELECT COALESCE(session.credential_id, session.id) AS credential,
         session.created_at AS started_at,
         LEAST(COALESCE(session.deleted_at, 'infinity'::timestamptz), CASE WHEN session.refresh_token_hash LIKE @dashboard_mint_pattern::text THEN session.expires_at ELSE session.refresh_expires_at END) AS ended_at,
         session.deleted
  FROM candidates AS session
  WHERE session.project_id = @project_id
     OR NOT COALESCE(
          (session.resource LIKE @inference_resource_pattern::text AND session.resource <> @project_inference_resource::text)
          OR EXISTS (SELECT 1 FROM user_session_clients AS client
                     WHERE client.id = session.user_session_client_id AND client.organization_id = @organization_id::text
                       AND client.project_id <> @project_id)
          OR (EXISTS (SELECT 1 FROM endpoints AS endpoint WHERE endpoint.project_id <> @project_id AND endpoint.gone_at > session.created_at
                        AND (endpoint.host IS NULL OR endpoint.host = session.resource_host)
                        AND CASE WHEN endpoint.is_root THEN session.resource_path IN ('', '/')
                                 ELSE endpoint.slug = substring(session.resource_path from '/([^/]+)/?$') END)
              AND NOT EXISTS (SELECT 1 FROM endpoints AS endpoint WHERE endpoint.project_id = @project_id AND endpoint.gone_at > session.created_at
                        AND (endpoint.host IS NULL OR endpoint.host = session.resource_host)
                        AND CASE WHEN endpoint.is_root THEN session.resource_path IN ('', '/')
                                 ELSE endpoint.slug = substring(session.resource_path from '/([^/]+)/?$') END)),
          FALSE)
)
SELECT days.day_start, COUNT(DISTINCT scoped.credential)::bigint AS active_sessions
FROM days
LEFT JOIN scoped
  ON scoped.started_at < days.day_start + '1 day'::interval
  AND scoped.ended_at > days.day_start
  AND scoped.started_at < @to_day::timestamptz
  AND scoped.ended_at > @from_day::timestamptz
GROUP BY days.day_start
ORDER BY days.day_start;

-- name: CountActiveSessionsNow :one
WITH endpoints AS (
  SELECT endpoint.project_id, endpoint.slug, domain.domain AS host, COALESCE(endpoint.is_domain_root, FALSE) AS is_root,
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
    AND LEAST(COALESCE(session.deleted_at, 'infinity'::timestamptz), CASE WHEN session.refresh_token_hash LIKE @dashboard_mint_pattern::text THEN session.expires_at ELSE session.refresh_expires_at END) >= @live_since::timestamptz
),
scoped AS (
  SELECT COALESCE(session.credential_id, session.id) AS credential,
         session.created_at AS started_at,
         LEAST(COALESCE(session.deleted_at, 'infinity'::timestamptz), CASE WHEN session.refresh_token_hash LIKE @dashboard_mint_pattern::text THEN session.expires_at ELSE session.refresh_expires_at END) AS ended_at,
         session.deleted
  FROM candidates AS session
  WHERE session.project_id = @project_id
     OR NOT COALESCE(
          (session.resource LIKE @inference_resource_pattern::text AND session.resource <> @project_inference_resource::text)
          OR EXISTS (SELECT 1 FROM user_session_clients AS client
                     WHERE client.id = session.user_session_client_id AND client.organization_id = @organization_id::text
                       AND client.project_id <> @project_id)
          OR (EXISTS (SELECT 1 FROM endpoints AS endpoint WHERE endpoint.project_id <> @project_id AND endpoint.gone_at > session.created_at
                        AND (endpoint.host IS NULL OR endpoint.host = session.resource_host)
                        AND CASE WHEN endpoint.is_root THEN session.resource_path IN ('', '/')
                                 ELSE endpoint.slug = substring(session.resource_path from '/([^/]+)/?$') END)
              AND NOT EXISTS (SELECT 1 FROM endpoints AS endpoint WHERE endpoint.project_id = @project_id AND endpoint.gone_at > session.created_at
                        AND (endpoint.host IS NULL OR endpoint.host = session.resource_host)
                        AND CASE WHEN endpoint.is_root THEN session.resource_path IN ('', '/')
                                 ELSE endpoint.slug = substring(session.resource_path from '/([^/]+)/?$') END)),
          FALSE)
)
SELECT COUNT(DISTINCT scoped.credential)::bigint AS active_sessions
FROM scoped
WHERE scoped.deleted IS FALSE
  AND scoped.ended_at > clock_timestamp();

-- name: AverageEndedSessionSeconds :one
-- Mean length, first sign-in to expiry or revocation, of sessions that ended
-- in the range and have no live row. Only credentials with a row ending after
-- live_since are read, all of their rows included.
WITH endpoints AS (
  SELECT endpoint.project_id, endpoint.slug, domain.domain AS host, COALESCE(endpoint.is_domain_root, FALSE) AS is_root,
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
scoped AS (
  SELECT COALESCE(session.credential_id, session.id) AS credential,
         session.created_at AS started_at,
         LEAST(COALESCE(session.deleted_at, 'infinity'::timestamptz), CASE WHEN session.refresh_token_hash LIKE @dashboard_mint_pattern::text THEN session.expires_at ELSE session.refresh_expires_at END) AS ended_at,
         session.deleted
  FROM candidates AS session
  WHERE session.project_id = @project_id
     OR NOT COALESCE(
          (session.resource LIKE @inference_resource_pattern::text AND session.resource <> @project_inference_resource::text)
          OR EXISTS (SELECT 1 FROM user_session_clients AS client
                     WHERE client.id = session.user_session_client_id AND client.organization_id = @organization_id::text
                       AND client.project_id <> @project_id)
          OR (EXISTS (SELECT 1 FROM endpoints AS endpoint WHERE endpoint.project_id <> @project_id AND endpoint.gone_at > session.created_at
                        AND (endpoint.host IS NULL OR endpoint.host = session.resource_host)
                        AND CASE WHEN endpoint.is_root THEN session.resource_path IN ('', '/')
                                 ELSE endpoint.slug = substring(session.resource_path from '/([^/]+)/?$') END)
              AND NOT EXISTS (SELECT 1 FROM endpoints AS endpoint WHERE endpoint.project_id = @project_id AND endpoint.gone_at > session.created_at
                        AND (endpoint.host IS NULL OR endpoint.host = session.resource_host)
                        AND CASE WHEN endpoint.is_root THEN session.resource_path IN ('', '/')
                                 ELSE endpoint.slug = substring(session.resource_path from '/([^/]+)/?$') END)),
          FALSE)
),
lives AS (
  SELECT scoped.credential, MIN(scoped.started_at) AS started_at, MAX(scoped.ended_at) AS ended_at
  FROM scoped
  GROUP BY scoped.credential
)
SELECT COALESCE(AVG(EXTRACT(EPOCH FROM lives.ended_at - lives.started_at)), -1)::float8 AS average_seconds
FROM lives
WHERE lives.ended_at >= @from_time::timestamptz
  AND lives.ended_at < @to_time::timestamptz
  AND lives.ended_at <= clock_timestamp();
