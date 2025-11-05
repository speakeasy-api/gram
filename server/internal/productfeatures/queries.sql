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
