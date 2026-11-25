-- Candidates are global staff review data; sightings carry the issuer's tenancy.

-- Upserts the candidate and the issuer's sighting. Status is never written, so
-- a decided candidate keeps its decision; only a pending one takes an earlier
-- first sighting, or a client name from an observation at least as recent as
-- its last sighting (a missing name is always filled). A URL that is not yet a
-- candidate is created only while the issuer has fewer than
-- max_pending_per_issuer pending sightings. Writes nothing when that cap skips
-- it or the issuer is gone. The cap is soft: concurrent writes can overshoot
-- it slightly.
-- name: RecordSighting :execrows
WITH live_issuer AS (
  -- A project-level issuer may leave organization_id unset; its project names it.
  SELECT iss.id, COALESCE(iss.organization_id, project.organization_id) AS organization_id
  FROM user_session_issuers AS iss
  LEFT JOIN projects AS project ON project.id = iss.project_id
  WHERE iss.id = @user_session_issuer_id::uuid
    AND iss.deleted IS FALSE
),
existing AS (
  SELECT c.id
  FROM cimd_client_candidates AS c
  WHERE c.url = @url::text
),
issuer_pending AS (
  SELECT count(*) AS n
  FROM (
    SELECT 1
    FROM cimd_client_candidate_sightings AS s
    JOIN cimd_client_candidates AS c ON c.id = s.candidate_id
    WHERE s.user_session_issuer_id = @user_session_issuer_id::uuid
      AND c.status = 'pending'
    LIMIT @max_pending_per_issuer::bigint
  ) AS capped
),
candidate AS (
  INSERT INTO cimd_client_candidates AS c (url, client_name, first_seen_at, last_seen_at)
  SELECT @url::text, sqlc.narg('client_name')::text, @first_seen_at::timestamptz, @last_seen_at::timestamptz
  WHERE EXISTS (SELECT 1 FROM live_issuer)
    AND (
      EXISTS (SELECT 1 FROM existing)
      OR (SELECT n FROM issuer_pending) < @max_pending_per_issuer::bigint
    )
  ON CONFLICT (url) DO UPDATE SET
    client_name = CASE
      WHEN c.status <> 'pending' THEN c.client_name
      WHEN c.client_name IS NULL THEN EXCLUDED.client_name
      WHEN EXCLUDED.client_name IS NOT NULL AND EXCLUDED.last_seen_at >= c.last_seen_at THEN EXCLUDED.client_name
      ELSE c.client_name
    END,
    first_seen_at = CASE WHEN c.status = 'pending' THEN LEAST(c.first_seen_at, EXCLUDED.first_seen_at) ELSE c.first_seen_at END,
    last_seen_at = GREATEST(c.last_seen_at, EXCLUDED.last_seen_at),
    updated_at = clock_timestamp()
  RETURNING c.id
)
INSERT INTO cimd_client_candidate_sightings AS s (candidate_id, user_session_issuer_id, organization_id, last_seen_at)
SELECT candidate.id, live_issuer.id, live_issuer.organization_id, @last_seen_at::timestamptz
FROM candidate, live_issuer
ON CONFLICT (candidate_id, user_session_issuer_id) DO UPDATE SET
  last_seen_at = GREATEST(s.last_seen_at, EXCLUDED.last_seen_at),
  updated_at = clock_timestamp();

-- Pages live issuers by primary key for the backfill.
-- name: ListIssuersForBackfill :many
SELECT iss.id, iss.client_id_metadata_admission_mode
FROM user_session_issuers AS iss
WHERE iss.deleted IS FALSE
  AND iss.id > @after_id::uuid
ORDER BY iss.id
LIMIT @page_size::int;

-- Pending sightings per issuer, served by
-- cimd_client_candidate_sightings_user_session_issuer_id_idx.
-- name: CountPendingSightingsByIssuer :many
SELECT s.user_session_issuer_id, count(*)::bigint AS pending
FROM cimd_client_candidate_sightings AS s
JOIN cimd_client_candidates AS c ON c.id = s.candidate_id
WHERE s.user_session_issuer_id = ANY(@user_session_issuer_ids::uuid[])
  AND c.status = 'pending'
GROUP BY s.user_session_issuer_id;

-- Pages the live CIMD client rows of the given issuers by
-- (user_session_issuer_id, id), served by user_session_clients_issuer_id_key.
-- name: ListCIMDClientsForBackfill :many
SELECT
  cl.id,
  cl.user_session_issuer_id,
  cl.client_id_metadata_uri::text AS url,
  cl.client_name,
  cl.created_at,
  COALESCE(cl.client_id_metadata_fetched_at, cl.updated_at)::timestamptz AS last_seen_at
FROM user_session_clients AS cl
WHERE cl.user_session_issuer_id = ANY(@user_session_issuer_ids::uuid[])
  AND (cl.user_session_issuer_id, cl.id) > (@after_issuer_id::uuid, @after_id::uuid)
  AND cl.client_id_metadata_uri IS NOT NULL
  AND cl.deleted IS FALSE
ORDER BY cl.user_session_issuer_id, cl.id
LIMIT @page_size::int;

-- name: ListExistingCandidates :many
SELECT c.url, c.status
FROM cimd_client_candidates AS c
WHERE c.url = ANY(@urls::text[]);

-- Existing sightings among the given URLs and issuers; the caller keeps the
-- pairs it asked about.
-- name: ListSightingsForBackfill :many
SELECT c.url, s.user_session_issuer_id
FROM cimd_client_candidates AS c
JOIN cimd_client_candidate_sightings AS s ON s.candidate_id = c.id
WHERE c.url = ANY(@urls::text[])
  AND s.user_session_issuer_id = ANY(@user_session_issuer_ids::uuid[]);

-- Upserts one backfill batch with the same rules as RecordSighting, minus the
-- cap, which the caller applies. Rows are positional across the arrays; each
-- (url, issuer) pair appears once. Rows of an issuer that is gone write
-- nothing. Returns the sightings written.
-- name: BackfillSightings :execrows
WITH batch AS (
  -- Set-returning functions in one select list advance together.
  SELECT
    unnest(@urls::text[]) AS url,
    unnest(@client_names::text[]) AS client_name,
    unnest(@first_seen_ats::timestamptz[]) AS first_seen_at,
    unnest(@last_seen_ats::timestamptz[]) AS last_seen_at,
    unnest(@user_session_issuer_ids::uuid[]) AS user_session_issuer_id
),
input AS (
  SELECT batch.*, COALESCE(iss.organization_id, project.organization_id) AS organization_id
  FROM batch
  JOIN user_session_issuers AS iss ON iss.id = batch.user_session_issuer_id
  LEFT JOIN projects AS project ON project.id = iss.project_id
  WHERE iss.deleted IS FALSE
),
per_url AS (
  SELECT
    input.url,
    (array_agg(input.client_name ORDER BY input.last_seen_at DESC) FILTER (WHERE input.client_name <> ''))[1] AS client_name,
    -- When the chosen name was seen, which the name rule compares rather
    -- than the newest row of the URL.
    max(input.last_seen_at) FILTER (WHERE input.client_name <> '') AS name_seen_at,
    min(input.first_seen_at) AS first_seen_at,
    max(input.last_seen_at) AS last_seen_at
  FROM input
  GROUP BY input.url
),
candidate AS (
  INSERT INTO cimd_client_candidates AS c (url, client_name, first_seen_at, last_seen_at)
  SELECT per_url.url, per_url.client_name, per_url.first_seen_at, per_url.last_seen_at
  FROM per_url
  ON CONFLICT (url) DO UPDATE SET
    client_name = CASE
      WHEN c.status <> 'pending' THEN c.client_name
      WHEN c.client_name IS NULL THEN EXCLUDED.client_name
      WHEN EXCLUDED.client_name IS NOT NULL
        AND (SELECT per_url.name_seen_at FROM per_url WHERE per_url.url = EXCLUDED.url) >= c.last_seen_at
        THEN EXCLUDED.client_name
      ELSE c.client_name
    END,
    first_seen_at = CASE WHEN c.status = 'pending' THEN LEAST(c.first_seen_at, EXCLUDED.first_seen_at) ELSE c.first_seen_at END,
    last_seen_at = GREATEST(c.last_seen_at, EXCLUDED.last_seen_at),
    updated_at = clock_timestamp()
  RETURNING c.id, c.url
)
INSERT INTO cimd_client_candidate_sightings AS s (candidate_id, user_session_issuer_id, organization_id, last_seen_at)
SELECT candidate.id, input.user_session_issuer_id, input.organization_id, input.last_seen_at
FROM input
JOIN candidate ON candidate.url = input.url
ON CONFLICT (candidate_id, user_session_issuer_id) DO UPDATE SET
  last_seen_at = GREATEST(s.last_seen_at, EXCLUDED.last_seen_at),
  updated_at = clock_timestamp();
