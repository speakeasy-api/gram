-- name: LockSkillName :exec
SELECT pg_advisory_xact_lock(hashtextextended('skill:' || (@project_id::uuid)::text || ':' || @name::text, 0));

-- name: LockSkillObservationReconciliation :exec
SELECT pg_advisory_xact_lock(hashtextextended('skill-observations:' || (@project_id::uuid)::text, 0));

-- name: GetSkillByNameForUpdate :one
SELECT *
FROM skills
WHERE project_id = @project_id
  AND name = @name
  AND archived_at IS NULL
FOR UPDATE;

-- name: GetSkillForUpdate :one
SELECT *
FROM skills
WHERE project_id = @project_id
  AND id = @id
  AND archived_at IS NULL
FOR UPDATE;

-- name: ListProjectsWithPendingSkillObservations :many
WITH RECURSIVE pending_projects AS (
  (
    SELECT candidate.project_id, 1 AS sequence
    FROM (
      (
        SELECT so.project_id
        FROM skill_observations so
        WHERE so.reconciled_at IS NULL
          AND so.project_id > @project_cursor
        ORDER BY so.project_id
        LIMIT 1
      )
      UNION ALL
      (
        SELECT so.project_id
        FROM skill_observations so
        JOIN projects p
          ON p.id = so.project_id
          AND p.deleted IS FALSE
        WHERE so.reconciled_at IS NOT NULL
          AND so.metrics_synced_at IS NULL
          AND so.session_id IS NOT NULL
          AND so.skill_version_id IS NOT NULL
          AND so.project_id > @project_cursor
        ORDER BY so.project_id
        LIMIT 1
      )
    ) candidate
    ORDER BY candidate.project_id
    LIMIT 1
  )
  UNION ALL
  SELECT next_project.project_id, current_project.sequence + 1
  FROM pending_projects current_project
  CROSS JOIN LATERAL (
    SELECT candidate.project_id
    FROM (
      (
        SELECT so.project_id
        FROM skill_observations so
        WHERE so.reconciled_at IS NULL
          AND so.project_id > current_project.project_id
        ORDER BY so.project_id
        LIMIT 1
      )
      UNION ALL
      (
        SELECT so.project_id
        FROM skill_observations so
        JOIN projects p
          ON p.id = so.project_id
          AND p.deleted IS FALSE
        WHERE so.reconciled_at IS NOT NULL
          AND so.metrics_synced_at IS NULL
          AND so.session_id IS NOT NULL
          AND so.skill_version_id IS NOT NULL
          AND so.project_id > current_project.project_id
        ORDER BY so.project_id
        LIMIT 1
      )
    ) candidate
    ORDER BY candidate.project_id
    LIMIT 1
  ) next_project
  WHERE current_project.sequence < @page_limit
)
SELECT project_id
FROM pending_projects
ORDER BY sequence
LIMIT @page_limit;

-- name: ListPendingSkillSessionVersions :many
SELECT
  so.id,
  so.created_at,
  so.seen_at,
  p.organization_id,
  so.project_id,
  so.session_id::text AS session_id,
  so.skill_id::uuid AS skill_id,
  so.skill_version_id::uuid AS skill_version_id,
  sv.canonical_sha256,
  -- Surface is part of the attribution join contract: assistant/assistants
  -- producers map to assistant, and every supported dev producer maps to dev.
  CASE WHEN so.provider IN ('assistant', 'assistants') THEN 'assistant' ELSE 'dev' END::text AS surface
FROM skill_observations so
JOIN projects p ON p.id = so.project_id
JOIN skills s
  ON s.project_id = so.project_id
  AND s.id = so.skill_id
JOIN skill_versions sv
  ON sv.skill_id = s.id
  AND sv.id = so.skill_version_id
WHERE so.project_id = @project_id
  AND so.reconciled_at IS NOT NULL
  AND so.metrics_synced_at IS NULL
  AND so.session_id IS NOT NULL
  AND so.skill_id IS NOT NULL
  AND so.skill_version_id IS NOT NULL
ORDER BY so.seen_at, so.id
LIMIT @batch_size;

-- name: MarkSkillSessionVersionsSynced :execrows
UPDATE skill_observations
SET metrics_synced_at = clock_timestamp()
WHERE project_id = @project_id
  AND id = ANY(@observation_ids::uuid[])
  AND reconciled_at IS NOT NULL
  AND metrics_synced_at IS NULL;

-- name: ClaimPendingSkillObservations :many
SELECT *
FROM skill_observations
WHERE project_id = @project_id
  AND reconciled_at IS NULL
ORDER BY seen_at, id
LIMIT @batch_size
FOR UPDATE SKIP LOCKED;

-- name: ResolveSkillObservationVersions :many
SELECT srh.raw_sha256, candidate.skill_id, candidate.skill_version_id
FROM skill_raw_hashes srh
JOIN LATERAL (
  SELECT sv.skill_id, sv.id AS skill_version_id
  FROM skills s
  JOIN skill_versions sv
    ON sv.skill_id = s.id
    AND sv.canonical_sha256 = srh.canonical_sha256
  WHERE s.project_id = srh.project_id
    AND s.archived_at IS NULL
  ORDER BY sv.skill_id, sv.id
  LIMIT 2
) candidate ON TRUE
WHERE srh.project_id = @project_id
  AND srh.raw_sha256 = ANY(@raw_sha256s::text[])
ORDER BY srh.raw_sha256, candidate.skill_id, candidate.skill_version_id;

-- name: CreateSkill :one
INSERT INTO skills (
  project_id,
  name,
  display_name,
  summary,
  source_kind,
  classification
) VALUES (
  @project_id,
  @name,
  @display_name,
  sqlc.narg(summary)::text,
  'manual',
  'custom'
)
ON CONFLICT (project_id, name) WHERE archived_at IS NULL
DO NOTHING
RETURNING *;

-- name: CreateCapturedSkill :one
INSERT INTO skills (
  project_id,
  name,
  display_name,
  summary,
  source_kind,
  classification
) VALUES (
  @project_id,
  @name,
  @display_name,
  sqlc.narg(summary)::text,
  'captured',
  'custom'
)
ON CONFLICT (project_id, name) WHERE archived_at IS NULL
DO NOTHING
RETURNING *;

-- name: CreateObservedSkill :one
INSERT INTO skills (
  project_id,
  name,
  display_name,
  summary,
  source_kind,
  classification
) VALUES (
  @project_id,
  @name,
  @display_name,
  NULL,
  'captured',
  'custom'
)
ON CONFLICT (project_id, name) WHERE archived_at IS NULL
DO NOTHING
RETURNING *;

-- name: CompleteSkillObservations :one
WITH completed AS (
  UPDATE skill_observations so
  SET skill_id = @skill_id,
      skill_version_id = sqlc.narg(skill_version_id)::uuid,
      reconciled_at = clock_timestamp(),
      reconcile_error_code = NULL
  WHERE so.project_id = @project_id
    AND so.id = ANY(@observation_ids::uuid[])
    AND so.reconciled_at IS NULL
  RETURNING so.seen_at, so.source, so.source_level, so.raw_sha256
), completed_hashes AS (
  SELECT DISTINCT raw_sha256
  FROM completed
  WHERE raw_sha256 IS NOT NULL
), own_distributed_hashes AS (
  SELECT completed_hashes.raw_sha256
  FROM completed_hashes
  WHERE EXISTS (
    SELECT 1
    FROM skill_distributions sd
    JOIN skill_versions sv
      ON sv.skill_id = sd.skill_id
      AND sv.spec_valid IS TRUE
    WHERE sd.project_id = @project_id
      AND sd.channel = 'plugin'
      AND (
        sv.raw_sha256 = completed_hashes.raw_sha256
        OR EXISTS (
          SELECT 1
          FROM skill_raw_hashes srh
          WHERE srh.project_id = sd.project_id
            AND srh.raw_sha256 = completed_hashes.raw_sha256
            AND srh.canonical_sha256 = sv.canonical_sha256
        )
      )
  )
), own_distributed_skill AS (
  SELECT EXISTS (
    SELECT 1
    FROM skill_distributions sd
    WHERE sd.project_id = @project_id
      AND sd.skill_id = @skill_id
      AND sd.channel = 'plugin'
  ) AS distributed
), evidence_rows AS (
  SELECT
    completed.seen_at,
    (
      lower(btrim(COALESCE(completed.source_level, ''))) IN ('plugin', 'bundled', 'admin', 'system')
      OR lower(btrim(COALESCE(completed.source, ''))) IN (
        'anthropic', 'claude', 'claude-code', 'openai', 'codex', 'cursor',
        'built-in', 'builtin', 'bundled', 'system', 'vendor'
      )
    )
    AND own_distributed_hashes.raw_sha256 IS NULL
    AND NOT (SELECT distributed FROM own_distributed_skill) AS built_in
  FROM completed
  LEFT JOIN own_distributed_hashes USING (raw_sha256)
), evidence AS (
  SELECT
    MIN(seen_at) AS first_seen_at,
    MAX(seen_at) AS last_seen_at,
    COUNT(*)::bigint AS seen_count,
    COALESCE(bool_and(built_in), FALSE) AS all_built_in
  FROM evidence_rows
)
UPDATE skills s
SET first_seen_at = CASE
      WHEN s.first_seen_at IS NULL THEN evidence.first_seen_at
      ELSE LEAST(s.first_seen_at, evidence.first_seen_at)
    END,
    last_seen_at = CASE
      WHEN s.last_seen_at IS NULL THEN evidence.last_seen_at
      ELSE GREATEST(s.last_seen_at, evidence.last_seen_at)
    END,
    seen_count = COALESCE(s.seen_count, 0) + evidence.seen_count,
    classification = CASE
      WHEN s.source_kind <> 'captured' THEN s.classification
      WHEN COALESCE(s.seen_count, 0) = 0 AND evidence.all_built_in THEN 'built_in'
      WHEN NOT evidence.all_built_in THEN 'custom'
      ELSE s.classification
    END
FROM evidence
WHERE s.project_id = @project_id
  AND s.id = @skill_id
  AND evidence.seen_count > 0
RETURNING evidence.seen_count;

-- name: FailSkillObservationReconciliations :execrows
UPDATE skill_observations
SET reconciled_at = clock_timestamp(),
    reconcile_error_code = @error_code,
    skill_id = NULL,
    skill_version_id = NULL
WHERE project_id = @project_id
  AND id = ANY(@observation_ids::uuid[])
  AND reconciled_at IS NULL;

-- name: BackfillSkillObservationsForCapturedVersion :execrows
UPDATE skill_observations so
SET skill_id = sqlc.arg(skill_id)::uuid,
    skill_version_id = sqlc.arg(skill_version_id)::uuid,
    reconciled_at = CASE WHEN so.reconcile_error_code IS NULL THEN so.reconciled_at ELSE NULL END,
    reconcile_error_code = NULL
FROM skill_versions sv
JOIN skills s ON s.id = sv.skill_id
WHERE so.project_id = @project_id
  AND so.raw_sha256 = sqlc.arg(raw_sha256)::text
  AND so.skill_version_id IS NULL
  AND (so.skill_id IS NULL OR so.skill_id = sqlc.arg(skill_id)::uuid)
  AND s.project_id = so.project_id
  AND s.id = sqlc.arg(skill_id)::uuid
  AND sv.skill_id = s.id
  AND sv.id = sqlc.arg(skill_version_id)::uuid
  AND sv.canonical_sha256 = @canonical_sha256
  AND NOT EXISTS (
    SELECT 1
    FROM skill_versions conflicting_version
    JOIN skills conflicting_skill ON conflicting_skill.id = conflicting_version.skill_id
    WHERE conflicting_skill.project_id = so.project_id
      AND conflicting_skill.archived_at IS NULL
      AND conflicting_version.canonical_sha256 = @canonical_sha256
      AND conflicting_version.id <> sqlc.arg(skill_version_id)::uuid
  );

-- name: CreateSkillVersion :one
INSERT INTO skill_versions (
  skill_id,
  content,
  canonical_sha256,
  raw_sha256,
  description,
  metadata,
  spec_valid,
  validation_errors,
  created_by_user_id
)
SELECT
  s.id,
  @content,
  @canonical_sha256,
  @raw_sha256,
  sqlc.narg(description)::text,
  @metadata::jsonb,
  @spec_valid,
  @validation_errors::jsonb,
  @created_by_user_id
FROM skills s
WHERE s.project_id = @project_id
  AND s.id = @skill_id
  AND s.archived_at IS NULL
ON CONFLICT (skill_id, canonical_sha256)
DO NOTHING
RETURNING *;

-- name: CreateSkillVersionLineage :exec
INSERT INTO skill_version_lineages (
  skill_version_id,
  skill_id,
  derived_from_version_id
)
SELECT sv.id, sv.skill_id, @derived_from_version_id
FROM skill_versions sv
JOIN skills s ON s.id = sv.skill_id
WHERE s.project_id = @project_id
  AND s.id = @skill_id
  AND sv.id = @skill_version_id;

-- name: GetProjectSkillVersion :one
SELECT sv.*
FROM skill_versions sv
JOIN skills s ON s.id = sv.skill_id
WHERE s.project_id = @project_id
  AND sv.id = @skill_version_id;

-- name: GetSkillVersionByHash :one
SELECT sv.*
FROM skill_versions sv
JOIN skills s ON s.id = sv.skill_id
WHERE s.project_id = @project_id
  AND s.id = @skill_id
  AND s.archived_at IS NULL
  AND sv.canonical_sha256 = @canonical_sha256;

-- name: InsertCapturedSkillVersionOrigin :exec
INSERT INTO skill_version_origins (skill_version_id, skill_id, project_id, origin)
SELECT sv.id, sv.skill_id, s.project_id, 'captured'
FROM skill_versions sv
JOIN skills s ON s.id = sv.skill_id
WHERE s.project_id = @project_id
  AND s.id = @skill_id
  AND sv.id = @skill_version_id
ON CONFLICT (skill_version_id) DO NOTHING;

-- name: DeleteSkillVersionOrigin :exec
DELETE FROM skill_version_origins
WHERE project_id = @project_id
  AND skill_id = @skill_id
  AND skill_version_id = @skill_version_id;

-- name: StoreSkillRawHashAlias :one
WITH inserted AS (
  INSERT INTO skill_raw_hashes (project_id, raw_sha256, canonical_sha256)
  SELECT s.project_id, @raw_sha256, sv.canonical_sha256
  FROM skill_versions sv
  JOIN skills s ON s.id = sv.skill_id
  WHERE s.project_id = @project_id
    AND s.id = @skill_id
    AND sv.id = @skill_version_id
    AND sv.canonical_sha256 = @canonical_sha256
  ON CONFLICT (project_id, raw_sha256) DO NOTHING
  RETURNING 1
)
SELECT TRUE AS matches
FROM inserted
UNION ALL
SELECT srh.canonical_sha256 = @canonical_sha256 AS matches
FROM skill_raw_hashes srh
WHERE srh.project_id = @project_id
  AND srh.raw_sha256 = @raw_sha256
LIMIT 1;

-- name: GetSkillVersionOrigin :one
SELECT *
FROM skill_version_origins
WHERE project_id = @project_id
  AND skill_id = @skill_id
  AND skill_version_id = @skill_version_id;

-- name: GetSkillRawHash :one
SELECT *
FROM skill_raw_hashes
WHERE project_id = @project_id
  AND raw_sha256 = @raw_sha256;

-- name: SyncSkillSummary :one
UPDATE skills
SET summary = sqlc.narg(summary)::text,
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND id = @id
  AND archived_at IS NULL
RETURNING *;

-- name: UpdateSkillDetails :one
UPDATE skills
SET name = @name,
    display_name = @display_name,
    summary = sqlc.narg(summary)::text,
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND id = @id
  AND archived_at IS NULL
RETURNING *;

-- name: PromoteObservedSkillToManual :one
UPDATE skills
SET source_kind = 'manual',
    classification = 'custom',
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND id = @id
  AND source_kind = 'captured'
  AND archived_at IS NULL
RETURNING *;

-- name: GetSkill :one
SELECT *
FROM skills
WHERE project_id = @project_id
  AND id = @id
  AND archived_at IS NULL;

-- name: GetSkillDetails :one
SELECT
  sqlc.embed(s),
  l.token AS share_token,
  COALESCE(state.latest_version_id, '00000000-0000-0000-0000-000000000000'::uuid) AS latest_version_id,
  COALESCE(state.version_count, 0)::bigint AS version_count,
  EXISTS (
    SELECT 1 FROM skill_versions sv
    WHERE sv.skill_id = s.id AND sv.spec_valid IS TRUE
  )::boolean AS has_valid_version,
  (
    SELECT COUNT(*)::bigint
    FROM skill_distributions sd
    JOIN assistants a
      ON a.id = sd.assistant_id
      AND a.project_id = sd.project_id
      AND a.deleted IS FALSE
    WHERE sd.project_id = s.project_id
      AND sd.skill_id = s.id
      AND sd.channel = 'assistant'
      AND sd.plugin_id IS NULL
      AND sd.assistant_id IS NOT NULL
      AND sd.revoked_at IS NULL
  ) AS assistant_count
FROM skills s
LEFT JOIN LATERAL (
  SELECT
    sv.id AS latest_version_id,
    COUNT(*) OVER()::bigint AS version_count
  FROM skill_versions sv
  WHERE sv.skill_id = s.id
  ORDER BY sv.created_at DESC, sv.id DESC
  LIMIT 1
) state ON TRUE
LEFT JOIN skill_share_links l
  ON l.skill_id = s.id
  AND l.revoked_at IS NULL
WHERE s.project_id = @project_id
  AND s.id = @skill_id
  AND s.archived_at IS NULL;

-- name: GetSkillState :one
SELECT
  COALESCE(state.latest_version_id, '00000000-0000-0000-0000-000000000000'::uuid) AS latest_version_id,
  COALESCE(state.version_count, 0)::bigint AS version_count,
  EXISTS (
    SELECT 1 FROM skill_versions sv
    WHERE sv.skill_id = s.id AND sv.spec_valid IS TRUE
  )::boolean AS has_valid_version
FROM skills s
LEFT JOIN LATERAL (
  SELECT
    sv.id AS latest_version_id,
    COUNT(*) OVER()::bigint AS version_count
  FROM skill_versions sv
  WHERE sv.skill_id = s.id
  ORDER BY sv.created_at DESC, sv.id DESC
  LIMIT 1
) state ON TRUE
WHERE s.project_id = @project_id
  AND s.id = @skill_id
  AND s.archived_at IS NULL;

-- name: ListSkills :many
SELECT
  sqlc.embed(s),
  l.token AS share_token,
  COALESCE(latest.id, '00000000-0000-0000-0000-000000000000'::uuid) AS latest_version_id,
  COALESCE(latest.version_count, 0)::bigint AS version_count,
  EXISTS (
    SELECT 1 FROM skill_versions sv
    WHERE sv.skill_id = s.id AND sv.spec_valid IS TRUE
  )::boolean AS has_valid_version
FROM skills s
LEFT JOIN LATERAL (
  SELECT
    sv.id,
    COUNT(*) OVER()::bigint AS version_count
  FROM skill_versions sv
  WHERE sv.skill_id = s.id
  ORDER BY sv.created_at DESC, sv.id DESC
  LIMIT 1
) latest ON TRUE
LEFT JOIN skill_share_links l
  ON l.skill_id = s.id
  AND l.revoked_at IS NULL
WHERE s.project_id = @project_id
  AND s.archived_at IS NULL
  AND (
    sqlc.narg(cursor_name)::text IS NULL
    OR s.name > sqlc.narg(cursor_name)::text
  )
ORDER BY s.name ASC
LIMIT @page_limit;

-- name: ListSkillVersions :many
SELECT
  sqlc.embed(sv),
  svl.derived_from_version_id,
  sightings.first_seen_at,
  sightings.last_seen_at,
  COALESCE(sightings.seen_count, 0)::bigint AS seen_count
FROM skill_versions sv
JOIN skills s ON s.id = sv.skill_id
LEFT JOIN skill_version_lineages svl
  ON svl.skill_id = sv.skill_id
  AND svl.skill_version_id = sv.id
LEFT JOIN LATERAL (
  SELECT
    MIN(so.seen_at)::timestamptz AS first_seen_at,
    MAX(so.seen_at)::timestamptz AS last_seen_at,
    COUNT(*)::bigint AS seen_count
  FROM skill_observations so
  WHERE so.project_id = s.project_id
    AND so.skill_id = sv.skill_id
    AND so.skill_version_id = sv.id
    AND so.reconciled_at IS NOT NULL
    AND so.reconcile_error_code IS NULL
) sightings ON TRUE
WHERE s.project_id = @project_id
  AND s.id = @skill_id
  AND s.archived_at IS NULL
  AND (
    sqlc.narg(cursor_created_at)::timestamptz IS NULL
    OR (sv.created_at, sv.id) < (
      sqlc.narg(cursor_created_at)::timestamptz,
      sqlc.narg(cursor_id)::uuid
    )
  )
ORDER BY sv.created_at DESC, sv.id DESC
LIMIT @page_limit;

-- name: GetSkillVersionDetails :one
SELECT
  sqlc.embed(sv),
  svl.derived_from_version_id,
  sightings.first_seen_at,
  sightings.last_seen_at,
  COALESCE(sightings.seen_count, 0)::bigint AS seen_count
FROM skill_versions sv
JOIN skills s ON s.id = sv.skill_id
LEFT JOIN skill_version_lineages svl
  ON svl.skill_id = sv.skill_id
  AND svl.skill_version_id = sv.id
LEFT JOIN LATERAL (
  SELECT
    MIN(so.seen_at)::timestamptz AS first_seen_at,
    MAX(so.seen_at)::timestamptz AS last_seen_at,
    COUNT(*)::bigint AS seen_count
  FROM skill_observations so
  WHERE so.project_id = s.project_id
    AND so.skill_id = sv.skill_id
    AND so.skill_version_id = sv.id
    AND so.reconciled_at IS NOT NULL
    AND so.reconcile_error_code IS NULL
) sightings ON TRUE
WHERE s.project_id = @project_id
  AND s.id = @skill_id
  AND s.archived_at IS NULL
  AND sv.id = @skill_version_id;

-- name: GetSkillAdoptionStats :one
SELECT
  COUNT(DISTINCT NULLIF(lower(btrim(so.hostname)), ''))::bigint AS distinct_hostnames,
  COUNT(*)::bigint AS activations_in_window
FROM skill_observations so
WHERE so.project_id = @project_id
  AND so.skill_id = sqlc.arg(skill_id)::uuid
  AND so.reconciled_at IS NOT NULL
  AND so.reconcile_error_code IS NULL
  AND so.seen_at >= @window_start
  AND so.seen_at < @window_end;

-- name: ListSkillSightingTimeline :many
SELECT
  (date_trunc('day', so.seen_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC')::timestamptz AS bucket_start,
  COUNT(*)::bigint AS activation_count
FROM skill_observations so
WHERE so.project_id = @project_id
  AND so.skill_id = sqlc.arg(skill_id)::uuid
  AND so.reconciled_at IS NOT NULL
  AND so.reconcile_error_code IS NULL
  AND so.seen_at >= @window_start
  AND so.seen_at < @window_end
GROUP BY bucket_start
ORDER BY bucket_start ASC;

-- name: ListActiveMachineLatestVersions :many
WITH latest AS (
  SELECT DISTINCT ON (lower(btrim(so.hostname)))
    lower(btrim(so.hostname)) AS hostname,
    so.skill_version_id
  FROM skill_observations so
  WHERE so.project_id = @project_id
    AND so.skill_id = sqlc.arg(skill_id)::uuid
    AND NULLIF(btrim(so.hostname), '') IS NOT NULL
    AND so.reconciled_at IS NOT NULL
    AND so.reconcile_error_code IS NULL
    AND so.seen_at >= @window_start
    AND so.seen_at < @window_end
  ORDER BY lower(btrim(so.hostname)), so.seen_at DESC, so.id DESC
)
SELECT skill_version_id, COUNT(*)::bigint AS machine_count
FROM latest
GROUP BY skill_version_id;

-- name: ListSkillDistributionTargetVersions :many
SELECT DISTINCT resolved.id
FROM skill_distributions sd
JOIN LATERAL (
  SELECT sv.id
  FROM skill_versions sv
  LEFT JOIN skill_version_origins svo
    ON svo.project_id = sd.project_id
    AND svo.skill_id = sv.skill_id
    AND svo.skill_version_id = sv.id
  WHERE sv.skill_id = sd.skill_id
    AND sv.spec_valid IS TRUE
    AND (sd.pinned_version_id IS NULL OR sv.id = sd.pinned_version_id)
  ORDER BY (svo.origin IS DISTINCT FROM 'captured') DESC, sv.created_at DESC, sv.id DESC
  LIMIT 1
) resolved ON TRUE
WHERE sd.project_id = @project_id
  AND sd.skill_id = @skill_id
  AND sd.channel = 'plugin'
  AND sd.revoked_at IS NULL
ORDER BY resolved.id;

-- name: ListUnknownSkillActivations :many
SELECT so.*
FROM skill_observations so
WHERE so.project_id = @project_id
  AND so.skill_id IS NULL
  AND so.reconciled_at IS NOT NULL
  AND so.reconcile_error_code IS NOT NULL
  AND (
    sqlc.narg(cursor_seen_at)::timestamptz IS NULL
    OR (so.seen_at, so.id) < (
      sqlc.narg(cursor_seen_at)::timestamptz,
      sqlc.narg(cursor_id)::uuid
    )
  )
ORDER BY so.seen_at DESC, so.id DESC
LIMIT @page_limit;

-- name: GetSkillName :one
SELECT name
FROM skills
WHERE project_id = @project_id
  AND id = @id
  AND archived_at IS NULL;

-- name: ArchiveSkill :one
UPDATE skills
SET archived_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND id = @id
  AND archived_at IS NULL
RETURNING *;

-- name: GetValidSkillVersion :one
SELECT sv.id
FROM skill_versions sv
JOIN skills s ON s.id = sv.skill_id
WHERE s.project_id = @project_id
  AND s.id = @skill_id
  AND s.archived_at IS NULL
  AND sv.id = @version_id
  AND sv.spec_valid IS TRUE;

-- name: GetLatestValidSkillVersion :one
SELECT sv.id
FROM skill_versions sv
JOIN skills s ON s.id = sv.skill_id
LEFT JOIN skill_version_origins svo
  ON svo.project_id = s.project_id
  AND svo.skill_id = sv.skill_id
  AND svo.skill_version_id = sv.id
WHERE s.project_id = @project_id
  AND s.id = @skill_id
  AND s.archived_at IS NULL
  AND sv.spec_valid IS TRUE
ORDER BY (svo.origin IS DISTINCT FROM 'captured') DESC, sv.created_at DESC, sv.id DESC
LIMIT 1;

-- name: GetPluginForDistribution :one
-- The share lock makes distribution creation serialize against plugin
-- deletion, which soft-deletes the plugin row before revoking distributions.
SELECT id, name
FROM plugins
WHERE id = @plugin_id
  AND project_id = @project_id
  AND deleted IS FALSE
FOR SHARE;

-- name: GetAssistantForDistribution :one
-- The share lock serializes distribution creation against assistant deletion.
SELECT id, name
FROM assistants
WHERE id = @assistant_id
  AND project_id = @project_id
  AND deleted IS FALSE
FOR SHARE;

-- name: GetActiveSkillDistributionRecord :one
SELECT
  sqlc.embed(sd),
  resolved.id AS resolved_version_id
FROM skill_distributions sd
JOIN LATERAL (
  SELECT sv.id
  FROM skill_versions sv
  LEFT JOIN skill_version_origins svo
    ON svo.project_id = sd.project_id
    AND svo.skill_id = sv.skill_id
    AND svo.skill_version_id = sv.id
  WHERE sv.skill_id = sd.skill_id
    AND sv.spec_valid IS TRUE
    AND (sd.pinned_version_id IS NULL OR sv.id = sd.pinned_version_id)
  ORDER BY (svo.origin IS DISTINCT FROM 'captured') DESC, sv.created_at DESC, sv.id DESC
  LIMIT 1
) resolved ON TRUE
WHERE sd.project_id = @project_id
  AND sd.skill_id = @skill_id
  AND sd.plugin_id IS NOT DISTINCT FROM sqlc.narg(plugin_id)::uuid
  AND sd.assistant_id IS NOT DISTINCT FROM sqlc.narg(assistant_id)::uuid
  AND sd.channel = @channel
  AND (
    (@channel = 'plugin' AND sqlc.narg(plugin_id)::uuid IS NOT NULL AND sqlc.narg(assistant_id)::uuid IS NULL)
    OR (@channel = 'assistant' AND sqlc.narg(assistant_id)::uuid IS NOT NULL AND sqlc.narg(plugin_id)::uuid IS NULL)
  )
  AND sd.revoked_at IS NULL
FOR UPDATE OF sd;

-- name: ListActiveSkillDistributions :many
SELECT
  sqlc.embed(sd),
  s.name AS skill_name,
  s.display_name AS skill_display_name,
  pl.name AS plugin_name,
  resolved.id AS resolved_version_id
FROM skill_distributions sd
JOIN plugins pl
  ON pl.id = sd.plugin_id
  AND pl.project_id = sd.project_id
  AND pl.deleted IS FALSE
JOIN skills s
  ON s.project_id = sd.project_id
  AND s.id = sd.skill_id
  AND s.archived_at IS NULL
JOIN LATERAL (
  SELECT sv.id
  FROM skill_versions sv
  LEFT JOIN skill_version_origins svo
    ON svo.project_id = sd.project_id
    AND svo.skill_id = sv.skill_id
    AND svo.skill_version_id = sv.id
  WHERE sv.skill_id = sd.skill_id
    AND sv.spec_valid IS TRUE
    AND (sd.pinned_version_id IS NULL OR sv.id = sd.pinned_version_id)
  ORDER BY (svo.origin IS DISTINCT FROM 'captured') DESC, sv.created_at DESC, sv.id DESC
  LIMIT 1
) resolved ON TRUE
WHERE sd.project_id = @project_id
  AND sd.channel = 'plugin'
  AND sd.plugin_id IS NOT NULL
  AND sd.assistant_id IS NULL
  AND sd.revoked_at IS NULL
  AND (sqlc.narg(skill_id)::uuid IS NULL OR sd.skill_id = sqlc.narg(skill_id)::uuid)
  AND (sqlc.narg(plugin_id)::uuid IS NULL OR sd.plugin_id = sqlc.narg(plugin_id)::uuid)
  AND (
    sqlc.narg(cursor_created_at)::timestamptz IS NULL
    OR (sd.created_at, sd.id) > (
      sqlc.narg(cursor_created_at)::timestamptz,
      sqlc.narg(cursor_id)::uuid
    )
  )
ORDER BY sd.created_at ASC, sd.id ASC
LIMIT @page_limit;

-- name: CreateSkillDistribution :one
INSERT INTO skill_distributions (
  project_id,
  skill_id,
  plugin_id,
  assistant_id,
  pinned_version_id,
  channel,
  created_by_user_id
)
SELECT
  s.project_id,
  s.id,
  sqlc.narg(plugin_id)::uuid,
  sqlc.narg(assistant_id)::uuid,
  sqlc.narg(pinned_version_id)::uuid,
  @channel,
  @created_by_user_id
FROM skills s
WHERE s.project_id = @project_id
  AND s.id = @skill_id
  AND s.archived_at IS NULL
  AND (
    (@channel = 'plugin' AND sqlc.narg(plugin_id)::uuid IS NOT NULL AND sqlc.narg(assistant_id)::uuid IS NULL)
    OR (@channel = 'assistant' AND sqlc.narg(assistant_id)::uuid IS NOT NULL AND sqlc.narg(plugin_id)::uuid IS NULL)
  )
RETURNING *;

-- name: UpdateSkillDistribution :one
UPDATE skill_distributions
SET pinned_version_id = sqlc.narg(pinned_version_id)::uuid,
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND skill_id = @skill_id
  AND plugin_id IS NOT DISTINCT FROM sqlc.narg(plugin_id)::uuid
  AND assistant_id IS NOT DISTINCT FROM sqlc.narg(assistant_id)::uuid
  AND channel = @channel
  AND (
    (@channel = 'plugin' AND sqlc.narg(plugin_id)::uuid IS NOT NULL AND sqlc.narg(assistant_id)::uuid IS NULL)
    OR (@channel = 'assistant' AND sqlc.narg(assistant_id)::uuid IS NOT NULL AND sqlc.narg(plugin_id)::uuid IS NULL)
  )
  AND revoked_at IS NULL
RETURNING *;

-- name: RevokeActiveSkillDistribution :one
UPDATE skill_distributions
SET revoked_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND skill_id = @skill_id
  AND plugin_id IS NOT DISTINCT FROM sqlc.narg(plugin_id)::uuid
  AND assistant_id IS NOT DISTINCT FROM sqlc.narg(assistant_id)::uuid
  AND channel = @channel
  AND (
    (@channel = 'plugin' AND sqlc.narg(plugin_id)::uuid IS NOT NULL AND sqlc.narg(assistant_id)::uuid IS NULL)
    OR (@channel = 'assistant' AND sqlc.narg(assistant_id)::uuid IS NOT NULL AND sqlc.narg(plugin_id)::uuid IS NULL)
  )
  AND revoked_at IS NULL
RETURNING *;

-- name: RevokeAllSkillDistributionsBySkill :many
-- The self-join returns the pre-revocation updated_at for audit snapshots.
UPDATE skill_distributions sd
SET revoked_at = clock_timestamp(),
    updated_at = clock_timestamp()
FROM skill_distributions prev
JOIN LATERAL (
  SELECT sv.id
  FROM skill_versions sv
  LEFT JOIN skill_version_origins svo
    ON svo.project_id = prev.project_id
    AND svo.skill_id = sv.skill_id
    AND svo.skill_version_id = sv.id
  WHERE sv.skill_id = prev.skill_id
    AND sv.spec_valid IS TRUE
    AND (prev.pinned_version_id IS NULL OR sv.id = prev.pinned_version_id)
  ORDER BY (svo.origin IS DISTINCT FROM 'captured') DESC, sv.created_at DESC, sv.id DESC
  LIMIT 1
) resolved ON TRUE
WHERE prev.id = sd.id
  AND sd.project_id = @project_id
  AND sd.skill_id = @skill_id
  AND sd.revoked_at IS NULL
RETURNING sqlc.embed(sd), prev.updated_at AS previous_updated_at, resolved.id AS resolved_version_id;

-- name: ListPendingSkillObservations :many
-- One keyset page of activations still awaiting efficacy enqueue, ordered on
-- the unique (seen_at, id) key so the caller can page through the whole pending
-- set inside a single pass.
--
-- The predicate is the activation's own — reconciled, unstamped, carrying the
-- session and skill version a scoring unit needs. Chat resolution is not part
-- of it: the chat id is derived from the session id in Go and the insert
-- rechecks the chat, so an activation whose chat is missing, empty or still
-- live costs one page slot and is then paged past. Nothing can sit at the head
-- of the queue and starve the scoreable activations behind it.
--
-- The actor columns ride along because the session id is client-supplied on the
-- dev surface: the insert binds a dev unit to a chat only when the activation's
-- own actor matches that chat's, so an activation naming someone else's session
-- can never associate their transcript.
SELECT
  so.id,
  so.session_id::text AS session_id,
  COALESCE(so.user_id, '')::text AS user_id,
  COALESCE(so.user_email, '')::text AS user_email,
  so.seen_at,
  so.skill_id::uuid AS skill_id,
  so.skill_version_id::uuid AS skill_version_id,
  sv.canonical_sha256,
  -- Surface mirrors ListPendingSkillSessionVersions: assistant/assistants
  -- producers map to assistant, every supported dev producer maps to dev.
  CASE WHEN so.provider IN ('assistant', 'assistants') THEN 'assistant' ELSE 'dev' END::text AS surface
FROM skill_observations so
JOIN skills s
  ON s.project_id = so.project_id
  AND s.id = so.skill_id
JOIN skill_versions sv
  ON sv.skill_id = s.id
  AND sv.id = so.skill_version_id
WHERE so.project_id = @project_id
  AND so.reconciled_at IS NOT NULL
  AND so.efficacy_enqueued_at IS NULL
  AND so.session_id IS NOT NULL
  AND so.skill_version_id IS NOT NULL
  AND (
    sqlc.narg(after_seen_at)::timestamptz IS NULL
    OR (so.seen_at, so.id) > (sqlc.narg(after_seen_at)::timestamptz, sqlc.narg(after_id)::uuid)
  )
ORDER BY so.seen_at, so.id
LIMIT @batch_size;

-- name: ListDeletedSkillEfficacyChatIDs :many
SELECT id
FROM chats
WHERE project_id = @project_id
  AND id = ANY(@chat_ids::uuid[])
  AND deleted IS TRUE;

-- name: RetireSkillObservationsForDeletedChats :execrows
-- A deleted chat can never become scoreable. Marking only observations Go
-- associated with confirmed deleted chat ids removes them from the safety sweep
-- without retiring missing chats whose transcript may still arrive late.
UPDATE skill_observations
SET efficacy_enqueued_at = clock_timestamp()
WHERE project_id = @project_id
  AND id = ANY(@observation_ids::uuid[])
  AND efficacy_enqueued_at IS NULL;

-- name: MarkSkillObservationsEfficacyEnqueued :execrows
UPDATE skill_observations
SET efficacy_enqueued_at = clock_timestamp()
WHERE project_id = @project_id
  AND id = ANY(@observation_ids::uuid[])
  AND efficacy_enqueued_at IS NULL;

-- name: InsertSkillShareLink :one
-- ON CONFLICT DO NOTHING turns the astronomically unlikely token collision
-- into a no-rows result the caller can retry without aborting its transaction.
INSERT INTO skill_share_links (
  project_id,
  skill_id,
  token,
  created_by_user_id
)
SELECT
  s.project_id,
  s.id,
  @token,
  @created_by_user_id
FROM skills s
WHERE s.project_id = @project_id
  AND s.id = @skill_id
  AND s.archived_at IS NULL
ON CONFLICT (token) DO NOTHING
RETURNING *;

-- name: GetActiveSkillShareLink :one
SELECT *
FROM skill_share_links
WHERE project_id = @project_id
  AND skill_id = @skill_id
  AND revoked_at IS NULL;

-- name: RevokeSkillShareLink :one
UPDATE skill_share_links
SET revoked_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND skill_id = @skill_id
  AND revoked_at IS NULL
RETURNING *;

-- name: GetSharedSkillByToken :one
-- Public read for the unauthenticated share-link endpoint. The join pins the
-- share link to its owning project's skill and the lateral picks the latest
-- version by creation order.
SELECT
  s.name,
  s.display_name,
  s.summary,
  latest.content,
  latest.created_at AS version_created_at
FROM skill_share_links l
JOIN skills s
  ON s.project_id = l.project_id
  AND s.id = l.skill_id
  AND s.archived_at IS NULL
JOIN LATERAL (
  SELECT sv.content, sv.created_at
  FROM skill_versions sv
  WHERE sv.skill_id = l.skill_id
  ORDER BY sv.created_at DESC, sv.id DESC
  LIMIT 1
) latest ON TRUE
WHERE l.token = @token
  AND l.revoked_at IS NULL;

-- name: ListSkillEfficacyChatStates :many
-- Classifies the chats a unit-source page derived: present-and-live chats are
-- enqueued, deleted ones have their activations retired, and absent ones are
-- left unstamped for a later pass because their transcript may still arrive.
SELECT id, deleted
FROM chats
WHERE project_id = @project_id
  AND id = ANY(@chat_ids::uuid[]);

-- name: GetSkillEfficacyProjectOrganization :one
SELECT organization_id
FROM projects
WHERE id = @project_id::uuid
  AND deleted IS FALSE;

-- name: ListSkillEfficacyJudgeSkills :many
-- The skills a session activated, as the session judge sees them: one row per
-- (skill version, surface) with the activation time and the authored content.
-- The actor binding mirrors the legacy enqueue's: a dev activation counts only
-- when its actor owns the chat — its user id against chats.user_id or its email
-- against chats.external_user_id — so a client-supplied session id can never
-- have another actor's transcript judged against it. Assistant session ids are
-- server-generated and their activations carry no actor, so they bind by the
-- session alone.
SELECT
  so.skill_id::uuid AS skill_id,
  so.skill_version_id::uuid AS skill_version_id,
  sv.canonical_sha256,
  s.name AS skill_name,
  sv.content AS skill_content,
  CASE WHEN so.provider IN ('assistant', 'assistants') THEN 'assistant' ELSE 'dev' END::text AS surface,
  max(so.seen_at)::timestamptz AS activated_at
FROM skill_observations so
JOIN skills s
  ON s.project_id = so.project_id
  AND s.id = so.skill_id
JOIN skill_versions sv
  ON sv.skill_id = s.id
  AND sv.id = so.skill_version_id
JOIN chats c
  ON c.id = @chat_id::uuid
  AND c.project_id = so.project_id
  AND c.deleted IS FALSE
WHERE so.project_id = @project_id
  AND so.session_id = @session_id::text
  AND so.reconciled_at IS NOT NULL
  AND so.skill_version_id IS NOT NULL
  AND (
    so.provider IN ('assistant', 'assistants')
    OR (COALESCE(so.user_id, '') <> '' AND c.user_id = so.user_id)
    OR (COALESCE(so.user_email, '') <> '' AND c.external_user_id = so.user_email)
  )
GROUP BY so.skill_id, so.skill_version_id, sv.canonical_sha256, s.name, sv.content, surface
ORDER BY activated_at DESC;
