-- name: CreateLiteLLMInstance :one
INSERT INTO litellm_instances (
    id
  , organization_id
  , project_id
  , api_key_id
  , created_by_user_id
  , name
  , failure_posture
) VALUES (
    @id
  , @organization_id
  , @project_id
  , @api_key_id
  , @created_by_user_id
  , @name
  , @failure_posture
)
RETURNING *;

-- name: ListLiteLLMInstances :many
SELECT
    li.id
  , li.organization_id
  , li.project_id
  , li.api_key_id
  , li.created_by_user_id
  , li.name
  , li.failure_posture
  , li.created_at
  , li.updated_at
  , p.name AS project_name
  , p.slug AS project_slug
  , ak.key_prefix
  , ak.last_accessed_at
  , (li.deleted IS FALSE AND ak.deleted IS FALSE) AS active
  , li.last_guardrail_event_at
  , li.last_otel_event_at
  , li.last_error_at
  , li.last_error_kind
  , li.reported_litellm_version
FROM litellm_instances li
JOIN projects p
  ON p.id = li.project_id
 AND p.organization_id = li.organization_id
JOIN api_keys ak
  ON ak.id = li.api_key_id
 AND ak.project_id = li.project_id
 AND ak.organization_id = li.organization_id
WHERE li.project_id = @project_id
  AND li.organization_id = @organization_id
ORDER BY li.created_at DESC;

-- name: GetLiteLLMInstanceForUpdate :one
SELECT *
FROM litellm_instances
WHERE id = @id
  AND project_id = @project_id
  AND organization_id = @organization_id
  AND deleted IS FALSE
FOR UPDATE;

-- name: GetActiveLiteLLMInstanceIDByAPIKey :one
SELECT li.id
FROM litellm_instances li
JOIN projects p
  ON p.id = li.project_id
 AND p.organization_id = li.organization_id
 AND p.deleted IS FALSE
JOIN api_keys ak
  ON ak.id = li.api_key_id
 AND ak.project_id = li.project_id
 AND ak.organization_id = li.organization_id
 AND ak.deleted IS FALSE
WHERE li.organization_id = @organization_id
  AND li.project_id = @project_id
  AND li.api_key_id = @api_key_id
  AND li.deleted IS FALSE;

-- name: LockActiveLiteLLMInstancesInOrganization :many
SELECT li.id
FROM litellm_instances li
JOIN projects p
  ON p.id = li.project_id
 AND p.organization_id = li.organization_id
JOIN api_keys ak
  ON ak.id = li.api_key_id
 AND ak.project_id = li.project_id
 AND ak.organization_id = li.organization_id
WHERE li.id = ANY(@ids::uuid[])
  AND li.organization_id = @organization_id
  AND li.deleted IS FALSE
  AND p.deleted IS FALSE
  AND ak.deleted IS FALSE
ORDER BY li.id
FOR SHARE OF li, p, ak;

-- name: IsActiveLiteLLMInstanceInOrganization :one
SELECT EXISTS(
  SELECT 1
  FROM litellm_instances li
  JOIN projects p
    ON p.id = li.project_id
   AND p.organization_id = li.organization_id
   AND p.deleted IS FALSE
  JOIN api_keys ak
    ON ak.id = li.api_key_id
   AND ak.project_id = li.project_id
   AND ak.organization_id = li.organization_id
   AND ak.deleted IS FALSE
  WHERE li.id = @id
    AND li.organization_id = @organization_id
    AND li.deleted IS FALSE
);

-- name: GetLiteLLMActingPrincipalMintContext :one
SELECT EXISTS(
    SELECT 1
    FROM users u
    JOIN organization_user_relationships our
      ON our.user_id = u.id
     AND our.organization_id = @organization_id
     AND our.deleted_at IS NULL
    WHERE u.id = @user_id
      AND u.deleted_at IS NULL
  ) AS active_member
  , COALESCE(li.id, '00000000-0000-0000-0000-000000000000'::uuid) AS id
  , COALESCE(li.api_key_id, '00000000-0000-0000-0000-000000000000'::uuid) AS api_key_id
FROM (SELECT 1) AS request
LEFT JOIN LATERAL (
  SELECT candidate.id, candidate.api_key_id
  FROM litellm_instances candidate
  JOIN projects p
    ON p.id = candidate.project_id
   AND p.organization_id = candidate.organization_id
   AND p.deleted IS FALSE
  JOIN api_keys ak
    ON ak.id = candidate.api_key_id
   AND ak.project_id = candidate.project_id
   AND ak.organization_id = candidate.organization_id
   AND ak.deleted IS FALSE
  WHERE candidate.id = @id
    AND candidate.organization_id = @organization_id
    AND candidate.project_id = @project_id
    AND candidate.deleted IS FALSE
) li ON TRUE;


-- name: RotateLiteLLMInstanceKey :one
UPDATE litellm_instances
SET api_key_id = @new_api_key_id
  , updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND organization_id = @organization_id
  AND api_key_id = @old_api_key_id
  AND deleted IS FALSE
RETURNING *;

-- name: RevokeLiteLLMInstance :one
UPDATE litellm_instances
SET deleted_at = clock_timestamp()
  , updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND organization_id = @organization_id
  AND deleted IS FALSE
RETURNING *;

-- name: RecordLiteLLMInstanceHealth :exec
UPDATE litellm_instances
SET last_guardrail_event_at = CASE
      WHEN @guardrail_observed_at::timestamptz IS NOT NULL
        AND (last_guardrail_event_at IS NULL OR @guardrail_observed_at::timestamptz > last_guardrail_event_at)
        THEN @guardrail_observed_at::timestamptz
      ELSE last_guardrail_event_at
    END
  , last_otel_event_at = CASE
      WHEN @otel_observed_at::timestamptz IS NOT NULL
        AND (last_otel_event_at IS NULL OR @otel_observed_at::timestamptz > last_otel_event_at)
        THEN @otel_observed_at::timestamptz
      ELSE last_otel_event_at
    END
  , last_error_at = CASE
      WHEN @error_observed_at::timestamptz IS NOT NULL
        AND @error_kind::text <> ''
        AND (last_error_at IS NULL OR @error_observed_at::timestamptz > last_error_at)
        THEN @error_observed_at::timestamptz
      ELSE last_error_at
    END
  , last_error_kind = CASE
      WHEN @error_observed_at::timestamptz IS NOT NULL
        AND @error_kind::text <> ''
        AND (last_error_at IS NULL OR @error_observed_at::timestamptz > last_error_at)
        THEN @error_kind::text
      ELSE last_error_kind
    END
  , reported_litellm_version = CASE
      WHEN @reported_litellm_version::text <> ''
        AND @reported_version_observed_at::timestamptz IS NOT NULL
        AND (reported_litellm_version_at IS NULL OR @reported_version_observed_at::timestamptz > reported_litellm_version_at)
        THEN @reported_litellm_version::text
      ELSE reported_litellm_version
    END
  , reported_litellm_version_at = CASE
      WHEN @reported_litellm_version::text <> ''
        AND @reported_version_observed_at::timestamptz IS NOT NULL
        AND (reported_litellm_version_at IS NULL OR @reported_version_observed_at::timestamptz > reported_litellm_version_at)
        THEN @reported_version_observed_at::timestamptz
      ELSE reported_litellm_version_at
    END
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND (
    (sqlc.narg('instance_id')::uuid IS NOT NULL AND id = sqlc.narg('instance_id')::uuid)
    OR (
      sqlc.narg('instance_id')::uuid IS NULL
      AND api_key_id = @api_key_id
      AND deleted IS FALSE
    )
  )
  AND (
    (
      @guardrail_observed_at::timestamptz IS NOT NULL
      AND (last_guardrail_event_at IS NULL OR @guardrail_observed_at::timestamptz > last_guardrail_event_at)
    )
    OR (
      @otel_observed_at::timestamptz IS NOT NULL
      AND (last_otel_event_at IS NULL OR @otel_observed_at::timestamptz > last_otel_event_at)
    )
    OR (
      @error_observed_at::timestamptz IS NOT NULL
      AND @error_kind::text <> ''
      AND (last_error_at IS NULL OR @error_observed_at::timestamptz > last_error_at)
    )
    OR (
      @reported_litellm_version::text <> ''
      AND @reported_version_observed_at::timestamptz IS NOT NULL
      AND (reported_litellm_version_at IS NULL OR @reported_version_observed_at::timestamptz > reported_litellm_version_at)
    )
  );
