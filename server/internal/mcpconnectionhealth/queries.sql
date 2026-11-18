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
  AND server.id = ANY(@ids::uuid[])
UNION ALL
SELECT gateway.id, gateway.name, 'gateway'::text AS kind
FROM meta_mcp_servers AS gateway
WHERE gateway.project_id = @project_id
  AND gateway.organization_id = @organization_id::text
  AND gateway.id = ANY(@ids::uuid[])
UNION ALL
SELECT toolset.id, toolset.name, 'toolset'::text AS kind
FROM toolsets AS toolset
WHERE toolset.project_id = @project_id
  AND toolset.organization_id = @organization_id::text
  AND toolset.id = ANY(@ids::uuid[])
UNION ALL
SELECT issuer.id, issuer.slug, 'authorization_server'::text AS kind
FROM user_session_issuers AS issuer
WHERE (issuer.project_id = @project_id OR (issuer.project_id IS NULL AND issuer.organization_id = @organization_id::text))
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
-- A credential keeps its id across refresh rotation, so it is one session.
-- A row lives from its creation until it is deleted or reaches its deadline.
WITH days AS (
  SELECT day_start::timestamptz AS day_start
  FROM generate_series(@from_day::timestamptz, @last_day::timestamptz, '1 day'::interval) AS day_start
),
lives AS (
  SELECT COALESCE(session.credential_id, session.id) AS credential,
         session.created_at AS started_at,
         LEAST(COALESCE(session.deleted_at, 'infinity'::timestamptz), session.refresh_expires_at) AS ended_at
  FROM user_sessions AS session
  WHERE session.user_session_issuer_id = @issuer_id
    AND (session.project_id = @project_id OR (session.project_id IS NULL AND session.organization_id = @organization_id::text))
    AND session.created_at < @to_day::timestamptz
    AND LEAST(COALESCE(session.deleted_at, 'infinity'::timestamptz), session.refresh_expires_at) > @from_day::timestamptz
)
SELECT days.day_start, COUNT(DISTINCT lives.credential)::bigint AS active_sessions
FROM days
LEFT JOIN lives
  ON lives.started_at < days.day_start + '1 day'::interval
  AND lives.ended_at > days.day_start
GROUP BY days.day_start
ORDER BY days.day_start;

-- name: CountActiveSessionsNow :one
SELECT COUNT(DISTINCT COALESCE(session.credential_id, session.id))::bigint AS active_sessions
FROM user_sessions AS session
WHERE session.user_session_issuer_id = @issuer_id
  AND (session.project_id = @project_id OR (session.project_id IS NULL AND session.organization_id = @organization_id::text))
  AND session.deleted IS FALSE
  AND session.refresh_expires_at > clock_timestamp();

-- name: AverageEndedSessionSeconds :one
-- Mean length, first sign-in to expiry or revocation, of sessions that ended
-- in the range and have no live row.
WITH ended AS (
  SELECT COALESCE(session.credential_id, session.id) AS credential
  FROM user_sessions AS session
  WHERE session.user_session_issuer_id = @issuer_id
    AND (session.project_id = @project_id OR (session.project_id IS NULL AND session.organization_id = @organization_id::text))
    AND LEAST(COALESCE(session.deleted_at, 'infinity'::timestamptz), session.refresh_expires_at) >= @from_time::timestamptz
    AND LEAST(COALESCE(session.deleted_at, 'infinity'::timestamptz), session.refresh_expires_at) < @to_time::timestamptz
    AND LEAST(COALESCE(session.deleted_at, 'infinity'::timestamptz), session.refresh_expires_at) <= clock_timestamp()
),
lives AS (
  SELECT COALESCE(session.credential_id, session.id) AS credential,
         MIN(session.created_at) AS started_at,
         MAX(LEAST(COALESCE(session.deleted_at, 'infinity'::timestamptz), session.refresh_expires_at)) AS ended_at
  FROM user_sessions AS session
  WHERE session.user_session_issuer_id = @issuer_id
    AND (session.project_id = @project_id OR (session.project_id IS NULL AND session.organization_id = @organization_id::text))
    AND COALESCE(session.credential_id, session.id) IN (SELECT credential FROM ended)
  GROUP BY 1
)
SELECT COALESCE(AVG(EXTRACT(EPOCH FROM lives.ended_at - lives.started_at)), -1)::float8 AS average_seconds
FROM lives
WHERE lives.ended_at >= @from_time::timestamptz
  AND lives.ended_at < @to_time::timestamptz
  AND lives.ended_at <= clock_timestamp();
