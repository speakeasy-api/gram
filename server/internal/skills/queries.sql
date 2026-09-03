-- name: LockSkillName :exec
SELECT pg_advisory_xact_lock(hashtextextended('skill:' || (@project_id::uuid)::text || ':' || @name::text, 0));

-- name: LockSkillObservationReconciliation :exec
SELECT pg_advisory_xact_lock(hashtextextended('skill-observations:' || (@project_id::uuid)::text, 0));

-- name: CreateSkillFeedback :one
INSERT INTO skill_feedback (
  id,
  project_id,
  skill_id,
  skill_version_id,
  skill_name,
  source,
  outcome,
  note,
  session_id,
  user_id,
  user_email
) VALUES (
  COALESCE(sqlc.narg(id)::uuid, generate_uuidv7()),
  @project_id,
  sqlc.narg(skill_id)::uuid,
  sqlc.narg(skill_version_id)::uuid,
  @skill_name,
  @source,
  @outcome,
  sqlc.narg(note)::text,
  sqlc.narg(session_id)::text,
  sqlc.narg(user_id)::text,
  sqlc.narg(user_email)::text
)
ON CONFLICT (project_id, id) DO UPDATE
SET id = EXCLUDED.id
RETURNING *;

-- name: GetActiveSkillByName :one
SELECT *
FROM skills
WHERE project_id = @project_id
  AND name = @name
  AND archived_at IS NULL;

-- name: ListRecentSkillFeedback :many
SELECT *
FROM skill_feedback
WHERE project_id = @project_id
  AND skill_name = @skill_name
ORDER BY created_at DESC, id DESC
LIMIT GREATEST(@page_limit::int, 0);

-- name: CountSkillFeedbackOutcomes :one
SELECT
  COUNT(*)::bigint AS total,
  COUNT(*) FILTER (WHERE outcome = 'helped')::bigint AS helped,
  COUNT(*) FILTER (WHERE outcome = 'partially_helped')::bigint AS partially_helped,
  COUNT(*) FILTER (WHERE outcome = 'did_not_help')::bigint AS did_not_help,
  COUNT(*) FILTER (WHERE outcome = 'misleading')::bigint AS misleading,
  COUNT(*) FILTER (WHERE outcome = 'harmful')::bigint AS harmful
FROM skill_feedback
WHERE project_id = @project_id
  AND skill_id = @skill_id;

-- name: GetSkillFeedbackMetrics :one
SELECT
  COUNT(*) FILTER (
    WHERE feedback.created_at >= @window_start
      AND feedback.created_at < @window_end
  )::bigint AS feedback_in_window,
  COUNT(*) FILTER (WHERE feedback.reviewed_at IS NULL)::bigint AS unreviewed,
  (
    SELECT COUNT(*)::bigint
    FROM skill_observations observation
    WHERE observation.project_id = @project_id
      AND observation.skill_id = @skill_id
      AND observation.reconciled_at IS NOT NULL
      AND observation.reconcile_error_code IS NULL
      AND observation.seen_at >= @window_start
      AND observation.seen_at < @window_end
  ) AS activations_in_window,
  (
    SELECT COUNT(*)::bigint
    FROM skill_observations observation
    WHERE observation.project_id = @project_id
      AND observation.skill_id = @skill_id
      AND observation.reconciled_at IS NOT NULL
      AND observation.reconcile_error_code IS NULL
      AND observation.session_id IS NOT NULL
      AND observation.seen_at >= @window_start
      AND observation.seen_at < @window_end
      AND EXISTS (
        SELECT 1
        FROM skill_feedback paired
        WHERE paired.project_id = observation.project_id
          AND paired.skill_id = observation.skill_id
          AND paired.session_id = observation.session_id
          AND paired.created_at >= @window_start
          AND paired.created_at < @window_end
      )
  ) AS feedback_activations_in_window,
  (
    SELECT COUNT(DISTINCT converted.id)::bigint
    FROM skill_feedback converted
    JOIN skill_edit_suggestion_feedback link
      ON link.project_id = converted.project_id
      AND link.feedback_id = converted.id
    JOIN skill_edit_suggestion_changes change
      ON change.project_id = link.project_id
      AND change.id = link.change_id
    JOIN skill_edit_suggestions suggestion
      ON suggestion.project_id = change.project_id
      AND suggestion.id = change.suggestion_id
    WHERE converted.project_id = @project_id
      AND converted.skill_id = @skill_id
      AND suggestion.skill_id = @skill_id
  ) AS converted
FROM skill_feedback feedback
WHERE feedback.project_id = @project_id
  AND feedback.skill_id = @skill_id;

-- name: ListSkillFeedbackTimeline :many
WITH buckets AS (
  SELECT generate_series(
    date_trunc('day', @window_start::timestamptz, 'UTC'),
    date_trunc('day', @window_end::timestamptz, 'UTC'),
    interval '1 day'
  )::timestamptz AS bucket_start
)
SELECT
  buckets.bucket_start,
  COUNT(feedback.id)::bigint AS feedback_count
FROM buckets
LEFT JOIN skill_feedback feedback
  ON feedback.project_id = @project_id
  AND feedback.skill_id = @skill_id
  AND feedback.created_at >= buckets.bucket_start
  AND feedback.created_at < buckets.bucket_start + interval '1 day'
  AND feedback.created_at >= @window_start
  AND feedback.created_at < @window_end
GROUP BY buckets.bucket_start
ORDER BY buckets.bucket_start;

-- name: ListSkillFeedbackByID :many
SELECT *
FROM skill_feedback
WHERE project_id = @project_id
  AND skill_id = @skill_id
  AND (
    sqlc.narg(cursor_created_at)::timestamptz IS NULL
    OR (created_at, id) < (
      sqlc.narg(cursor_created_at)::timestamptz,
      sqlc.narg(cursor_id)::uuid
    )
  )
ORDER BY created_at DESC, id DESC
LIMIT @page_limit;

-- name: ListUnreviewedSkillFeedback :many
SELECT *
FROM skill_feedback
WHERE project_id = @project_id
  AND skill_name = @skill_name
  AND reviewed_at IS NULL
ORDER BY created_at, id
LIMIT GREATEST(@page_limit::int, 0);

-- name: MarkSkillFeedbackReviewed :execrows
UPDATE skill_feedback
SET reviewed_at = clock_timestamp()
WHERE project_id = @project_id
  AND skill_name = @skill_name
  AND id = ANY(@ids::uuid[])
  AND reviewed_at IS NULL;

-- name: CountUnreviewedSkillFeedback :one
SELECT COUNT(*)
FROM skill_feedback
WHERE project_id = @project_id
  AND skill_name = @skill_name
  AND reviewed_at IS NULL;

-- name: ResolveSkillSuggestionBase :one
WITH base AS (
  SELECT sv.*
  FROM skills s
  JOIN skill_versions sv ON sv.skill_id = s.id
  LEFT JOIN skill_version_origins svo
    ON svo.project_id = s.project_id
    AND svo.skill_id = sv.skill_id
    AND svo.skill_version_id = sv.id
  WHERE s.project_id = @project_id
    AND s.id = @skill_id
    AND s.archived_at IS NULL
    AND sv.spec_valid IS TRUE
  ORDER BY (svo.origin IS DISTINCT FROM 'captured') DESC, COALESCE(sv.promoted_at, sv.created_at) DESC, sv.id DESC
  LIMIT 1
)
SELECT
  base.id AS base_version_id,
  COALESCE(base.promoted_at, base.created_at) AS base_floor_reference_at,
  base.content AS base_content,
  base.canonical_sha256 AS base_canonical_sha256,
  COALESCE(predecessor.id, '00000000-0000-0000-0000-000000000000'::uuid) AS predecessor_version_id,
  COALESCE(predecessor.content, '')::text AS predecessor_content,
  COALESCE(predecessor.canonical_sha256, '')::text AS predecessor_canonical_sha256
FROM base
LEFT JOIN skill_version_lineages lineage
  ON lineage.skill_id = base.skill_id
  AND lineage.skill_version_id = base.id
LEFT JOIN LATERAL (
  SELECT previous.id, previous.content, previous.canonical_sha256
  FROM skill_versions previous
  LEFT JOIN skill_version_origins previous_origin
    ON previous_origin.project_id = @project_id
    AND previous_origin.skill_id = previous.skill_id
    AND previous_origin.skill_version_id = previous.id
  WHERE previous.skill_id = base.skill_id
    AND (
      (base.promoted_at IS NULL AND previous.id = lineage.derived_from_version_id)
      OR (
        (base.promoted_at IS NOT NULL OR lineage.derived_from_version_id IS NULL)
        AND previous.spec_valid IS TRUE
        AND (COALESCE(previous.promoted_at, previous.created_at), previous.id) < (COALESCE(base.promoted_at, base.created_at), base.id)
      )
    )
  ORDER BY (previous_origin.origin IS DISTINCT FROM 'captured') DESC, COALESCE(previous.promoted_at, previous.created_at) DESC, previous.id DESC
  LIMIT 1
) predecessor ON TRUE;

-- name: ResolveSkillRegressionBases :many
WITH bases AS (
  SELECT DISTINCT ON (s.id)
    s.id AS skill_id,
    sv.id,
    sv.promoted_at,
    sv.created_at
  FROM skills s
  JOIN skill_versions sv ON sv.skill_id = s.id
  LEFT JOIN skill_version_origins svo
    ON svo.project_id = s.project_id
    AND svo.skill_id = sv.skill_id
    AND svo.skill_version_id = sv.id
  WHERE s.project_id = @project_id
    AND s.id = ANY(@skill_ids::uuid[])
    AND s.archived_at IS NULL
    AND sv.spec_valid IS TRUE
  ORDER BY s.id, (svo.origin IS DISTINCT FROM 'captured') DESC, COALESCE(sv.promoted_at, sv.created_at) DESC, sv.id DESC
)
SELECT
  base.skill_id,
  base.id AS base_version_id,
  COALESCE(predecessor.id, '00000000-0000-0000-0000-000000000000'::uuid) AS predecessor_version_id
FROM bases base
LEFT JOIN skill_version_lineages lineage
  ON lineage.skill_id = base.skill_id
  AND lineage.skill_version_id = base.id
LEFT JOIN LATERAL (
  SELECT previous.id
  FROM skill_versions previous
  LEFT JOIN skill_version_origins previous_origin
    ON previous_origin.project_id = @project_id
    AND previous_origin.skill_id = previous.skill_id
    AND previous_origin.skill_version_id = previous.id
  WHERE previous.skill_id = base.skill_id
    AND (
      (base.promoted_at IS NULL AND previous.id = lineage.derived_from_version_id)
      OR (
        (base.promoted_at IS NOT NULL OR lineage.derived_from_version_id IS NULL)
        AND previous.spec_valid IS TRUE
        AND (COALESCE(previous.promoted_at, previous.created_at), previous.id) < (COALESCE(base.promoted_at, base.created_at), base.id)
      )
    )
  ORDER BY (previous_origin.origin IS DISTINCT FROM 'captured') DESC, COALESCE(previous.promoted_at, previous.created_at) DESC, previous.id DESC
  LIMIT 1
) predecessor ON TRUE
ORDER BY base.skill_id;

-- name: GetOpenSkillEditSuggestion :one
SELECT suggestion.*
FROM skill_edit_suggestions suggestion
JOIN skills s
  ON s.project_id = suggestion.project_id
  AND s.id = suggestion.skill_id
WHERE suggestion.project_id = @project_id
  AND suggestion.skill_id = @skill_id
  AND suggestion.status = 'open'
  AND s.archived_at IS NULL;

-- name: ListOpenSkillEditSuggestions :many
SELECT
  sqlc.embed(suggestion),
  s.name AS skill_name,
  s.display_name AS skill_display_name,
  base.content AS base_content,
  (
    SELECT COUNT(*)
    FROM skill_edit_suggestion_feedback link
    JOIN skill_edit_suggestion_changes change
      ON change.project_id = link.project_id
      AND change.id = link.change_id
    WHERE link.project_id = suggestion.project_id
      AND change.suggestion_id = suggestion.id
  ) AS feedback_count,
  (
    SELECT COUNT(DISTINCT feedback.session_id)
    FROM skill_edit_suggestion_feedback link
    JOIN skill_edit_suggestion_changes change
      ON change.project_id = link.project_id
      AND change.id = link.change_id
    JOIN skill_feedback feedback
      ON feedback.project_id = link.project_id
      AND feedback.id = link.feedback_id
    WHERE link.project_id = suggestion.project_id
      AND change.suggestion_id = suggestion.id
      AND feedback.session_id IS NOT NULL
  ) AS feedback_session_count
FROM skill_edit_suggestions suggestion
JOIN skills s
  ON s.project_id = suggestion.project_id
  AND s.id = suggestion.skill_id
JOIN skill_versions base
  ON base.skill_id = suggestion.skill_id
  AND base.id = suggestion.base_version_id
WHERE suggestion.project_id = @project_id
  AND suggestion.status = 'open'
  AND s.archived_at IS NULL
  AND (sqlc.narg(skill_id)::uuid IS NULL OR suggestion.skill_id = sqlc.narg(skill_id)::uuid)
  AND (
    sqlc.narg(cursor_created_at)::timestamptz IS NULL
    OR (suggestion.created_at, suggestion.id) < (
      sqlc.narg(cursor_created_at)::timestamptz,
      sqlc.narg(cursor_id)::uuid
    )
  )
ORDER BY suggestion.created_at DESC, suggestion.id DESC
LIMIT @page_limit;

-- name: CountOpenSkillEditSuggestions :one
SELECT COUNT(*)
FROM skill_edit_suggestions suggestion
JOIN skills s
  ON s.project_id = suggestion.project_id
  AND s.id = suggestion.skill_id
WHERE suggestion.project_id = @project_id
  AND suggestion.status = 'open'
  AND s.archived_at IS NULL
  AND (sqlc.narg(skill_id)::uuid IS NULL OR suggestion.skill_id = sqlc.narg(skill_id)::uuid);

-- name: ListOpenSkillEditSuggestionsForApproval :many
SELECT
  suggestion.id,
  suggestion.skill_id,
  s.name AS skill_name,
  s.display_name AS skill_display_name
FROM skill_edit_suggestions suggestion
JOIN skills s
  ON s.project_id = suggestion.project_id
  AND s.id = suggestion.skill_id
WHERE suggestion.project_id = @project_id
  AND s.archived_at IS NULL
  AND (
    (
      COALESCE(cardinality(@suggestion_ids::uuid[]), 0) = 0
      AND suggestion.status = 'open'
    )
    OR suggestion.id = ANY(@suggestion_ids::uuid[])
  )
ORDER BY suggestion.created_at DESC, suggestion.id DESC;

-- name: GetSkillEditSuggestionDetails :one
SELECT
  sqlc.embed(suggestion),
  s.name AS skill_name,
  s.display_name AS skill_display_name,
  base.content AS base_content,
  (
    SELECT COUNT(*)
    FROM skill_edit_suggestion_feedback link
    JOIN skill_edit_suggestion_changes change
      ON change.project_id = link.project_id
      AND change.id = link.change_id
    WHERE link.project_id = suggestion.project_id
      AND change.suggestion_id = suggestion.id
  ) AS feedback_count,
  (
    SELECT COUNT(DISTINCT feedback.session_id)
    FROM skill_edit_suggestion_feedback link
    JOIN skill_edit_suggestion_changes change
      ON change.project_id = link.project_id
      AND change.id = link.change_id
    JOIN skill_feedback feedback
      ON feedback.project_id = link.project_id
      AND feedback.id = link.feedback_id
    WHERE link.project_id = suggestion.project_id
      AND change.suggestion_id = suggestion.id
      AND feedback.session_id IS NOT NULL
  ) AS feedback_session_count
FROM skill_edit_suggestions suggestion
JOIN skills s
  ON s.project_id = suggestion.project_id
  AND s.id = suggestion.skill_id
JOIN skill_versions base
  ON base.skill_id = suggestion.skill_id
  AND base.id = suggestion.base_version_id
WHERE suggestion.project_id = @project_id
  AND suggestion.id = @id
  AND s.archived_at IS NULL;

-- name: GetSkillEditSuggestionForUpdate :one
SELECT suggestion.*
FROM skill_edit_suggestions suggestion
JOIN skills s
  ON s.project_id = suggestion.project_id
  AND s.id = suggestion.skill_id
WHERE suggestion.project_id = @project_id
  AND suggestion.id = @id
  AND s.archived_at IS NULL
FOR UPDATE OF suggestion;

-- name: ApproveOpenSkillEditSuggestion :one
UPDATE skill_edit_suggestions suggestion
SET status = 'approved',
    approved_by_user_id = @approved_by_user_id,
    approved_at = clock_timestamp(),
    updated_at = clock_timestamp()
FROM skills s
WHERE suggestion.project_id = @project_id
  AND suggestion.skill_id = @skill_id
  AND suggestion.id = @id
  AND suggestion.status = 'open'
  AND s.project_id = suggestion.project_id
  AND s.id = suggestion.skill_id
  AND s.archived_at IS NULL
RETURNING suggestion.*;

-- name: SupersedeOpenSkillEditSuggestionByID :one
UPDATE skill_edit_suggestions suggestion
SET status = 'superseded',
    updated_at = clock_timestamp()
FROM skills s
WHERE suggestion.project_id = @project_id
  AND suggestion.skill_id = @skill_id
  AND suggestion.id = @id
  AND suggestion.status = 'open'
  AND s.project_id = suggestion.project_id
  AND s.id = suggestion.skill_id
  AND s.archived_at IS NULL
RETURNING suggestion.*;

-- name: GetLatestSkillEditSuggestion :one
SELECT suggestion.*
FROM skill_edit_suggestions suggestion
JOIN skills s
  ON s.project_id = suggestion.project_id
  AND s.id = suggestion.skill_id
WHERE suggestion.project_id = @project_id
  AND suggestion.skill_id = @skill_id
  AND s.archived_at IS NULL
ORDER BY suggestion.created_at DESC, suggestion.id DESC
LIMIT 1;

-- name: CountScoredSkillEvaluationsAfter :one
SELECT COUNT(*)
FROM skill_efficacy_evaluations evaluation
JOIN skills s
  ON s.project_id = evaluation.project_id
  AND s.id = evaluation.skill_id
WHERE evaluation.project_id = @project_id
  AND evaluation.skill_id = @skill_id
  AND evaluation.skill_version_id = @base_version_id
  AND evaluation.state = 'scored'
  AND evaluation.scored_at > @scored_after
  AND s.archived_at IS NULL;

-- name: DismissSkillEditSuggestion :one
UPDATE skill_edit_suggestions suggestion
SET status = 'dismissed',
    updated_at = clock_timestamp()
FROM skills s
WHERE suggestion.project_id = @project_id
  AND suggestion.skill_id = @skill_id
  AND suggestion.id = @id
  AND suggestion.status = 'open'
  AND s.project_id = suggestion.project_id
  AND s.id = suggestion.skill_id
  AND s.archived_at IS NULL
RETURNING suggestion.*;

-- name: CreateSkillEditSuggestion :one
INSERT INTO skill_edit_suggestions (
  project_id,
  skill_id,
  base_version_id,
  rationale,
  scored_session_count
)
SELECT
  s.project_id,
  s.id,
  sv.id,
  @rationale,
  @scored_session_count
FROM skills s
JOIN skill_versions sv
  ON sv.skill_id = s.id
  AND sv.id = @base_version_id
WHERE s.project_id = @project_id
  AND s.id = @skill_id
  AND s.archived_at IS NULL
RETURNING *;

-- name: UpdateOpenSkillEditSuggestion :one
UPDATE skill_edit_suggestions suggestion
SET rationale = @rationale,
    scored_session_count = @scored_session_count,
    updated_at = clock_timestamp()
FROM skills s
WHERE suggestion.project_id = @project_id
  AND suggestion.skill_id = @skill_id
  AND suggestion.base_version_id = @base_version_id
  AND suggestion.status = 'open'
  AND s.project_id = suggestion.project_id
  AND s.id = suggestion.skill_id
  AND s.archived_at IS NULL
RETURNING suggestion.*;

-- name: UpdateLatestSkillEditSuggestionWatermark :one
WITH latest AS (
  SELECT suggestion.id
  FROM skill_edit_suggestions suggestion
  JOIN skills s
    ON s.project_id = suggestion.project_id
    AND s.id = suggestion.skill_id
  WHERE suggestion.project_id = @project_id
    AND suggestion.skill_id = @skill_id
    AND suggestion.base_version_id = @base_version_id
    AND s.archived_at IS NULL
  ORDER BY suggestion.created_at DESC, suggestion.id DESC
  LIMIT 1
)
UPDATE skill_edit_suggestions suggestion
SET scored_session_count = @scored_session_count,
    updated_at = clock_timestamp()
FROM latest
WHERE suggestion.project_id = @project_id
  AND suggestion.skill_id = @skill_id
  AND suggestion.base_version_id = @base_version_id
  AND suggestion.id = latest.id
RETURNING suggestion.*;

-- name: CreateSkillEditSuggestionWatermark :one
INSERT INTO skill_edit_suggestions (
  project_id,
  skill_id,
  base_version_id,
  rationale,
  status,
  scored_session_count
)
SELECT
  s.project_id,
  s.id,
  sv.id,
  @rationale,
  'superseded',
  @scored_session_count
FROM skills s
JOIN skill_versions sv
  ON sv.skill_id = s.id
  AND sv.id = @base_version_id
WHERE s.project_id = @project_id
  AND s.id = @skill_id
  AND s.archived_at IS NULL
RETURNING *;

-- name: LinkSkillEditSuggestionFeedback :execrows
INSERT INTO skill_edit_suggestion_feedback (
  project_id,
  change_id,
  feedback_id
)
SELECT
  feedback.project_id,
  @change_id,
  feedback.id
FROM skill_feedback feedback
WHERE feedback.project_id = @project_id
  AND feedback.id = ANY(@feedback_ids::uuid[])
ON CONFLICT DO NOTHING;

-- name: CountSkillEditSuggestionFeedback :one
SELECT COUNT(*)
FROM skill_edit_suggestion_feedback link
JOIN skill_edit_suggestion_changes change
  ON change.project_id = link.project_id
  AND change.id = link.change_id
WHERE link.project_id = @project_id
  AND change.suggestion_id = @suggestion_id;

-- name: CreateSkillEditSuggestionChange :one
INSERT INTO skill_edit_suggestion_changes (
  project_id,
  suggestion_id,
  proposed_diff,
  rationale,
  position
)
SELECT
  suggestion.project_id,
  suggestion.id,
  @proposed_diff,
  @rationale,
  @position
FROM skill_edit_suggestions suggestion
WHERE suggestion.project_id = @project_id
  AND suggestion.id = @suggestion_id
RETURNING *;

-- name: DeleteSkillEditSuggestionChanges :exec
DELETE FROM skill_edit_suggestion_changes
WHERE project_id = @project_id
  AND suggestion_id = @suggestion_id;

-- name: DeleteSkillEditSuggestionChange :exec
DELETE FROM skill_edit_suggestion_changes
WHERE project_id = @project_id
  AND id = @id;

-- name: DeleteSkillEditSuggestionChangesByIDs :exec
DELETE FROM skill_edit_suggestion_changes
WHERE project_id = @project_id
  AND id = ANY (@ids::uuid[]);

-- name: RebaseSkillEditSuggestionChange :exec
UPDATE skill_edit_suggestion_changes
SET proposed_diff = @proposed_diff,
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND id = @id;

-- name: ListSkillEditSuggestionChanges :many
SELECT
  change.*,
  (
    SELECT COUNT(*)
    FROM skill_edit_suggestion_feedback link
    WHERE link.project_id = change.project_id
      AND link.change_id = change.id
  )::bigint AS feedback_count,
  (
    SELECT COUNT(DISTINCT feedback.session_id)
    FROM skill_edit_suggestion_feedback link
    JOIN skill_feedback feedback
      ON feedback.project_id = link.project_id
      AND feedback.id = link.feedback_id
    WHERE link.project_id = change.project_id
      AND link.change_id = change.id
      AND feedback.session_id IS NOT NULL
  )::bigint AS feedback_session_count
FROM skill_edit_suggestion_changes change
WHERE change.project_id = @project_id
  AND change.suggestion_id = ANY(@suggestion_ids::uuid[])
ORDER BY change.suggestion_id, change.position, change.id;

-- name: GetSkillEditSuggestionChange :one
SELECT change.*, suggestion.skill_id, suggestion.base_version_id, suggestion.status
FROM skill_edit_suggestion_changes change
JOIN skill_edit_suggestions suggestion
  ON suggestion.project_id = change.project_id
  AND suggestion.id = change.suggestion_id
WHERE change.project_id = @project_id
  AND change.id = @id;

-- name: RebaseOpenSkillEditSuggestion :one
UPDATE skill_edit_suggestions suggestion
SET base_version_id = @base_version_id,
    updated_at = clock_timestamp()
FROM skills s
WHERE suggestion.project_id = @project_id
  AND suggestion.skill_id = @skill_id
  AND suggestion.id = @id
  AND suggestion.status = 'open'
  AND s.project_id = suggestion.project_id
  AND s.id = suggestion.skill_id
  AND s.archived_at IS NULL
RETURNING suggestion.*;

-- name: SupersedeOpenSkillEditSuggestion :one
UPDATE skill_edit_suggestions suggestion
SET status = 'superseded',
    updated_at = clock_timestamp()
FROM skills s
WHERE suggestion.project_id = @project_id
  AND suggestion.skill_id = @skill_id
  AND suggestion.base_version_id <> @current_base_version_id
  AND suggestion.status = 'open'
  AND s.project_id = suggestion.project_id
  AND s.id = suggestion.skill_id
  AND s.archived_at IS NULL
RETURNING suggestion.*;

-- name: ListRecentlyActiveSkills :many
SELECT *
FROM skills
WHERE project_id = @project_id
  AND archived_at IS NULL
  AND last_seen_at >= @active_since
  AND EXISTS (
    SELECT 1
    FROM skill_versions sv
    WHERE sv.skill_id = skills.id
      AND sv.spec_valid IS TRUE
  )
  AND (
    sqlc.narg(cursor_last_seen_at)::timestamptz IS NULL
    OR (last_seen_at, id) < (
      sqlc.narg(cursor_last_seen_at)::timestamptz,
      sqlc.narg(cursor_id)::uuid
    )
  )
ORDER BY last_seen_at DESC, id DESC
LIMIT @page_limit;

-- name: ListSkillSuggestionProjects :many
SELECT p.id AS project_id
FROM projects p
WHERE p.deleted IS FALSE
  AND p.id > @after_project_id
  AND EXISTS (
    SELECT 1
    FROM skills s
    WHERE s.project_id = p.id
      AND s.archived_at IS NULL
      AND s.last_seen_at >= @active_since
      AND EXISTS (
        SELECT 1
        FROM skill_versions sv
        WHERE sv.skill_id = s.id
          AND sv.spec_valid IS TRUE
      )
  )
ORDER BY p.id
LIMIT @page_limit;

-- name: ListRecentScoredSkillEvaluationChats :many
WITH recent AS (
  SELECT DISTINCT ON (evaluation.chat_id)
    evaluation.chat_id,
    evaluation.surface,
    evaluation.skill_version_id,
    evaluation.scored_at
  FROM skill_efficacy_evaluations evaluation
  JOIN skills s
    ON s.project_id = evaluation.project_id
    AND s.id = evaluation.skill_id
  JOIN chats c
    ON c.project_id = evaluation.project_id
    AND c.id = evaluation.chat_id
    AND c.deleted IS FALSE
  WHERE evaluation.project_id = @project_id
    AND evaluation.skill_id = @skill_id
    AND evaluation.skill_version_id = ANY(@version_ids::uuid[])
    AND evaluation.state = 'scored'
    AND evaluation.scored_at >= @scored_since
    AND s.archived_at IS NULL
  ORDER BY evaluation.chat_id, evaluation.scored_at DESC, evaluation.id DESC
)
SELECT *
FROM recent
ORDER BY scored_at DESC, chat_id DESC
LIMIT @page_limit;

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

-- name: PromoteSkillVersion :one
UPDATE skill_versions sv
SET promoted_at = clock_timestamp()
FROM skills s
WHERE s.project_id = @project_id
  AND s.id = @skill_id
  AND s.archived_at IS NULL
  AND sv.skill_id = s.id
  AND sv.id = @skill_version_id
  AND sv.spec_valid IS TRUE
RETURNING sv.*;

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
    tags = @tags,
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
  ORDER BY COALESCE(sv.promoted_at, sv.created_at) DESC, sv.id DESC
  LIMIT 1
) state ON TRUE
LEFT JOIN skill_share_links l
  ON l.skill_id = s.id
  AND l.revoked_at IS NULL
WHERE s.project_id = @project_id
  AND s.id = @skill_id
  AND s.archived_at IS NULL;

-- name: ListSkillVersionPromptInjectionFindings :many
SELECT DISTINCT
  COALESCE(rr.rule_id, 'prompt_injection')::text AS rule_id,
  COALESCE(rr.description, 'Detected a prompt injection attempt.')::text AS description,
  COALESCE(rr.confidence, 0)::double precision AS confidence
FROM risk_results rr
JOIN skill_versions sv ON sv.id = rr.skill_version_id
JOIN skills s ON s.id = sv.skill_id
JOIN risk_policies rp
  ON rp.id = rr.risk_policy_id
  AND rp.project_id = s.project_id
  AND rp.enabled IS TRUE
  AND rp.deleted IS FALSE
  AND rr.risk_policy_version = rp.version
WHERE s.project_id = @project_id
  AND s.id = @skill_id
  AND s.archived_at IS NULL
  AND sv.id = @skill_version_id
  AND rr.project_id = s.project_id
  AND rr.source = 'prompt_injection'
  AND rr.found IS TRUE
  AND rr.excluded_at IS NULL
  AND rr.false_positive_at IS NULL
ORDER BY 1, 2, 3 DESC;

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
  ORDER BY COALESCE(sv.promoted_at, sv.created_at) DESC, sv.id DESC
  LIMIT 1
) state ON TRUE
WHERE s.project_id = @project_id
  AND s.id = @skill_id
  AND s.archived_at IS NULL;

-- CountSkills handles empty cursor pages. Keep its filters in sync with ListSkills
-- so normal pages avoid a second query.
-- name: CountSkills :one
SELECT COUNT(*)
FROM skills
WHERE project_id = @project_id
  AND archived_at IS NULL
  AND (
    sqlc.narg(search)::text IS NULL
    OR name ILIKE '%' || sqlc.narg(search)::text || '%'
    OR display_name ILIKE '%' || sqlc.narg(search)::text || '%'
    OR COALESCE(summary, '') ILIKE '%' || sqlc.narg(search)::text || '%'
  )
  AND (
    COALESCE(cardinality(@source_kinds::text[]), 0) = 0
    OR source_kind = ANY(@source_kinds::text[])
  )
  AND (
    COALESCE(cardinality(@classifications::text[]), 0) = 0
    OR classification = ANY(@classifications::text[])
  )
  AND (
    COALESCE(cardinality(@tags::text[]), 0) = 0
    OR tags && @tags::text[]
  );

-- name: ListDistinctSkillTags :many
SELECT DISTINCT tag::text AS tag
FROM skills s
CROSS JOIN LATERAL unnest(s.tags) AS tag
WHERE s.project_id = @project_id
  AND s.archived_at IS NULL
ORDER BY tag;

-- name: ListSkills :many
SELECT
  sqlc.embed(s),
  l.token AS share_token,
  COALESCE(latest.id, '00000000-0000-0000-0000-000000000000'::uuid) AS latest_version_id,
  COALESCE(latest.version_count, 0)::bigint AS version_count,
  EXISTS (
    SELECT 1 FROM skill_versions sv
    WHERE sv.skill_id = s.id AND sv.spec_valid IS TRUE
  )::boolean AS has_valid_version,
  (
    SELECT COUNT(*)
    FROM skills counted
    WHERE counted.project_id = @project_id
      AND counted.archived_at IS NULL
      AND (
        sqlc.narg(search)::text IS NULL
        OR counted.name ILIKE '%' || sqlc.narg(search)::text || '%'
        OR counted.display_name ILIKE '%' || sqlc.narg(search)::text || '%'
        OR COALESCE(counted.summary, '') ILIKE '%' || sqlc.narg(search)::text || '%'
      )
      AND (
        COALESCE(cardinality(@source_kinds::text[]), 0) = 0
        OR counted.source_kind = ANY(@source_kinds::text[])
      )
      AND (
        COALESCE(cardinality(@classifications::text[]), 0) = 0
        OR counted.classification = ANY(@classifications::text[])
      )
      AND (
        COALESCE(cardinality(@tags::text[]), 0) = 0
        OR counted.tags && @tags::text[]
      )
  )::bigint AS total_count
FROM skills s
LEFT JOIN LATERAL (
  SELECT
    sv.id,
    COUNT(*) OVER()::bigint AS version_count
  FROM skill_versions sv
  WHERE sv.skill_id = s.id
  ORDER BY COALESCE(sv.promoted_at, sv.created_at) DESC, sv.id DESC
  LIMIT 1
) latest ON TRUE
LEFT JOIN skill_share_links l
  ON l.skill_id = s.id
  AND l.revoked_at IS NULL
WHERE s.project_id = @project_id
  AND s.archived_at IS NULL
  AND (
    sqlc.narg(search)::text IS NULL
    OR s.name ILIKE '%' || sqlc.narg(search)::text || '%'
    OR s.display_name ILIKE '%' || sqlc.narg(search)::text || '%'
    OR COALESCE(s.summary, '') ILIKE '%' || sqlc.narg(search)::text || '%'
  )
  AND (
    COALESCE(cardinality(@source_kinds::text[]), 0) = 0
    OR s.source_kind = ANY(@source_kinds::text[])
  )
  AND (
    COALESCE(cardinality(@classifications::text[]), 0) = 0
    OR s.classification = ANY(@classifications::text[])
  )
  AND (
    COALESCE(cardinality(@tags::text[]), 0) = 0
    OR s.tags && @tags::text[]
  )
  AND (
    (
      COALESCE(NULLIF(@sort_order::text, ''), 'name') = 'name'
      AND (
        sqlc.narg(cursor_name)::text IS NULL
        OR s.name > sqlc.narg(cursor_name)::text
      )
    )
    OR (
      COALESCE(NULLIF(@sort_order::text, ''), 'name') = 'updated'
      AND (
        sqlc.narg(cursor_updated_at)::timestamptz IS NULL
        OR (s.updated_at, s.id) < (
          sqlc.narg(cursor_updated_at)::timestamptz,
          sqlc.narg(cursor_id)::uuid
        )
      )
    )
  )
ORDER BY
  CASE WHEN COALESCE(NULLIF(@sort_order::text, ''), 'name') = 'name' THEN s.name END ASC,
  CASE WHEN COALESCE(NULLIF(@sort_order::text, ''), 'name') = 'updated' THEN s.updated_at END DESC,
  CASE WHEN COALESCE(NULLIF(@sort_order::text, ''), 'name') = 'updated' THEN s.id END DESC
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
  so.skill_version_id,
  COUNT(*)::bigint AS activation_count
FROM skill_observations so
WHERE so.project_id = @project_id
  AND so.skill_id = sqlc.arg(skill_id)::uuid
  AND so.reconciled_at IS NOT NULL
  AND so.reconcile_error_code IS NULL
  AND so.seen_at >= @window_start
  AND so.seen_at < @window_end
GROUP BY bucket_start, so.skill_version_id
ORDER BY bucket_start ASC, so.skill_version_id ASC NULLS LAST;

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
  ORDER BY (svo.origin IS DISTINCT FROM 'captured') DESC, COALESCE(sv.promoted_at, sv.created_at) DESC, sv.id DESC
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
ORDER BY (svo.origin IS DISTINCT FROM 'captured') DESC, COALESCE(sv.promoted_at, sv.created_at) DESC, sv.id DESC
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
  ORDER BY (svo.origin IS DISTINCT FROM 'captured') DESC, COALESCE(sv.promoted_at, sv.created_at) DESC, sv.id DESC
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
  ORDER BY (svo.origin IS DISTINCT FROM 'captured') DESC, COALESCE(sv.promoted_at, sv.created_at) DESC, sv.id DESC
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
  ORDER BY (svo.origin IS DISTINCT FROM 'captured') DESC, COALESCE(sv.promoted_at, sv.created_at) DESC, sv.id DESC
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

-- name: EnqueueSkillEfficacyEvaluations :exec
-- Idempotent enqueue of one evaluation per scoring unit, and the only place a
-- unit's eligibility is decided. A unit is written when its project and chat are
-- live and the visible transcript is quiet. Confirmation and reservation also
-- require the activation itself to be quiet. A conflict refreshes a pending
-- unit's observed_at so a resumed session must become quiet again before
-- reservation. The stamp is authorised by ListSkillEfficacyEvaluationUnits
-- rather than the write count because terminal units also absorb later
-- activations of the same scoring unit.
--
-- A dev unit is admitted only when the activation's actor is the chat's actor —
-- its user id against chats.user_id or its email against chats.external_user_id,
-- the columns the capture path writes those two values to. A dev session id is
-- client-supplied, so without that binding an activation could name any chat in
-- the project and have that chat's transcript scored. An activation carrying no
-- actor at all matches nothing and is refused. The assistant surface is exempt:
-- its session ids are server-generated and its activations carry no actor.
-- Deduplication happens after actor binding so user-id and email observations
-- can both confirm without targeting the same upsert row twice.
WITH input_units AS (
  SELECT
    unnest(@session_ids::text[]) AS session_id,
    unnest(@surfaces::text[]) AS surface,
    unnest(@chat_ids::uuid[]) AS chat_id,
    unnest(@skill_ids::uuid[]) AS skill_id,
    unnest(@skill_version_ids::uuid[]) AS skill_version_id,
    unnest(@canonical_sha256s::text[]) AS canonical_sha256,
    unnest(@observed_ats::timestamptz[]) AS observed_at,
    unnest(@user_ids::text[]) AS user_id,
    unnest(@user_emails::text[]) AS user_email
), actor_bound_units AS (
  SELECT
    p.organization_id,
    p.id AS project_id,
    unit.surface,
    unit.session_id,
    unit.chat_id,
    unit.skill_id,
    unit.skill_version_id,
    unit.canonical_sha256,
    unit.observed_at
  FROM input_units unit
  JOIN projects p
    ON p.id = @project_id::uuid
    AND p.deleted IS FALSE
  JOIN chats c
    ON c.id = unit.chat_id
    AND c.project_id = p.id
    AND c.deleted IS FALSE
    AND (
      unit.surface = 'assistant'
      OR (unit.user_id <> '' AND c.user_id = unit.user_id)
      OR (unit.user_email <> '' AND c.external_user_id = unit.user_email)
    )
  WHERE EXISTS (
      SELECT 1
      FROM chat_messages cm
      WHERE cm.chat_id = c.id
        AND (cm.project_id IS NULL OR cm.project_id = p.id)
    )
    AND NOT EXISTS (
      SELECT 1
      FROM chat_messages cm
      WHERE cm.chat_id = c.id
        AND (cm.project_id IS NULL OR cm.project_id = p.id)
        AND cm.created_at > now() - @inactivity::interval
    )
), deduplicated_units AS (
  SELECT DISTINCT ON (project_id, session_id, skill_version_id, surface) *
  FROM actor_bound_units
  ORDER BY project_id, session_id, skill_version_id, surface, observed_at DESC
)
INSERT INTO skill_efficacy_evaluations (
  organization_id,
  project_id,
  surface,
  session_id,
  chat_id,
  skill_id,
  skill_version_id,
  canonical_sha256,
  observed_at
)
SELECT
  unit.organization_id,
  unit.project_id,
  unit.surface,
  unit.session_id,
  unit.chat_id,
  unit.skill_id,
  unit.skill_version_id,
  unit.canonical_sha256,
  unit.observed_at
FROM deduplicated_units unit
ON CONFLICT (project_id, session_id, skill_version_id, surface) DO UPDATE
SET observed_at = GREATEST(skill_efficacy_evaluations.observed_at, excluded.observed_at),
    updated_at = clock_timestamp()
WHERE skill_efficacy_evaluations.state = 'pending';

-- name: ListSkillEfficacyEvaluationUnits :many
-- Confirmation read: the only thing that may authorise stamping
-- skill_observations.efficacy_enqueued_at. Units absent here were not enqueued
-- and must stay unstamped so a later pass retries them.
-- The per-column unnest expands the arrays in lockstep, one row per unit,
-- and the join probes skill_efficacy_evaluations on its unique key instead of
-- scanning the project's evaluation history.
--
-- It repeats the insert's actor binding against the evaluation's chat and
-- echoes the actor back, so a caller stamps only the activations whose own
-- actor matched. A dev activation naming someone else's session finds their
-- evaluation on the unique key but not through the binding, so it is refused
-- for good rather than absorbed into a unit that is not its own — and the
-- rightful owner's activations for that same session confirm regardless.
SELECT
  unit.session_id::text AS session_id,
  unit.surface::text AS surface,
  unit.skill_version_id::uuid AS skill_version_id,
  unit.user_id::text AS user_id,
  unit.user_email::text AS user_email
FROM (
  SELECT
    unnest(@session_ids::text[]) AS session_id,
    unnest(@surfaces::text[]) AS surface,
    unnest(@skill_version_ids::uuid[]) AS skill_version_id,
    unnest(@user_ids::text[]) AS user_id,
    unnest(@user_emails::text[]) AS user_email
) unit
JOIN skill_efficacy_evaluations e
  ON e.project_id = @project_id
  AND e.session_id = unit.session_id
  AND e.surface = unit.surface
  AND e.skill_version_id = unit.skill_version_id
JOIN projects p
  ON p.id = e.project_id
  AND p.deleted IS FALSE
JOIN chats c
  ON c.id = e.chat_id
  AND c.project_id = p.id
  AND c.deleted IS FALSE
  AND (
    unit.surface = 'assistant'
    OR (unit.user_id <> '' AND c.user_id = unit.user_id)
    OR (unit.user_email <> '' AND c.external_user_id = unit.user_email)
  )
WHERE e.state IN ('scored', 'failed')
   OR (
     e.state = 'pending'
     AND e.observed_at <= now() - @inactivity::interval
     AND NOT EXISTS (
       SELECT 1
       FROM chat_messages cm
       WHERE cm.chat_id = c.id
         AND (cm.project_id IS NULL OR cm.project_id = p.id)
         AND cm.created_at > now() - @inactivity::interval
     )
   );

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

-- name: LockProjectOrganizationSkillEfficacyBudget :exec
-- First statement of the reservation transaction: serialises counting and
-- reserving per organization, entered through the project, and held to commit.
SELECT pg_advisory_xact_lock(hashtextextended('skill-efficacy:' || p.organization_id, 0))
FROM projects p
WHERE p.id = @project_id::uuid;

-- name: LockOrganizationSkillEfficacyBudget :exec
-- Settings updates share the reservation lock so their audit snapshot and the
-- sampling policy observed by reservations both describe committed state.
SELECT pg_advisory_xact_lock(hashtextextended('skill-efficacy:' || @organization_id::text, 0));

-- name: GetSkillEfficacySettingsForProject :one
-- All-null settings columns mean the organization has no row, and Go applies
-- the package defaults.
SELECT
  p.organization_id,
  s.enabled,
  s.per_skill_daily_cap,
  s.org_daily_cap,
  s.new_version_burst
FROM projects p
LEFT JOIN skill_efficacy_settings s ON s.organization_id = p.organization_id
WHERE p.id = @project_id::uuid
  AND p.deleted IS FALSE;

-- name: GetSkillEfficacySettingsForOrganization :one
SELECT *
FROM skill_efficacy_settings
WHERE organization_id = @organization_id;

-- name: UpsertSkillEfficacySettingsForOrganization :one
INSERT INTO skill_efficacy_settings (
  organization_id,
  enabled,
  per_skill_daily_cap,
  org_daily_cap,
  new_version_burst
)
VALUES (
  @organization_id,
  @enabled,
  @per_skill_daily_cap,
  @org_daily_cap,
  @new_version_burst
)
ON CONFLICT (organization_id) DO UPDATE
SET enabled = excluded.enabled,
    per_skill_daily_cap = excluded.per_skill_daily_cap,
    org_daily_cap = excluded.org_daily_cap,
    new_version_burst = excluded.new_version_burst,
    updated_at = clock_timestamp()
RETURNING *;

-- name: CountSkillEfficacyOrgSpendForProject :one
-- Org-grained spend for the day, entered through the project: counts every
-- project in the organization.
SELECT count(*) AS spend
FROM skill_efficacy_evaluations e
JOIN projects p ON p.organization_id = e.organization_id
WHERE p.id = @project_id::uuid
  AND e.reserved_on = @reserved_on::date;

-- name: CountSkillEfficacySkillDailySpend :many
SELECT e.skill_id, count(*) AS spend
FROM skill_efficacy_evaluations e
WHERE e.project_id = @project_id
  AND e.skill_id = ANY(@skill_ids::uuid[])
  AND e.reserved_on = @reserved_on::date
GROUP BY e.skill_id;

-- name: CountSkillEfficacyVersionLifetimeSpend :many
-- Lifetime spend per requested version, counted no further than @burst_cap.
-- The count is only ever subtracted from that cap, so rows past it cannot
-- change the answer and each version's scan stops as soon as the cap is
-- reached. Every requested version comes back, a version with no spend as 0.
SELECT v.skill_version_id::uuid AS skill_version_id, capped.spend
FROM unnest(@skill_version_ids::uuid[]) AS v(skill_version_id)
CROSS JOIN LATERAL (
  SELECT count(*) AS spend
  FROM (
    SELECT 1
    FROM skill_efficacy_evaluations e
    WHERE e.project_id = @project_id
      AND e.skill_version_id = v.skill_version_id
      AND e.reserved_on IS NOT NULL
    LIMIT @burst_cap::int
  ) spent
) capped;

-- name: ListPendingSkillEfficacyEvaluations :many
-- Recent-first keyset page over a project's pending evaluations, ordered on the
-- unique (observed_at, id) key. A null cursor starts at the head of the queue;
-- a caller pages by handing back the last row it saw. That is what lets a
-- reservation walk past candidates its caps have exhausted instead of stalling
-- on the same head every pass.
--
-- No row lock is taken. pending -> reserved is written only by the reservation
-- pass, and every such pass holds the same per-organization advisory lock for
-- its whole transaction, so a candidate read here cannot leave pending
-- underneath it. A row arriving at pending concurrently — an insert, or a stale
-- reservation reset — only ever adds work, and the reserving UPDATE still
-- guards on state = 'pending'.
--
-- Enqueue only ever admits a live project and a live chat, but the queue
-- outlives both: a deletion after enqueue leaves a backlog whose subject is
-- gone. The liveness recheck sits before paging so a deleted unit is never a
-- candidate and never spends the organization's budget.
SELECT
  e.id,
  e.organization_id,
  e.project_id,
  e.surface,
  e.session_id,
  e.chat_id,
  e.skill_id,
  e.skill_version_id,
  e.canonical_sha256,
  e.observed_at,
  e.state,
  e.reserved_on,
  e.claim_token,
  e.attempts,
  e.last_error,
  e.scored_at,
  e.failed_at,
  e.created_at,
  e.updated_at
FROM skill_efficacy_evaluations e
JOIN projects p
  ON p.id = e.project_id
  AND p.deleted IS FALSE
JOIN chats c
  ON c.project_id = e.project_id
  AND c.id = e.chat_id
  AND c.deleted IS FALSE
WHERE e.project_id = @project_id
  AND e.state = 'pending'
  AND (e.reserved_on IS NOT NULL) = @has_reserved_spend::boolean
  AND e.observed_at <= now() - @inactivity::interval
  AND NOT EXISTS (
    SELECT 1
    FROM chat_messages cm
    WHERE cm.chat_id = c.id
      AND (cm.project_id IS NULL OR cm.project_id = p.id)
      AND cm.created_at > now() - @inactivity::interval
  )
  AND (
    sqlc.narg('cursor_observed_at')::timestamptz IS NULL
    OR (e.observed_at, e.id) < (sqlc.narg('cursor_observed_at')::timestamptz, sqlc.narg('cursor_id')::uuid)
  )
ORDER BY e.observed_at DESC, e.id DESC
LIMIT @page_size;

-- name: ReserveSkillEfficacyEvaluations :execrows
UPDATE skill_efficacy_evaluations
SET state = 'reserved',
    reserved_on = coalesce(reserved_on, @reserved_on::date),
    claim_token = @claim_token::uuid,
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND id = ANY(@ids::uuid[])
  AND state = 'pending'
  AND claim_token IS NULL
  AND failed_at IS NULL;

-- name: LoadReservedSkillEfficacyEvaluations :many
-- Crash-recovery claim. Ownership is soft and time-bounded: a reserved row is
-- owned while its updated_at is younger than @claim_lease, so a second claimer
-- inside the lease selects nothing and the model call never has to hold a
-- transaction open.
UPDATE skill_efficacy_evaluations e
SET claim_token = @claim_token::uuid,
    updated_at = clock_timestamp()
WHERE e.project_id = @project_id
  AND e.id IN (
    SELECT c.id
    FROM skill_efficacy_evaluations c
    WHERE c.project_id = @project_id
      AND c.state = 'reserved'
      AND c.updated_at < now() - @claim_lease::interval
      AND c.updated_at >= now() - @recovery_after::interval
    ORDER BY c.observed_at DESC, c.id DESC
    LIMIT @batch_size
    FOR UPDATE SKIP LOCKED
  )
RETURNING
  e.id,
  e.organization_id,
  e.project_id,
  e.surface,
  e.session_id,
  e.chat_id,
  e.skill_id,
  e.skill_version_id,
  e.canonical_sha256,
  e.observed_at,
  e.state,
  e.reserved_on,
  e.claim_token,
  e.attempts,
  e.last_error,
  e.scored_at,
  e.failed_at,
  e.created_at,
  e.updated_at;

-- name: ClaimLegacySkillEfficacyEvaluations :execrows
-- Compatibility for workflow histories recorded before claim tokens existed.
UPDATE skill_efficacy_evaluations
SET claim_token = @claim_token::uuid,
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND id = ANY(@ids::uuid[])
  AND state = 'reserved'
  AND claim_token IS NULL;

-- name: BackdateReservedSkillEfficacyEvaluationsFixture :execrows
-- Test-only fixture: age a project's reserved rows past a recovery lease so a
-- test can make staleness deterministic instead of retrying a sweep that
-- recovers rows cumulatively.
UPDATE skill_efficacy_evaluations
SET updated_at = updated_at - @backdate_by::interval
WHERE project_id = @project_id
  AND state = 'reserved';

-- name: ClearSkillEfficacyClaimTokenFixture :execrows
-- Test-only fixture for a reservation written before claim_token existed.
UPDATE skill_efficacy_evaluations
SET claim_token = NULL
WHERE project_id = @project_id
  AND id = @id
  AND state = 'reserved';

-- name: CreateScoredSkillEfficacyEvaluationFixture :one
-- Test-only fixture for suggestion evidence timestamp and watermark tests.
INSERT INTO skill_efficacy_evaluations (
  organization_id,
  project_id,
  surface,
  session_id,
  chat_id,
  skill_id,
  skill_version_id,
  canonical_sha256,
  observed_at,
  state,
  scored_at
)
SELECT
  p.organization_id,
  s.project_id,
  @surface,
  @session_id,
  @chat_id,
  s.id,
  sv.id,
  sv.canonical_sha256,
  @scored_at,
  'scored',
  @scored_at
FROM skills s
JOIN projects p
  ON p.id = s.project_id
  AND p.deleted IS FALSE
JOIN skill_versions sv
  ON sv.skill_id = s.id
  AND sv.id = @base_version_id
WHERE s.project_id = @project_id
  AND s.id = @skill_id
  AND s.archived_at IS NULL
RETURNING skill_efficacy_evaluations.*;

-- name: RecoverStaleSkillEfficacyReservations :one
-- Bounded recovery for abandoned reservations. The row lock keeps concurrent
-- sweepers disjoint; the ownership predicates fence a worker that raced the
-- recovery. reserved_on is deliberately retained as immutable spend history.
WITH stale AS (
  SELECT c.id, c.claim_token, c.updated_at
  FROM skill_efficacy_evaluations c
  WHERE c.project_id = @project_id
    AND c.state = 'reserved'
    AND c.updated_at < now() - @stale_after::interval
  ORDER BY c.updated_at, c.id
  LIMIT @batch_size
  FOR UPDATE SKIP LOCKED
), recovered AS (
  UPDATE skill_efficacy_evaluations e
  SET attempts = e.attempts + 1,
      last_error = @last_error,
      state = CASE WHEN e.attempts + 1 >= @max_attempts::integer THEN 'failed' ELSE 'pending' END,
      failed_at = CASE WHEN e.attempts + 1 >= @max_attempts::integer THEN clock_timestamp() ELSE e.failed_at END,
      claim_token = NULL,
      updated_at = clock_timestamp()
  FROM stale s
  WHERE e.project_id = @project_id
    AND e.id = s.id
    AND e.state = 'reserved'
    AND e.claim_token IS NOT DISTINCT FROM s.claim_token
    AND e.updated_at = s.updated_at
  RETURNING e.state
)
SELECT
  count(*) FILTER (WHERE state = 'pending') AS recovered,
  count(*) FILTER (WHERE state = 'failed') AS dead_lettered
FROM recovered;

-- name: MarkSkillEfficacyEvaluationScored :execrows
UPDATE skill_efficacy_evaluations
SET state = 'scored',
    scored_at = clock_timestamp(),
    claim_token = NULL,
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND id = @id
  AND state = 'reserved'
  AND claim_token = @claim_token::uuid;

-- name: RefreshSkillEfficacyEvaluationClaim :execrows
-- Reassert ownership immediately before publishing the external score. The
-- updated_at bump keeps lease claimers from rotating the token during the sink
-- write and scored transition.
UPDATE skill_efficacy_evaluations
SET updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND id = @id
  AND state = 'reserved'
  AND claim_token = @claim_token::uuid;

-- name: RecordSkillEfficacyEvaluationAttempt :one
-- Model, sink, or row-validation failure. The row never returns to pending:
-- that would free the budget and let a second reservation re-spend the same unit.
UPDATE skill_efficacy_evaluations
SET attempts = attempts + 1,
    last_error = @last_error,
    state = CASE WHEN attempts + 1 >= @max_attempts::integer THEN 'failed' ELSE 'reserved' END,
    failed_at = CASE WHEN attempts + 1 >= @max_attempts::integer THEN clock_timestamp() ELSE failed_at END,
    claim_token = CASE WHEN attempts + 1 >= @max_attempts::integer THEN NULL ELSE claim_token END,
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND id = @id
  AND state = 'reserved'
  AND claim_token = @claim_token::uuid
RETURNING state, attempts, failed_at;

-- name: GetSkillEfficacyEvaluationState :one
SELECT state, reserved_on, claim_token, attempts, last_error, scored_at, failed_at
FROM skill_efficacy_evaluations
WHERE project_id = @project_id
  AND id = @id;

-- name: GetSkillEfficacyJudgeInputs :many
-- evaluation_created_at is the row's birth stamp, which no transition rewrites.
-- It is the publication guard's stable lower bound across ownership changes.
--
-- The project and chat liveness the reservation checked is rechecked here: a
-- deletion that lands between reserving and publishing drops the row from this
-- read, so the batch judges nothing and writes no score for it. The row stays
-- reserved and is later recovered to pending, where the same guard keeps it out of
-- every candidate page.
SELECT
  e.id,
  e.organization_id,
  e.surface,
  e.session_id,
  e.chat_id,
  e.skill_id,
  e.skill_version_id,
  e.canonical_sha256,
  e.observed_at,
  e.reserved_on,
  e.created_at AS evaluation_created_at,
  e.attempts,
  s.name AS skill_name,
  sv.content AS skill_content
FROM skill_efficacy_evaluations e
JOIN projects p
  ON p.id = e.project_id
  AND p.deleted IS FALSE
JOIN chats c
  ON c.project_id = e.project_id
  AND c.id = e.chat_id
  AND c.deleted IS FALSE
JOIN skills s
  ON s.project_id = e.project_id
  AND s.id = e.skill_id
JOIN skill_versions sv
  ON sv.skill_id = s.id
  AND sv.id = e.skill_version_id
WHERE e.project_id = @project_id
  AND e.state = 'reserved'
  AND e.claim_token = @claim_token::uuid
  AND e.id = ANY(@ids::uuid[])
ORDER BY e.observed_at DESC, e.id DESC;

-- name: UpsertSkillEfficacySettingsForProject :one
-- Writes the organization's efficacy budget, entered through the project: the
-- organization is derived from a live project, so a deleted or unknown project
-- writes nothing.
--
-- No serving path calls this: the pipeline only reads settings, and every
-- organization runs on the defaults until a budget is written for it. It is the
-- write the budget tests exercise the read against, so the stored and defaulted
-- shapes are both covered.
INSERT INTO skill_efficacy_settings (
  organization_id,
  enabled,
  per_skill_daily_cap,
  org_daily_cap,
  new_version_burst
)
SELECT
  p.organization_id,
  @enabled::boolean,
  @per_skill_daily_cap::integer,
  @org_daily_cap::integer,
  @new_version_burst::integer
FROM projects p
WHERE p.id = @project_id::uuid
  AND p.deleted IS FALSE
ON CONFLICT (organization_id) DO UPDATE
SET enabled = excluded.enabled,
    per_skill_daily_cap = excluded.per_skill_daily_cap,
    org_daily_cap = excluded.org_daily_cap,
    new_version_burst = excluded.new_version_burst,
    updated_at = clock_timestamp()
RETURNING *;

-- name: ListProjectsWithPendingSkillEfficacyWork :many
-- Projects holding efficacy work the pipeline has not finished: activations
-- reconciled but never enqueued, evaluations still pending, or reservations
-- whose owner is gone. Each source is walked one project at a time and the
-- recursion merges them, so a page costs the page size rather than the size of
-- the backlog behind it.
WITH RECURSIVE pending_projects AS (
  (
    SELECT candidate.project_id, 1 AS sequence
    FROM (
      (
        SELECT so.project_id
        FROM skill_observations so
        JOIN projects p
          ON p.id = so.project_id
          AND p.deleted IS FALSE
        WHERE so.reconciled_at IS NOT NULL
          AND so.efficacy_enqueued_at IS NULL
          AND so.session_id IS NOT NULL
          AND so.skill_version_id IS NOT NULL
          AND so.project_id > @project_cursor
        ORDER BY so.project_id
        LIMIT 1
      )
      UNION ALL
      (
        SELECT e.project_id
        FROM skill_efficacy_evaluations e
        JOIN projects p
          ON p.id = e.project_id
          AND p.deleted IS FALSE
        JOIN chats c
          ON c.project_id = e.project_id
          AND c.id = e.chat_id
          AND c.deleted IS FALSE
        WHERE e.state = 'pending'
          AND e.project_id > @project_cursor
        ORDER BY e.project_id
        LIMIT 1
      )
      UNION ALL
      (
        SELECT e.project_id
        FROM skill_efficacy_evaluations e
        JOIN projects p
          ON p.id = e.project_id
          AND p.deleted IS FALSE
        WHERE e.state = 'reserved'
          AND e.updated_at < now() - @stale_after::interval
          AND e.project_id > @project_cursor
        ORDER BY e.project_id
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
        JOIN projects p
          ON p.id = so.project_id
          AND p.deleted IS FALSE
        WHERE so.reconciled_at IS NOT NULL
          AND so.efficacy_enqueued_at IS NULL
          AND so.session_id IS NOT NULL
          AND so.skill_version_id IS NOT NULL
          AND so.project_id > current_project.project_id
        ORDER BY so.project_id
        LIMIT 1
      )
      UNION ALL
      (
        SELECT e.project_id
        FROM skill_efficacy_evaluations e
        JOIN projects p
          ON p.id = e.project_id
          AND p.deleted IS FALSE
        JOIN chats c
          ON c.project_id = e.project_id
          AND c.id = e.chat_id
          AND c.deleted IS FALSE
        WHERE e.state = 'pending'
          AND e.project_id > current_project.project_id
        ORDER BY e.project_id
        LIMIT 1
      )
      UNION ALL
      (
        SELECT e.project_id
        FROM skill_efficacy_evaluations e
        JOIN projects p
          ON p.id = e.project_id
          AND p.deleted IS FALSE
        WHERE e.state = 'reserved'
          AND e.updated_at < now() - @stale_after::interval
          AND e.project_id > current_project.project_id
        ORDER BY e.project_id
        LIMIT 1
      )
    ) candidate
    ORDER BY candidate.project_id
    LIMIT 1
  ) next_project
  WHERE current_project.sequence < @page_limit
)
-- has_stale says which of the three sources named the project, to the only
-- resolution the sweep acts on: it resets reservations, and every project it
-- visits would otherwise pay for a reset that matches no row. One index probe
-- per returned project answers it, against one blind UPDATE per project.
SELECT
  pending_projects.project_id,
  EXISTS (
    SELECT 1
    FROM skill_efficacy_evaluations e
    WHERE e.project_id = pending_projects.project_id
      AND e.state = 'reserved'
      AND e.updated_at < now() - @stale_after::interval
  ) AS has_stale
FROM pending_projects
ORDER BY sequence
LIMIT @page_limit;
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
-- version by effective promotion time.
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
  SELECT sv.content, COALESCE(sv.promoted_at, sv.created_at) AS created_at
  FROM skill_versions sv
  WHERE sv.skill_id = l.skill_id
  ORDER BY COALESCE(sv.promoted_at, sv.created_at) DESC, sv.id DESC
  LIMIT 1
) latest ON TRUE
WHERE l.token = @token
  AND l.revoked_at IS NULL;

-- name: GetSharedSkillByTokenForOrganization :one
-- Custom-domain variant of GetSharedSkillByToken: the extra projects join pins
-- the share link to the organization that owns the serving domain, so one
-- tenant's skill can never be rendered under another tenant's custom domain.
SELECT
  s.name,
  s.display_name,
  s.summary,
  latest.content,
  latest.created_at AS version_created_at
FROM skill_share_links l
JOIN projects p
  ON p.id = l.project_id
  AND p.organization_id = @organization_id
  AND NOT p.deleted
JOIN skills s
  ON s.project_id = l.project_id
  AND s.id = l.skill_id
  AND s.archived_at IS NULL
JOIN LATERAL (
  SELECT sv.content, COALESCE(sv.promoted_at, sv.created_at) AS created_at
  FROM skill_versions sv
  WHERE sv.skill_id = l.skill_id
  ORDER BY COALESCE(sv.promoted_at, sv.created_at) DESC, sv.id DESC
  LIMIT 1
) latest ON TRUE
WHERE l.token = @token
  AND l.revoked_at IS NULL;

-- name: ListSkillEditSuggestionFeedback :many
SELECT feedback.*
FROM skill_edit_suggestion_feedback link
JOIN skill_feedback feedback
  ON feedback.project_id = link.project_id
  AND feedback.id = link.feedback_id
WHERE link.project_id = @project_id
  AND link.change_id = @change_id
ORDER BY feedback.created_at DESC, feedback.id DESC
LIMIT GREATEST(@page_limit::int, 0);
