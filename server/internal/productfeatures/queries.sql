-- name: IsFeatureEnabled :one
SELECT EXISTS (
        SELECT 1
        FROM organization_features
        WHERE organization_id = @organization_id
            AND feature_name = @feature_name
            AND deleted IS FALSE
) AS enabled;

-- name: EnableFeature :one
INSERT INTO organization_features (
    organization_id,
    feature_name
) VALUES (
    @organization_id,
    @feature_name
)
RETURNING *;

-- name: DeleteFeature :one
UPDATE organization_features
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND feature_name = @feature_name
  AND deleted IS FALSE
RETURNING *;

-- name: ListSessionCaptureExclusions :many
SELECT user_id
FROM session_capture_exclusions
WHERE organization_id = @organization_id
  AND deleted IS FALSE
ORDER BY user_id;

-- name: IsUserSessionCaptureExcluded :one
SELECT EXISTS (
        SELECT 1
        FROM session_capture_exclusions
        WHERE organization_id = @organization_id
            AND user_id = @user_id
            AND deleted IS FALSE
) AS excluded;

-- name: ListSessionCaptureExclusionsForUsers :many
SELECT organization_id, user_id
FROM session_capture_exclusions
WHERE organization_id = @organization_id
  AND user_id = ANY(@user_ids::text[])
  AND deleted IS FALSE;

-- name: AddSessionCaptureExclusion :one
INSERT INTO session_capture_exclusions (
    organization_id,
    user_id
) VALUES (
    @organization_id,
    @user_id
)
ON CONFLICT (organization_id, user_id) WHERE (deleted IS FALSE)
DO UPDATE SET updated_at = clock_timestamp()
RETURNING *;

-- name: RemoveSessionCaptureExclusion :one
UPDATE session_capture_exclusions
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND user_id = @user_id
  AND deleted IS FALSE
RETURNING *;

-- name: ClearSessionCaptureExclusions :many
UPDATE session_capture_exclusions
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND deleted IS FALSE
RETURNING user_id;
