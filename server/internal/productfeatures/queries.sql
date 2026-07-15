-- name: LockOrganizationMetadata :one
SELECT id
FROM organization_metadata
WHERE id = @organization_id
FOR UPDATE;

-- name: IsFeatureEnabled :one
SELECT EXISTS (
        SELECT 1
        FROM organization_features
        WHERE organization_id = @organization_id
            AND feature_name = @feature_name
            AND deleted IS FALSE
) AS enabled;

-- name: EnableFeature :exec
INSERT INTO organization_features (
    organization_id,
    feature_name
) VALUES (
    @organization_id,
    @feature_name
)
ON CONFLICT (organization_id, feature_name) WHERE deleted IS FALSE
DO NOTHING;

-- name: DeleteFeature :one
UPDATE organization_features
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND feature_name = @feature_name
  AND deleted IS FALSE
RETURNING *;
