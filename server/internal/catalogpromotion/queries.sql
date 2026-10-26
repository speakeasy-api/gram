-- name: GetSource :one
SELECT p.organization_id, m.project_id, m.id AS mcp_server_id, r.id AS remote_mcp_server_id, left(m.name,200)::text AS source_name,
  r.url, r.transport_type, m.visibility, left(COALESCE(r.name,''),200)::text AS remote_name, COALESCE(m.requires_upstream_auth,false)::boolean AS requires_upstream_auth, (m.requires_upstream_auth IS NOT NULL)::boolean AS upstream_auth_known, m.remote_session_issuer_id,
  left(COALESCE(pr.resource_name,''),200)::text AS resource_name, left(COALESCE(pr.resource_documentation,''),2049)::text AS resource_documentation,
  md5(m.updated_at::text || r.updated_at::text || COALESCE(h.revision, '') || COALESCE(a.revision, ''))::text AS source_revision,
  GREATEST(m.updated_at, r.updated_at, h.updated_at, a.updated_at)::timestamptz AS changed_at,
  (COALESCE(h.header_count, 0) + COALESCE(a.header_count, 0))::bigint AS header_count
FROM mcp_servers m
JOIN projects p ON p.id = m.project_id
JOIN remote_mcp_servers r ON r.id = m.remote_mcp_server_id AND r.project_id = m.project_id
LEFT JOIN LATERAL (
  SELECT resource_name,resource_documentation FROM remote_protected_resources
  WHERE organization_id=p.organization_id AND project_id=m.project_id AND resource_identifier=r.url AND deleted IS FALSE
  ORDER BY updated_at DESC,id DESC LIMIT 1
) pr ON true
LEFT JOIN LATERAL (
  SELECT count(*) FILTER (WHERE deleted IS FALSE) AS header_count, max(GREATEST(updated_at,deleted_at)) AS updated_at,
    string_agg(id::text || updated_at::text || deleted::text, ',' ORDER BY id) AS revision
  FROM remote_mcp_server_headers WHERE remote_mcp_server_id = r.id
) h ON true
LEFT JOIN LATERAL (
  SELECT COALESCE(sum(ah.header_count) FILTER (WHERE l.deleted IS FALSE AND ac.deleted IS FALSE AND ac.enabled),0)::bigint AS header_count,
    max(GREATEST(l.updated_at,l.deleted_at,ac.updated_at,ac.deleted_at,ah.updated_at)) AS updated_at,
    string_agg(l.id::text || l.updated_at::text || l.deleted::text || ac.id::text || ac.updated_at::text || ac.deleted::text || ac.enabled::text || COALESCE(ah.revision,''), ',' ORDER BY l.id) AS revision
  FROM mcp_server_auth_clients l
  JOIN mcp_auth_clients ac ON ac.id=l.auth_client_id AND ac.organization_id=l.organization_id
  LEFT JOIN LATERAL (
    SELECT count(*) FILTER (WHERE deleted IS FALSE) AS header_count,
      max(GREATEST(updated_at,deleted_at)) AS updated_at,
      string_agg(id::text || updated_at::text || deleted::text, ',' ORDER BY id) AS revision
    FROM mcp_auth_client_headers WHERE auth_client_id=ac.id AND organization_id=ac.organization_id
  ) ah ON true
  WHERE l.mcp_server_id=m.id AND l.project_id=m.project_id AND l.organization_id=p.organization_id
) a ON true
WHERE p.organization_id = @organization_id AND m.project_id = @project_id AND m.id = @mcp_server_id
  AND m.deleted IS FALSE AND r.deleted IS FALSE AND p.deleted IS FALSE
FOR UPDATE OF m, r, p;

-- name: DiscoveryTargets :many
WITH catalogued_endpoints AS MATERIALIZED (
 SELECT DISTINCT regexp_replace(lower(substring(remote->>'url' FROM '(?i)^https://[^/?#]+')), ':443$', '')
  || COALESCE(substring(remote->>'url' FROM '(?i)^https://[^/?#]+(.*)$'), '') AS url
 FROM mcp_registry_entries e,
 jsonb_array_elements(CASE WHEN jsonb_typeof(e.data #> '{server,remotes}')='array' THEN e.data #> '{server,remotes}' ELSE '[]'::jsonb END) AS remote
)
SELECT p.organization_id, m.project_id, m.id AS mcp_server_id
FROM mcp_servers m
JOIN projects p ON p.id = m.project_id
JOIN remote_mcp_servers r ON r.id = m.remote_mcp_server_id AND r.project_id = m.project_id
LEFT JOIN registry_promotion_candidates c ON c.project_id = m.project_id AND c.mcp_server_id = m.id
WHERE m.deleted IS FALSE AND r.deleted IS FALSE AND p.deleted IS FALSE AND c.promoted_at IS NULL
 AND (c.curation_status IS NULL OR c.curation_status='active')
 AND NOT EXISTS (SELECT 1 FROM catalogued_endpoints e WHERE e.url = regexp_replace(lower(substring(r.url FROM '(?i)^https://[^/?#]+')), ':443$', '') || COALESCE(substring(r.url FROM '(?i)^https://[^/?#]+(.*)$'), ''))
 AND p.organization_id <> @excluded_organization_id::text
AND (c.discovery_next_attempt_at IS NULL OR c.discovery_next_attempt_at <= @now::timestamptz)
ORDER BY c.discovery_last_attempt_at NULLS FIRST, m.id
LIMIT 50;

-- name: StoreEvidence :one
INSERT INTO registry_promotion_candidates (organization_id,project_id,mcp_server_id,remote_mcp_server_id,source_revision,eligible,reason,observed_successes,observed_failures,observed_days,checked_at)
VALUES(@organization_id,@project_id,@mcp_server_id,@remote_mcp_server_id,@source_revision,@eligible,@reason,@observed_successes,@observed_failures,@observed_days,@checked_at)
ON CONFLICT(project_id,mcp_server_id) DO UPDATE SET
 source_revision=EXCLUDED.source_revision, remote_mcp_server_id=EXCLUDED.remote_mcp_server_id, eligible=EXCLUDED.eligible, reason=EXCLUDED.reason,
 observed_successes=EXCLUDED.observed_successes, observed_failures=EXCLUDED.observed_failures,
 observed_days=EXCLUDED.observed_days, checked_at=EXCLUDED.checked_at, updated_at=clock_timestamp()
WHERE registry_promotion_candidates.promoted_at IS NULL
 AND registry_promotion_candidates.curation_status='active'
 AND (registry_promotion_candidates.curation_updated_at IS NULL OR registry_promotion_candidates.curation_updated_at <= EXCLUDED.checked_at)
 AND (registry_promotion_candidates.checked_at <= EXCLUDED.checked_at
 OR registry_promotion_candidates.source_revision <> EXCLUDED.source_revision)
RETURNING *;

-- name: GetCandidate :one
SELECT * FROM registry_promotion_candidates
WHERE organization_id= @organization_id AND project_id= @project_id AND mcp_server_id= @mcp_server_id
FOR UPDATE;

-- name: ListCandidates :many
WITH catalogued_endpoints AS MATERIALIZED (
 SELECT DISTINCT regexp_replace(lower(substring(remote->>'url' FROM '(?i)^https://[^/?#]+')), ':443$', '')
  || COALESCE(substring(remote->>'url' FROM '(?i)^https://[^/?#]+(.*)$'), '') AS url
 FROM mcp_registry_entries e,
 jsonb_array_elements(CASE WHEN jsonb_typeof(e.data #> '{server,remotes}')='array' THEN e.data #> '{server,remotes}' ELSE '[]'::jsonb END) AS remote
)
SELECT sqlc.embed(c),left(COALESCE(m.name,''),200)::text AS source_name FROM registry_promotion_candidates c
LEFT JOIN mcp_servers m ON m.id=c.mcp_server_id AND m.project_id=c.project_id AND m.deleted IS FALSE
LEFT JOIN remote_mcp_servers r ON r.id=m.remote_mcp_server_id AND r.project_id=m.project_id AND r.deleted IS FALSE
WHERE (c.promoted_at IS NOT NULL OR EXISTS (
 SELECT 1 FROM mcp_servers source
 JOIN projects source_project ON source_project.id=source.project_id
 JOIN remote_mcp_servers remote ON remote.id=source.remote_mcp_server_id AND remote.project_id=source.project_id
 WHERE source.id=c.mcp_server_id AND source.project_id=c.project_id AND source_project.organization_id=c.organization_id
 AND source.deleted IS FALSE AND source_project.deleted IS FALSE AND remote.deleted IS FALSE
))
AND (
 @status_filter::text='all'
 OR (@status_filter::text IN ('active','candidate') AND c.promoted_at IS NULL AND c.curation_status='active'
 AND (@status_filter::text='candidate' OR (c.reason <> 'already_catalogued'
  AND NOT EXISTS (SELECT 1 FROM catalogued_endpoints e WHERE e.url = regexp_replace(lower(substring(r.url FROM '(?i)^https://[^/?#]+')), ':443$', '') || COALESCE(substring(r.url FROM '(?i)^https://[^/?#]+(.*)$'), '')))))
 OR (@status_filter::text IN ('rejected','archived') AND c.promoted_at IS NULL AND c.curation_status= @status_filter::text)
 OR (@status_filter::text='promoted' AND c.promoted_at IS NOT NULL)
)
AND (NOT @has_cursor::boolean OR (c.created_at,c.id) < (@cursor_created_at::timestamptz,@cursor_id::uuid))
ORDER BY c.created_at DESC,c.id DESC LIMIT @page_limit;

-- name: PromoteCandidate :one
UPDATE registry_promotion_candidates SET registry_entry_id= @registry_entry_id,public_metadata= @public_metadata,
 endpoint_identity= @endpoint_identity,request_digest= @request_digest,retry_key= @retry_key,promoted_by= @promoted_by,
 promoted_at=clock_timestamp(),updated_at=clock_timestamp()
WHERE id= @id AND organization_id= @organization_id AND project_id IS NOT DISTINCT FROM sqlc.narg(project_id)::uuid AND promoted_at IS NULL AND curation_status='active'
RETURNING *;

-- name: PendingDeliveries :many
SELECT * FROM registry_promotion_candidates
WHERE promoted_at IS NOT NULL AND docs_queued_at IS NULL AND docs_needs_attention IS FALSE
 AND (docs_next_attempt_at IS NULL OR docs_next_attempt_at <= @now::timestamptz)
 AND (docs_lease_until IS NULL OR docs_lease_until <= @now::timestamptz)
ORDER BY docs_last_attempt_at NULLS FIRST,id LIMIT 50;

-- name: ClaimDocs :one
UPDATE registry_promotion_candidates SET docs_attempts=docs_attempts+1, docs_failures=LEAST(docs_failures+1,6),
 docs_last_attempt_at= @now::timestamptz,docs_next_attempt_at= @lease_until::timestamptz,
 docs_lease_until= @lease_until::timestamptz,docs_lease_token= @lease_token::uuid,
 docs_revision=generate_uuidv7(),updated_at=clock_timestamp()
WHERE id= @id AND project_id IS NOT DISTINCT FROM sqlc.narg(project_id)::uuid AND promoted_at IS NOT NULL AND docs_queued_at IS NULL AND docs_needs_attention IS FALSE
 AND (docs_next_attempt_at IS NULL OR docs_next_attempt_at <= @now::timestamptz)
 AND (docs_lease_until IS NULL OR docs_lease_until <= @now::timestamptz)
RETURNING *;

-- name: FinishDocs :execrows
UPDATE registry_promotion_candidates SET docs_url=sqlc.narg(docs_url)::text,
 docs_queued_at=sqlc.narg(docs_queued_at)::timestamptz,delivery_error=sqlc.narg(delivery_error)::text,
 docs_needs_attention= @needs_attention::boolean,docs_failures= @failures::integer,
 docs_next_attempt_at=sqlc.narg(next_attempt_at)::timestamptz,
 docs_lease_until=NULL,docs_lease_token=NULL,docs_revision=generate_uuidv7(),updated_at=clock_timestamp()
WHERE id= @id AND project_id IS NOT DISTINCT FROM sqlc.narg(project_id)::uuid AND docs_lease_token= @lease_token::uuid;

-- name: ClaimDiscovery :one
INSERT INTO registry_promotion_candidates(organization_id,project_id,mcp_server_id,remote_mcp_server_id,
 source_revision,eligible,reason,discovery_last_attempt_at,discovery_next_attempt_at)
SELECT p.organization_id,m.project_id,m.id,r.id,'unobserved',false,'telemetry_unavailable',@now::timestamptz,@next_attempt_at::timestamptz
FROM mcp_servers m JOIN projects p ON p.id=m.project_id
JOIN remote_mcp_servers r ON r.id=m.remote_mcp_server_id AND r.project_id=m.project_id
WHERE p.organization_id= @organization_id AND m.project_id= @project_id AND m.id= @mcp_server_id
 AND m.deleted IS FALSE AND r.deleted IS FALSE AND p.deleted IS FALSE
ON CONFLICT(project_id,mcp_server_id) DO UPDATE SET
 discovery_last_attempt_at=EXCLUDED.discovery_last_attempt_at,discovery_next_attempt_at=EXCLUDED.discovery_next_attempt_at,
 updated_at=clock_timestamp()
WHERE registry_promotion_candidates.promoted_at IS NULL
 AND registry_promotion_candidates.curation_status='active'
 AND (registry_promotion_candidates.discovery_next_attempt_at IS NULL OR registry_promotion_candidates.discovery_next_attempt_at <= @now::timestamptz)
RETURNING *;

-- name: FinishDiscovery :exec
UPDATE registry_promotion_candidates SET discovery_failures=CASE WHEN @failed::boolean THEN discovery_failures+1 ELSE 0 END,
 eligible=CASE WHEN @failure_reason::text <> '' AND checked_at <= @attempted_at::timestamptz THEN false ELSE eligible END,
 reason=CASE WHEN @failure_reason::text <> '' AND checked_at <= @attempted_at::timestamptz THEN @failure_reason::text ELSE reason END,
 updated_at=clock_timestamp()
WHERE id= @id AND project_id IS NOT DISTINCT FROM sqlc.narg(project_id)::uuid AND discovery_last_attempt_at= @attempted_at::timestamptz AND promoted_at IS NULL AND curation_status='active';

-- name: GetPromotion :one
SELECT * FROM registry_promotion_candidates WHERE organization_id= @organization_id AND id= @id FOR UPDATE;

-- name: RetryDocs :one
UPDATE registry_promotion_candidates SET docs_needs_attention=false,docs_failures=0,
 docs_next_attempt_at=NULL,docs_lease_token=NULL,docs_lease_until=NULL,delivery_error=NULL,
 docs_retry_key= @retry_key,docs_retry_revision= @retry_revision,docs_retry_by= @actor,
 docs_revision=generate_uuidv7(),updated_at=clock_timestamp()
WHERE id= @id AND project_id IS NOT DISTINCT FROM sqlc.narg(project_id)::uuid AND organization_id= @organization_id RETURNING *;

-- name: JobStatus :one
SELECT count(*) FILTER (WHERE promoted_at IS NOT NULL AND docs_queued_at IS NULL AND docs_needs_attention IS FALSE)::bigint AS pending_docs,
 count(*) FILTER (WHERE docs_needs_attention)::bigint AS needs_attention,
 min(promoted_at) FILTER (WHERE promoted_at IS NOT NULL AND docs_queued_at IS NULL AND docs_needs_attention IS FALSE)::timestamptz AS oldest_pending_at,
 max(discovery_last_attempt_at)::timestamptz AS last_discovery_at,
 count(*) FILTER (WHERE discovery_failures>0 AND promoted_at IS NULL AND curation_status='active')::bigint AS discovery_failures
FROM registry_promotion_candidates;

-- name: FindRegistryEndpoint :one
SELECT EXISTS(SELECT 1 FROM mcp_registry_entries e,
 jsonb_array_elements(CASE WHEN jsonb_typeof(e.data #> '{server,remotes}')='array' THEN e.data #> '{server,remotes}' ELSE '[]'::jsonb END) AS remote
 WHERE regexp_replace(lower(substring(remote->>'url' FROM '(?i)^https://[^/?#]+')), ':443$', '')
 || COALESCE(substring(remote->>'url' FROM '(?i)^https://[^/?#]+(.*)$'), '') = @url::text)::boolean;

-- name: LockEndpoint :exec
SELECT pg_advisory_xact_lock(hashtextextended(@url::text, 0));

-- name: InvalidateCandidate :exec
UPDATE registry_promotion_candidates SET eligible=false, reason='source_changed', updated_at=clock_timestamp()
WHERE id = @id AND project_id IS NOT DISTINCT FROM sqlc.narg(project_id)::uuid AND promoted_at IS NULL AND curation_status='active';

-- name: SeedPromotionSourceFixture :one
WITH remote AS (
 INSERT INTO remote_mcp_servers(project_id,name,slug,transport_type,url,created_at,updated_at)
 VALUES(@project_id,'Synthetic source','synthetic-source','streamable-http',@url,@created_at,@created_at) RETURNING id
)
INSERT INTO mcp_servers(project_id,name,slug,remote_mcp_server_id,visibility,created_at,updated_at)
SELECT @project_id,'Synthetic deployment','synthetic-deployment',id,'public',@created_at,@created_at FROM remote
RETURNING id;

-- name: ChangePromotionSourceFixture :exec
UPDATE mcp_servers SET updated_at=clock_timestamp(), visibility= @visibility
WHERE id = @id AND project_id = @project_id;

-- name: PromotionRowFixture :one
SELECT * FROM registry_promotion_candidates WHERE project_id = @project_id AND mcp_server_id = @mcp_server_id;

-- name: CountPublishedPromotionFixture :one
SELECT count(*) FROM registry_promotion_candidates c JOIN mcp_registry_entries e ON e.id = c.registry_entry_id
WHERE c.project_id = @project_id AND e.published;

-- name: DeletePromotionProjectFixture :exec
DELETE FROM projects WHERE id = @project_id;

-- name: DeletePromotionRegistryEntryFixture :exec
DELETE FROM mcp_registry_entries WHERE id = @id;

-- name: RepointPromotionSourceFixture :exec
WITH replacement AS (
 INSERT INTO remote_mcp_servers(id,project_id,name,slug,transport_type,url)
 SELECT @new_remote_id::uuid,m.project_id,'Synthetic replacement','synthetic-replacement',r.transport_type,r.url
 FROM mcp_servers m JOIN remote_mcp_servers r ON r.id=m.remote_mcp_server_id AND r.project_id=m.project_id
 WHERE m.project_id= @project_id AND m.id= @mcp_server_id
 RETURNING id
)
UPDATE mcp_servers m SET remote_mcp_server_id=replacement.id,updated_at=clock_timestamp()
FROM replacement WHERE m.project_id= @project_id AND m.id= @mcp_server_id;

-- name: CurateCandidate :one
UPDATE registry_promotion_candidates SET curation_status= @curation_status,
 curation_revision=generate_uuidv7(),curation_updated_at=clock_timestamp(),curation_updated_by= @actor,
 curation_retry_key= @retry_key,curation_retry_revision= @retry_revision,curation_retry_action= @action,
 eligible=CASE WHEN @action::text='restore' THEN false ELSE eligible END,
 reason=CASE WHEN @action::text='restore' THEN 'insufficient_evidence' ELSE reason END,
 discovery_next_attempt_at=CASE WHEN @action::text='restore' THEN NULL ELSE discovery_next_attempt_at END,
 updated_at=clock_timestamp()
WHERE id= @id AND organization_id= @organization_id AND project_id IS NOT DISTINCT FROM sqlc.narg(project_id)::uuid
 AND promoted_at IS NULL AND curation_revision= @curation_revision
RETURNING *;

-- name: SetCandidateCreatedAtFixture :exec
UPDATE registry_promotion_candidates SET created_at= @created_at
WHERE organization_id= @organization_id AND project_id= @project_id AND id= @id;

-- name: GetCurationTarget :one
SELECT * FROM registry_promotion_candidates WHERE organization_id= @organization_id AND id= @id;

-- name: SoftDeletePromotionSourceFixture :exec
UPDATE mcp_servers SET deleted_at=clock_timestamp()
WHERE id= @id AND project_id= @project_id;

-- name: LockSource :one
SELECT m.id FROM mcp_servers m
JOIN projects p ON p.id=m.project_id
JOIN remote_mcp_servers r ON r.id=m.remote_mcp_server_id AND r.project_id=m.project_id
WHERE p.organization_id= @organization_id AND m.project_id= @project_id AND m.id= @mcp_server_id
 AND m.deleted IS FALSE AND r.deleted IS FALSE AND p.deleted IS FALSE
FOR UPDATE OF m,r,p;

-- name: LockSourceHeaders :exec
-- The locked remote server fences insertions; deletion does not lock its parent.
SELECT h.id FROM remote_mcp_server_headers h
JOIN remote_mcp_servers r ON r.id=h.remote_mcp_server_id
JOIN mcp_servers m ON m.remote_mcp_server_id=r.id AND m.project_id=r.project_id
JOIN projects p ON p.id=m.project_id
WHERE p.organization_id= @organization_id AND m.project_id= @project_id AND m.id= @mcp_server_id
ORDER BY h.id FOR SHARE OF h NOWAIT;

-- name: LockSourceAuthClients :exec
-- The source server is already locked against new attachments. NOWAIT avoids
-- reversing auth-client writers' client-before-server lock order.
SELECT ac.id FROM mcp_server_auth_clients l
JOIN mcp_auth_clients ac ON ac.id=l.auth_client_id AND ac.organization_id=l.organization_id
WHERE l.project_id= @project_id AND l.mcp_server_id= @mcp_server_id AND l.organization_id= @organization_id
ORDER BY ac.id,l.id FOR UPDATE OF ac,l NOWAIT;

-- name: LockSourceAuthClientHeaders :exec
-- Parent client locks prevent new headers; these locks fence existing edits.
SELECT h.id FROM mcp_auth_client_headers h
JOIN mcp_server_auth_clients l ON l.auth_client_id=h.auth_client_id AND l.organization_id=h.organization_id
WHERE l.project_id= @project_id AND l.mcp_server_id= @mcp_server_id AND l.organization_id= @organization_id
ORDER BY h.id FOR SHARE OF h NOWAIT;

-- name: ChangePromotionAuthHeaderFixture :exec
UPDATE mcp_auth_client_headers h SET is_required= @is_required,deleted_at=sqlc.narg(deleted_at)::timestamptz,updated_at=clock_timestamp()
WHERE h.auth_client_id= @auth_client_id AND h.organization_id= @organization_id
 AND EXISTS(SELECT 1 FROM mcp_server_auth_clients l WHERE l.auth_client_id=h.auth_client_id AND l.project_id= @project_id);

-- name: ChangePromotionSourceURLFixture :exec
UPDATE remote_mcp_servers r SET url= @url, updated_at=clock_timestamp()
FROM mcp_servers m
WHERE m.id= @mcp_server_id AND m.project_id= @project_id AND r.id=m.remote_mcp_server_id AND r.project_id=m.project_id;
