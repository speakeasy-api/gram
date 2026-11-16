-- A remote session's organization is its client's organization: the
-- organization of the client's project, otherwise the client's own
-- organization_id. Only organizations with an organization_metadata row
-- count, because remote_sessions.organization_id references that table.

-- name: CountSessions :one
-- Counts remote sessions, including soft-deleted ones, by what the backfill
-- can do with them. Only pending rows are written; the rest are reported.
SELECT
  COUNT(*) FILTER (
    WHERE s.organization_id IS NULL AND org.id IS NOT NULL
  )::bigint AS pending,
  COUNT(*) FILTER (
    WHERE s.organization_id IS NULL AND c.project_id IS NULL AND c.organization_id IS NULL
  )::bigint AS global_client,
  COUNT(*) FILTER (
    WHERE s.organization_id IS NULL AND org.id IS NULL
      AND (c.project_id IS NOT NULL OR c.organization_id IS NOT NULL)
  )::bigint AS missing_organization,
  COUNT(*) FILTER (
    WHERE s.organization_id IS NOT NULL
      AND s.organization_id IS DISTINCT FROM COALESCE(p.organization_id, c.organization_id)
  )::bigint AS mismatched
FROM remote_sessions AS s
JOIN remote_session_clients AS c ON c.id = s.remote_session_client_id
LEFT JOIN projects AS p ON p.id = c.project_id
LEFT JOIN organization_metadata AS org ON org.id = COALESCE(p.organization_id, c.organization_id);

-- name: ListPendingOrganizations :many
-- Organizations that own at least one remote session still missing its
-- organization_id.
SELECT DISTINCT org.id
FROM remote_sessions AS s
JOIN remote_session_clients AS c ON c.id = s.remote_session_client_id
LEFT JOIN projects AS p ON p.id = c.project_id
JOIN organization_metadata AS org ON org.id = COALESCE(p.organization_id, c.organization_id)
WHERE s.organization_id IS NULL
ORDER BY org.id;

-- name: FillOrganizationBatch :execrows
-- Sets organization_id on up to batch_size of one organization's sessions,
-- soft-deleted ones included, and soft-deleted clients' sessions too. It
-- starts from the organization's clients (project clients first, as in the
-- lookup above) and reaches their sessions through the client index, rather
-- than scanning every pending session. updated_at is left alone: it is the
-- refresh compare-and-swap version and the keepalive due clock.
WITH organization_clients AS (
  SELECT c.id
  FROM remote_session_clients AS c
  JOIN projects AS p ON p.id = c.project_id
  WHERE p.organization_id = @organization_id::text
  UNION ALL
  SELECT c.id
  FROM remote_session_clients AS c
  WHERE c.project_id IS NULL
    AND c.organization_id = @organization_id::text
)
UPDATE remote_sessions AS s
SET organization_id = @organization_id::text
WHERE s.organization_id IS NULL
  AND s.id IN (
    SELECT pending.id
    FROM remote_sessions AS pending
    WHERE pending.remote_session_client_id IN (SELECT id FROM organization_clients)
      AND pending.organization_id IS NULL
    LIMIT @batch_size::int
  );
