-- name: LockSkillName :exec
SELECT pg_advisory_xact_lock(hashtextextended('skill:' || (@project_id::uuid)::text || ':' || @name::text, 0));

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
    SELECT so.project_id, 1 AS sequence
    FROM skill_observations so
    WHERE so.reconciled_at IS NULL
      AND so.project_id > @project_cursor
    ORDER BY so.project_id
    LIMIT 1
  )
  UNION ALL
  SELECT next_project.project_id, current_project.sequence + 1
  FROM pending_projects current_project
  CROSS JOIN LATERAL (
    SELECT so.project_id
    FROM skill_observations so
    WHERE so.reconciled_at IS NULL
      AND so.project_id > current_project.project_id
    ORDER BY so.project_id
    LIMIT 1
  ) next_project
  WHERE current_project.sequence < @page_limit
)
SELECT project_id
FROM pending_projects
ORDER BY sequence
LIMIT @page_limit;

-- name: ClaimPendingSkillObservations :many
SELECT *
FROM skill_observations
WHERE project_id = @project_id
  AND reconciled_at IS NULL
ORDER BY seen_at, id
LIMIT @batch_size
FOR UPDATE SKIP LOCKED;

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
      reconciled_at = clock_timestamp(),
      reconcile_error_code = NULL
  WHERE so.project_id = @project_id
    AND so.id = ANY(@observation_ids::uuid[])
    AND so.reconciled_at IS NULL
  RETURNING so.seen_at, so.provider, so.source, so.source_level, so.raw_sha256
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
      OR lower(btrim(completed.provider)) IN ('anthropic', 'claude', 'claude-code', 'openai', 'codex', 'cursor')
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
  AND s.archived_at IS NULL
  AND evidence.seen_count > 0
RETURNING evidence.seen_count;

-- name: FailSkillObservationReconciliations :execrows
UPDATE skill_observations
SET reconciled_at = clock_timestamp(),
    reconcile_error_code = @error_code,
    skill_id = NULL
WHERE project_id = @project_id
  AND id = ANY(@observation_ids::uuid[])
  AND reconciled_at IS NULL;

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

-- name: UpdateSkill :one
UPDATE skills
SET display_name = @display_name,
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
  sqlc.embed(latest),
  state.version_count
FROM skills s
JOIN LATERAL (
  SELECT
    sv.id AS latest_version_id,
    COUNT(*) OVER()::bigint AS version_count
  FROM skill_versions sv
  WHERE sv.skill_id = s.id
  ORDER BY sv.created_at DESC, sv.id DESC
  LIMIT 1
) state ON TRUE
JOIN skill_versions latest
  ON latest.id = state.latest_version_id
  AND latest.skill_id = s.id
WHERE s.project_id = @project_id
  AND s.id = @skill_id
  AND s.archived_at IS NULL;

-- name: GetSkillState :one
SELECT
  state.latest_version_id,
  state.version_count
FROM skills s
JOIN LATERAL (
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
  latest.id AS latest_version_id,
  latest.version_count
FROM skills s
JOIN LATERAL (
  SELECT
    sv.id,
    COUNT(*) OVER()::bigint AS version_count
  FROM skill_versions sv
  WHERE sv.skill_id = s.id
  ORDER BY sv.created_at DESC, sv.id DESC
  LIMIT 1
) latest ON TRUE
WHERE s.project_id = @project_id
  AND s.archived_at IS NULL
  AND (
    sqlc.narg(cursor_name)::text IS NULL
    OR s.name > sqlc.narg(cursor_name)::text
  )
ORDER BY s.name ASC
LIMIT @page_limit;

-- name: ListSkillVersions :many
SELECT sv.*
FROM skill_versions sv
JOIN skills s ON s.id = sv.skill_id
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
  AND sd.plugin_id = @plugin_id
  AND sd.channel = 'plugin'
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
JOIN plugins pl ON pl.id = sd.plugin_id
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
  pinned_version_id,
  channel,
  created_by_user_id
)
SELECT
  s.project_id,
  s.id,
  @plugin_id::uuid,
  sqlc.narg(pinned_version_id)::uuid,
  'plugin',
  @created_by_user_id
FROM skills s
WHERE s.project_id = @project_id
  AND s.id = @skill_id
  AND s.archived_at IS NULL
RETURNING *;

-- name: UpdateSkillDistribution :one
UPDATE skill_distributions
SET pinned_version_id = sqlc.narg(pinned_version_id)::uuid,
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND skill_id = @skill_id
  AND plugin_id = @plugin_id
  AND channel = 'plugin'
  AND revoked_at IS NULL
RETURNING *;

-- name: RevokeActiveSkillDistribution :one
UPDATE skill_distributions
SET revoked_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND skill_id = @skill_id
  AND plugin_id = @plugin_id
  AND channel = 'plugin'
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
